package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"api-tester/internal/client"
	"api-tester/internal/config"
	"api-tester/internal/generator"
	"api-tester/internal/metrics"
	"api-tester/internal/model"
	"api-tester/internal/storage"
)

type Runner struct {
	cfg     *config.Config
	store   *storage.Store
	client  *client.APIClient
	builder *generator.PayloadBuilder
	metrics *metrics.Registry
}

func New(cfg *config.Config, store *storage.Store, cl *client.APIClient, b *generator.PayloadBuilder, m *metrics.Registry) *Runner {
	return &Runner{cfg: cfg, store: store, client: cl, builder: b, metrics: m}
}

func (r *Runner) RunOnce(ctx context.Context, endpoints []model.Endpoint, mode string) (*model.RunReport, error) {
	runID, err := r.store.CreateRun(ctx, mode)
	if err != nil {
		return nil, err
	}
	start := time.Now().UTC()
	run := model.Run{ID: runID, Mode: mode, Status: "running", StartedAt: start, EndpointCnt: len(endpoints)}
	var timeoutCount int64
	var totalCost int64
	var successCount int64
	var failedCount int64
	var totalCalls int64
	var stopFlag atomic.Bool

	plannedJobs := buildJobs(endpoints, r.builder, mode, r.cfg)
	report := &model.RunReport{
		RunID:         runID,
		Mode:          mode,
		StartedAt:     start,
		EndpointCount: len(endpoints),
		PlannedCalls:  len(plannedJobs),
	}

	if len(plannedJobs) == 0 {
		run.Status = "finished"
		run.FinishedAt = time.Now().UTC()
		_ = r.store.FinishRun(ctx, run)
		r.metrics.RunCounter.WithLabelValues(mode, "finished").Inc()
		return report, nil
	}

	jobsCh := make(chan job)
	wg := sync.WaitGroup{}

	conc := r.cfg.Runner.Concurrency
	if mode == "stress" && r.cfg.Stress.Concurrency > 0 {
		conc = r.cfg.Stress.Concurrency
	}
	limiter := (*rate.Limiter)(nil)
	if mode == "stress" && r.cfg.Stress.GlobalRateLimitRPS > 0 {
		limiter = rate.NewLimiter(rate.Limit(r.cfg.Stress.GlobalRateLimitRPS), r.cfg.Stress.GlobalRateLimitRPS)
	}

	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobsCh {
				if stopFlag.Load() {
					return
				}
				r.metrics.ActiveWorkers.Inc()
				rec := r.executeJob(ctx, runID, j, limiter)
				r.metrics.ActiveWorkers.Dec()

				if rec.TaskStatus == -2 {
					atomic.AddInt64(&timeoutCount, 1)
				}
				if rec.Success {
					atomic.AddInt64(&successCount, 1)
					r.metrics.CallCounter.WithLabelValues(rec.EndpointPath, rec.Method, "success").Inc()
				} else {
					atomic.AddInt64(&failedCount, 1)
					r.metrics.CallCounter.WithLabelValues(rec.EndpointPath, rec.Method, "failed").Inc()
				}
				r.metrics.CallDurationMs.WithLabelValues(rec.EndpointPath, rec.Method).Observe(float64(rec.CostMs))
				atomic.AddInt64(&totalCalls, 1)
				atomic.AddInt64(&totalCost, rec.CostMs)

				if r.cfg.Stress.Enabled && mode == "stress" && r.cfg.Stress.StopOnHighFailureRate {
					tc := atomic.LoadInt64(&totalCalls)
					if tc >= 10 {
						failRate := float64(atomic.LoadInt64(&failedCount)) * 100 / float64(tc)
						if failRate >= float64(r.cfg.Stress.HighFailureRatePct) {
							stopFlag.Store(true)
						}
					}
				}
			}
		}()
	}

	go func() {
		defer close(jobsCh)
		for _, j := range plannedJobs {
			if stopFlag.Load() {
				return
			}
			select {
			case <-ctx.Done():
				return
			case jobsCh <- j:
			}
		}
	}()
	wg.Wait()

	run.Status = "finished"
	run.FinishedAt = time.Now().UTC()
	run.TotalCalls = int(totalCalls)
	run.SuccessCnt = int(successCount)
	run.FailedCnt = int(failedCount)
	run.TimeoutCnt = int(timeoutCount)
	if totalCalls > 0 {
		run.AvgCostMs = totalCost / totalCalls
	}
	report.FinishedAt = run.FinishedAt
	report.SuccessCount = run.SuccessCnt
	report.FailedCount = run.FailedCnt
	report.TimeoutCount = run.TimeoutCnt
	report.AvgCostMs = run.AvgCostMs
	if run.TotalCalls > 0 {
		report.FailureRatePct = float64(run.FailedCnt) * 100 / float64(run.TotalCalls)
		r.metrics.LastRunSuccessPct.Set(100 - report.FailureRatePct)
	}
	if err := os.MkdirAll(r.cfg.Runner.ReportDir, 0o755); err != nil {
		return nil, err
	}
	repPath := filepath.Join(r.cfg.Runner.ReportDir, fmt.Sprintf("run_%d.json", runID))
	agg, err := r.store.AggregateRunReport(ctx, runID)
	if err == nil {
		agg.Mode = mode
		agg.StartedAt = start
		agg.FinishedAt = run.FinishedAt
		agg.EndpointCount = len(endpoints)
		agg.PlannedCalls = len(plannedJobs)
		repBytes, _ := json.MarshalIndent(agg, "", "  ")
		_ = os.WriteFile(repPath, repBytes, 0o644)
		report = agg
	}
	run.ReportPath = repPath
	if err := r.store.FinishRun(ctx, run); err != nil {
		return nil, err
	}
	r.metrics.RunCounter.WithLabelValues(mode, "finished").Inc()
	return report, nil
}

type job struct {
	Endpoint model.Endpoint
	Payload  map[string]any
	Attempt  int
}

func buildJobs(endpoints []model.Endpoint, builder *generator.PayloadBuilder, mode string, cfg *config.Config) []job {
	var jobs []job
	for _, ep := range endpoints {
		if !ep.Active {
			continue
		}
		if cfg.Runner.OnlyIncludeTaskAPIs && !ep.HasTaskID {
			continue
		}
		payloads := builder.Build(ep)
		rounds := cfg.Runner.MaxEndpointRounds
		if mode == "stress" && cfg.Stress.PerEndpointRounds > 0 {
			rounds = cfg.Stress.PerEndpointRounds
		}
		for i := 0; i < rounds; i++ {
			for _, payload := range payloads {
				jobs = append(jobs, job{Endpoint: ep, Payload: payload, Attempt: 1})
			}
		}
	}
	return jobs
}

func (r *Runner) executeJob(ctx context.Context, runID int64, j job, limiter *rate.Limiter) model.CallRecord {
	start := time.Now().UTC()
	rec := model.CallRecord{
		RunID:          runID,
		EndpointPath:   j.Endpoint.Path,
		Method:         j.Endpoint.Method,
		SourceFile:     j.Endpoint.SourceFile,
		RequestPayload: j.Payload,
		RequestHeaders: map[string]string{r.cfg.API.APIKeyHeader: redacted(r.cfg.API.APIKey)},
		Attempt:        j.Attempt,
		CreatedAt:      start,
		TaskStatus:     -1,
	}
	if limiter != nil {
		_ = limiter.Wait(ctx)
	}
	var lastErr error
	var resp *client.CallResponse
	attempts := max(1, r.cfg.Runner.RetryCount+1)
	for attempt := 1; attempt <= attempts; attempt++ {
		rec.Attempt = attempt
		callCtx, cancel := context.WithTimeout(ctx, time.Duration(r.cfg.Runner.HTTPTimeoutSec)*time.Second)
		resp, lastErr = r.client.Invoke(callCtx, j.Endpoint, j.Payload)
		cancel()
		if lastErr == nil && resp != nil && resp.StatusCode < 500 {
			break
		}
		time.Sleep(time.Duration(r.cfg.Runner.RetryBackoffMs) * time.Millisecond)
	}
	if lastErr != nil {
		rec.ErrorMessage = lastErr.Error()
		rec.Success = false
		rec.CostMs = time.Since(start).Milliseconds()
		rec.FinishedAt = time.Now().UTC()
		_ = r.store.InsertCallRecord(ctx, rec)
		return rec
	}
	rec.ResponseCode = resp.StatusCode
	rec.ResponseBody = resp.Body
	rec.TaskID = resp.TaskID

	if j.Endpoint.HasTaskID && resp.TaskID != "" {
		pollCtx, cancel := context.WithTimeout(ctx, r.cfg.TaskTimeout())
		defer cancel()
		taskStatus, body, err := r.pollTask(pollCtx, resp.TaskID)
		rec.TaskStatus = taskStatus
		if err != nil {
			rec.ErrorMessage = err.Error()
		}
		if body != "" {
			rec.ResponseBody = body
		}
		rec.Success = slices.Contains(r.cfg.API.SuccessStatuses, taskStatus)
		if !rec.Success && err == nil && taskStatus == -2 {
			rec.ErrorMessage = "task polling timeout"
		}
	} else {
		rec.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	}
	rec.CostMs = time.Since(start).Milliseconds()
	rec.FinishedAt = time.Now().UTC()
	_ = r.store.InsertCallRecord(ctx, rec)
	return rec
}

func (r *Runner) pollTask(ctx context.Context, taskID string) (int, string, error) {
	ticker := time.NewTicker(time.Duration(r.cfg.Runner.PollIntervalSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return -2, "", ctx.Err()
		case <-ticker.C:
			status, body, err := r.client.PollTask(ctx, taskID)
			if err != nil {
				return -1, body, err
			}
			if slices.Contains(r.cfg.API.SuccessStatuses, status) {
				return status, body, nil
			}
			if slices.Contains(r.cfg.API.FailureStatuses, status) {
				return status, body, nil
			}
		}
	}
}

func redacted(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

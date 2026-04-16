package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/exp/slices"
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

func New(cfg *config.Config, store *storage.Store, client *client.APIClient, builder *generator.PayloadBuilder, metrics *metrics.Registry) *Runner {
	return &Runner{cfg: cfg, store: store, client: client, builder: builder, metrics: metrics}
}

func (r *Runner) RunOnce(ctx context.Context, endpoints []model.Endpoint, mode string) (*model.RunReport, error) {
	start := time.Now().UTC()
	runID, err := r.store.CreateRun(ctx, mode)
	if err != nil {
		return nil, err
	}
	r.metrics.RunCounter.WithLabelValues(mode, "started").Inc()

	plannedJobs := buildJobs(endpoints, r.builder, mode, r.cfg)
	workers := r.cfg.Runner.Concurrency
	if mode == "stress" && r.cfg.Stress.Concurrency > 0 {
		workers = r.cfg.Stress.Concurrency
	}
	if workers <= 0 {
		workers = 1
	}

	jobsCh := make(chan job)
	resultsCh := make(chan model.CallRecord, len(plannedJobs))
	var limiter *rate.Limiter
	if mode == "stress" && r.cfg.Stress.GlobalRateLimitRPS > 0 {
		limiter = rate.NewLimiter(rate.Limit(r.cfg.Stress.GlobalRateLimitRPS), r.cfg.Stress.GlobalRateLimitRPS)
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobsCh {
				resultsCh <- r.executeJob(ctx, runID, j, limiter)
			}
		}()
	}

	go func() {
		defer close(jobsCh)
		for _, j := range plannedJobs {
			jobsCh <- j
		}
	}()

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var total, successCnt, failedCnt, timeoutCnt int64
	var totalCost int64
	for rec := range resultsCh {
		atomic.AddInt64(&total, 1)
		atomic.AddInt64(&totalCost, rec.CostMs)
		statusLabel := fmt.Sprintf("%d", rec.TaskStatus)
		if rec.Success {
			atomic.AddInt64(&successCnt, 1)
			r.metrics.CallCounter.WithLabelValues(rec.EndpointPath, "success", statusLabel).Inc()
		} else {
			atomic.AddInt64(&failedCnt, 1)
			if rec.TaskStatus == -2 {
				atomic.AddInt64(&timeoutCnt, 1)
			}
			r.metrics.CallCounter.WithLabelValues(rec.EndpointPath, "failed", statusLabel).Inc()
		}
		r.metrics.CallDurationMs.WithLabelValues(rec.EndpointPath).Observe(float64(rec.CostMs))
	}

	run := model.Run{ID: runID, Mode: mode, StartedAt: start, FinishedAt: time.Now().UTC(), EndpointCnt: len(endpoints), TotalCalls: int(total), SuccessCnt: int(successCnt), FailedCnt: int(failedCnt), TimeoutCnt: int(timeoutCnt)}
	if total > 0 {
		run.AvgCostMs = totalCost / total
	}
	if failedCnt > 0 {
		run.Status = "finished_with_failures"
	} else {
		run.Status = "finished"
	}

	var report *model.RunReport
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
	apiKeyHeader := "Apikey"
	if r != nil && r.cfg != nil && strings.TrimSpace(r.cfg.API.APIKeyHeader) != "" {
		apiKeyHeader = r.cfg.API.APIKeyHeader
	}
	reqHeaders := map[string]string{}
	if r != nil && r.cfg != nil {
		reqHeaders[apiKeyHeader] = redacted(r.cfg.API.APIKey)
	}
	rec := model.CallRecord{
		RunID:          runID,
		EndpointPath:   j.Endpoint.Path,
		Method:         j.Endpoint.Method,
		SourceFile:     j.Endpoint.SourceFile,
		RequestPayload: j.Payload,
		RequestHeaders: reqHeaders,
		Attempt:        j.Attempt,
		CreatedAt:      start,
		TaskStatus:     -1,
	}
	finishWithError := func(msg string) model.CallRecord {
		rec.ErrorMessage = msg
		rec.Success = false
		rec.CostMs = time.Since(start).Milliseconds()
		rec.FinishedAt = time.Now().UTC()
		if r != nil && r.store != nil {
			_ = r.store.InsertCallRecord(ctx, rec)
		}
		return rec
	}
	if r == nil || r.cfg == nil || r.client == nil || r.store == nil {
		return finishWithError("runner dependencies not initialized")
	}
	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return finishWithError("rate limiter wait failed: " + err.Error())
		}
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
		if attempt < attempts {
			time.Sleep(time.Duration(r.cfg.Runner.RetryBackoffMs) * time.Millisecond)
		}
	}
	if lastErr != nil {
		return finishWithError(lastErr.Error())
	}
	if resp == nil {
		return finishWithError("invoke returned nil response")
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
	if r == nil || r.cfg == nil || r.client == nil {
		return -1, "", fmt.Errorf("runner not initialized")
	}
	ticker := time.NewTicker(time.Duration(r.cfg.Runner.PollIntervalSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return -2, "", nil
		case <-ticker.C:
			status, body, err := r.client.PollTask(ctx, taskID)
			if err != nil {
				return -1, body, err
			}
			if slices.Contains(r.cfg.API.SuccessStatuses, status) || slices.Contains(r.cfg.API.FailureStatuses, status) {
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

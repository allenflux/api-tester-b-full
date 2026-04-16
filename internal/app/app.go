package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/robfig/cron/v3"

	"api-tester/internal/client"
	"api-tester/internal/config"
	"api-tester/internal/generator"
	"api-tester/internal/metrics"
	"api-tester/internal/model"
	"api-tester/internal/runner"
	"api-tester/internal/scanner"
	"api-tester/internal/storage"
	"api-tester/internal/web"
)

type App struct {
	cfg     *config.Config
	store   *storage.Store
	scanner *scanner.Scanner
	runner  *runner.Runner
	web     *web.Server
	metrics *metrics.Registry
	cron    *cron.Cron
	mu      sync.Mutex
}

func New(configPath string) (*App, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("loaded config is nil")
	}
	if strings.TrimSpace(cfg.API.BaseURL) == "" {
		return nil, fmt.Errorf("api.base_url is empty")
	}

	if err := os.MkdirAll(cfg.Runner.ReportDir, 0o755); err != nil {
		return nil, err
	}

	store, err := storage.New(cfg.Database.Path)
	if err != nil {
		return nil, err
	}

	met := metrics.New()
	cl := client.New(cfg)
	if cl == nil {
		return nil, fmt.Errorf("client.New returned nil")
	}

	builder := generator.NewPayloadBuilder(cfg)
	r := runner.New(cfg, store, cl, builder, met)
	w := web.New(store)

	return &App{
		cfg:     cfg,
		store:   store,
		scanner: scanner.New(cfg),
		runner:  r,
		web:     w,
		metrics: met,
	}, nil
}
func (a *App) Close() error {
	if a.cron != nil {
		a.cron.Stop()
	}
	return a.store.Close()
}

func (a *App) Run(ctx context.Context) error {
	endpoints, err := a.refreshEndpoints(ctx)
	if err != nil {
		return err
	}
	log.Printf("discovered %d endpoints", len(endpoints))

	go func() {
		if err := web.RunHTTP(ctx, a.cfg.Web.ListenAddr, a.web.Handler()); err != nil {
			log.Printf("http server error: %v", err)
		}
	}()

	if a.cfg.Schedule.Enabled {
		c := cron.New()
		_, err := c.AddFunc(a.cfg.Schedule.Cron, func() {
			if _, err := a.execute(context.Background(), endpoints); err != nil {
				log.Printf("scheduled run failed: %v", err)
			}
		})
		if err != nil {
			return fmt.Errorf("setup cron: %w", err)
		}
		a.cron = c
		a.cron.Start()
	}
	if a.cfg.Schedule.RunAtStart || !a.cfg.Schedule.Enabled {
		if _, err := a.execute(ctx, endpoints); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}

func (a *App) refreshEndpoints(ctx context.Context) ([]model.Endpoint, error) {
	endpoints, err := a.scanner.Scan()
	if err != nil {
		return nil, err
	}
	if err := a.store.UpsertEndpoints(ctx, endpoints); err != nil {
		return nil, err
	}
	a.metrics.EndpointGauge.Set(float64(len(endpoints)))
	return endpoints, nil
}

func (a *App) execute(ctx context.Context, endpoints []model.Endpoint) (*model.RunReport, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	mode := a.cfg.Runner.Mode
	if mode == "" {
		mode = "smoke"
	}
	report, err := a.runner.RunOnce(ctx, endpoints, mode)
	if err != nil {
		a.metrics.RunCounter.WithLabelValues(mode, "failed").Inc()
		return nil, err
	}
	if a.cfg.Stress.Enabled {
		// Separate stress pass after smoke.
		stressReport, stressErr := a.runner.RunOnce(ctx, endpoints, "stress")
		if stressErr != nil {
			a.metrics.RunCounter.WithLabelValues("stress", "failed").Inc()
			return report, stressErr
		}
		return stressReport, nil
	}
	return report, nil
}

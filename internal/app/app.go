package app

import (
	"context"
	"fmt"
	"log"
	"os"
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
	if err := os.MkdirAll(cfg.Runner.ReportDir, 0o755); err != nil {
		return nil, err
	}
	store, err := storage.New(cfg.Database.Path)
	if err != nil {
		return nil, err
	}
	met := metrics.New()
	cl := client.New(cfg)
	builder := generator.NewPayloadBuilder(cfg)
	r := runner.New(cfg, store, cl, builder, met)

	a := &App{
		cfg:     cfg,
		store:   store,
		scanner: scanner.New(cfg),
		runner:  r,
		metrics: met,
	}
	a.web = web.New(store, a.TriggerRun)
	return a, nil
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
	log.Printf("loaded %d endpoints", len(endpoints))

	go func() {
		if err := web.RunHTTP(ctx, a.cfg.Web.ListenAddr, a.web.Handler()); err != nil {
			log.Printf("http server error: %v", err)
		}
	}()

	if a.cfg.Schedule.Enabled {
		c := cron.New()
		_, err := c.AddFunc(a.cfg.Schedule.Cron, func() {
			if _, err := a.TriggerRun(context.Background(), a.cfg.Runner.Mode); err != nil {
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
		if _, err := a.execute(ctx, endpoints, normalizeMode(a.cfg.Runner.Mode)); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}

func (a *App) ExportEndpointYAML() error {
	catalog, err := a.scanner.ExportYAML(a.cfg.Source.EndpointYAMLPath)
	if err != nil {
		return err
	}
	log.Printf("exported %d endpoints to %s", len(catalog.Endpoints), a.cfg.Source.EndpointYAMLPath)
	return nil
}

func (a *App) TriggerRun(ctx context.Context, mode string) (*model.RunReport, error) {
	endpoints, err := a.refreshEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	return a.execute(ctx, endpoints, normalizeMode(mode))
}

func normalizeMode(mode string) string {
	if mode == "stress" {
		return "stress"
	}
	return "smoke"
}

func (a *App) refreshEndpoints(ctx context.Context) ([]model.Endpoint, error) {
	var endpoints []model.Endpoint
	if !a.cfg.Runner.ScanOnStartup {
		catalog, err := scanner.LoadYAML(a.cfg.Source.EndpointYAMLPath)
		if err != nil {
			return nil, err
		}
		endpoints = catalog.Endpoints
	} else {
		scanned, err := a.scanner.Scan()
		if err != nil {
			return nil, err
		}
		endpoints = scanned
		if a.cfg.Source.EndpointYAMLPath != "" {
			if _, err := a.scanner.ExportYAML(a.cfg.Source.EndpointYAMLPath); err != nil {
				log.Printf("export endpoint yaml failed: %v", err)
			}
		}
	}
	if err := a.store.UpsertEndpoints(ctx, endpoints); err != nil {
		return nil, err
	}
	a.metrics.EndpointGauge.Set(float64(len(endpoints)))
	return endpoints, nil
}

func (a *App) execute(ctx context.Context, endpoints []model.Endpoint, mode string) (*model.RunReport, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	report, err := a.runner.RunOnce(ctx, endpoints, mode)
	if err != nil {
		a.metrics.RunCounter.WithLabelValues(mode, "failed").Inc()
		return nil, err
	}
	return report, nil
}

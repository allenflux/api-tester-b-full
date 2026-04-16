package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Registry struct {
	RunCounter        *prometheus.CounterVec
	CallCounter       *prometheus.CounterVec
	CallDurationMs    *prometheus.HistogramVec
	EndpointGauge     prometheus.Gauge
	ActiveWorkers     prometheus.Gauge
	LastRunSuccessPct prometheus.Gauge
}

func New() *Registry {
	r := &Registry{
		RunCounter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "api_tester_runs_total",
			Help: "Total runs by status and mode",
		}, []string{"mode", "status"}),
		CallCounter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "api_tester_calls_total",
			Help: "Total endpoint calls by result",
		}, []string{"path", "method", "result"}),
		CallDurationMs: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "api_tester_call_duration_ms",
			Help:    "Duration of API calls in milliseconds",
			Buckets: []float64{100, 300, 500, 1000, 3000, 5000, 10000, 30000, 60000, 120000},
		}, []string{"path", "method"}),
		EndpointGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "api_tester_discovered_endpoints",
			Help: "Discovered endpoint count",
		}),
		ActiveWorkers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "api_tester_active_workers",
			Help: "Workers currently executing",
		}),
		LastRunSuccessPct: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "api_tester_last_run_success_pct",
			Help: "Success percentage of the most recent run",
		}),
	}
	prometheus.MustRegister(r.RunCounter, r.CallCounter, r.CallDurationMs, r.EndpointGauge, r.ActiveWorkers, r.LastRunSuccessPct)
	return r
}

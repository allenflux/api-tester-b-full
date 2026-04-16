package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	App       AppConfig       `yaml:"app"`
	Source    SourceConfig    `yaml:"source"`
	API       APIConfig       `yaml:"api"`
	Runner    RunnerConfig    `yaml:"runner"`
	Stress    StressConfig    `yaml:"stress"`
	Schedule  ScheduleConfig  `yaml:"schedule"`
	Database  DatabaseConfig  `yaml:"database"`
	Web       WebConfig       `yaml:"web"`
	Overrides OverridesConfig `yaml:"overrides"`
}

type AppConfig struct {
	Name string `yaml:"name"`
	Env  string `yaml:"env"`
}

type SourceConfig struct {
	ProjectRoot    string   `yaml:"project_root"`
	RouterDirs     []string `yaml:"router_dirs"`
	IncludePatterns []string `yaml:"include_patterns"`
	ExcludePaths   []string `yaml:"exclude_paths"`
}

type APIConfig struct {
	BaseURL            string            `yaml:"base_url"`
	APIKey             string            `yaml:"apikey"`
	APIKeyHeader       string            `yaml:"apikey_header"`
	Headers            map[string]string `yaml:"headers"`
	TaskStatusPath     string            `yaml:"task_status_path"`
	TaskStatusMethod   string            `yaml:"task_status_method"`
	TaskStatusIDField  string            `yaml:"task_status_id_field"`
	TaskStatusValueKey string            `yaml:"task_status_value_key"`
	SuccessStatuses    []int             `yaml:"success_statuses"`
	FailureStatuses    []int             `yaml:"failure_statuses"`
	RequestTimeoutSec  int               `yaml:"request_timeout_sec"`
	InsecureSkipVerify bool              `yaml:"insecure_skip_verify"`
}

type RunnerConfig struct {
	Mode               string `yaml:"mode"`
	ScanOnStartup      bool   `yaml:"scan_on_startup"`
	Concurrency        int    `yaml:"concurrency"`
	PollIntervalSec    int    `yaml:"poll_interval_sec"`
	TaskTimeoutSec     int    `yaml:"task_timeout_sec"`
	HTTPTimeoutSec     int    `yaml:"http_timeout_sec"`
	MaxEndpointRounds  int    `yaml:"max_endpoint_rounds"`
	RetryCount         int    `yaml:"retry_count"`
	RetryBackoffMs     int    `yaml:"retry_backoff_ms"`
	ReportDir          string `yaml:"report_dir"`
	OnlyIncludeTaskAPIs bool  `yaml:"only_include_task_apis"`
}

type StressConfig struct {
	Enabled             bool `yaml:"enabled"`
	Concurrency         int  `yaml:"concurrency"`
	DurationSec         int  `yaml:"duration_sec"`
	PerEndpointRounds   int  `yaml:"per_endpoint_rounds"`
	GlobalRateLimitRPS  int  `yaml:"global_rate_limit_rps"`
	StopOnHighFailureRate bool `yaml:"stop_on_high_failure_rate"`
	HighFailureRatePct  int  `yaml:"high_failure_rate_pct"`
}

type ScheduleConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Cron       string `yaml:"cron"`
	RunAtStart bool   `yaml:"run_at_start"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type WebConfig struct {
	ListenAddr string `yaml:"listen_addr"`
	BasePath   string `yaml:"base_path"`
}

type OverridesConfig struct {
	Defaults        map[string]any               `yaml:"defaults"`
	ByParameterName map[string][]any             `yaml:"by_parameter_name"`
	ByPath          map[string]map[string][]any  `yaml:"by_path"`
	DisabledPaths   []string                     `yaml:"disabled_paths"`
	ForceMethods    map[string]string            `yaml:"force_methods"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.App.Name == "" {
		c.App.Name = "api-tester"
	}
	if c.API.APIKeyHeader == "" {
		c.API.APIKeyHeader = "Apikey"
	}
	if c.API.TaskStatusPath == "" {
		c.API.TaskStatusPath = "/api/public/task"
	}
	if c.API.TaskStatusMethod == "" {
		c.API.TaskStatusMethod = "GET"
	}
	if c.API.TaskStatusIDField == "" {
		c.API.TaskStatusIDField = "task_id"
	}
	if c.API.TaskStatusValueKey == "" {
		c.API.TaskStatusValueKey = "status"
	}
	if len(c.API.SuccessStatuses) == 0 {
		c.API.SuccessStatuses = []int{2}
	}
	if len(c.API.FailureStatuses) == 0 {
		c.API.FailureStatuses = []int{3, 4, 5}
	}
	if c.API.RequestTimeoutSec == 0 {
		c.API.RequestTimeoutSec = 30
	}
	if c.Runner.Concurrency <= 0 {
		c.Runner.Concurrency = 8
	}
	if c.Runner.PollIntervalSec <= 0 {
		c.Runner.PollIntervalSec = 3
	}
	if c.Runner.TaskTimeoutSec <= 0 {
		c.Runner.TaskTimeoutSec = 600
	}
	if c.Runner.HTTPTimeoutSec <= 0 {
		c.Runner.HTTPTimeoutSec = 30
	}
	if c.Runner.MaxEndpointRounds <= 0 {
		c.Runner.MaxEndpointRounds = 1
	}
	if c.Runner.RetryBackoffMs <= 0 {
		c.Runner.RetryBackoffMs = 1000
	}
	if c.Runner.ReportDir == "" {
		c.Runner.ReportDir = "reports"
	}
	if c.Database.Path == "" {
		c.Database.Path = "data/api_tester.db"
	}
	if c.Web.ListenAddr == "" {
		c.Web.ListenAddr = ":18081"
	}
	if c.Stress.Concurrency <= 0 {
		c.Stress.Concurrency = c.Runner.Concurrency
	}
	if c.Stress.PerEndpointRounds <= 0 {
		c.Stress.PerEndpointRounds = 1
	}
	if c.Stress.HighFailureRatePct <= 0 {
		c.Stress.HighFailureRatePct = 30
	}
	if c.Schedule.Cron == "" {
		c.Schedule.Cron = "0 3 * * *"
	}
	if len(c.Source.RouterDirs) == 0 {
		c.Source.RouterDirs = []string{"src/routers"}
	}
	if len(c.Source.IncludePatterns) == 0 {
		c.Source.IncludePatterns = []string{"*.py"}
	}
	if c.Overrides.Defaults == nil {
		c.Overrides.Defaults = map[string]any{}
	}
	if c.Overrides.ByParameterName == nil {
		c.Overrides.ByParameterName = map[string][]any{}
	}
	if c.Overrides.ByPath == nil {
		c.Overrides.ByPath = map[string]map[string][]any{}
	}
	if c.Overrides.ForceMethods == nil {
		c.Overrides.ForceMethods = map[string]string{}
	}
}

func (c *Config) TaskTimeout() time.Duration {
	return time.Duration(c.Runner.TaskTimeoutSec) * time.Second
}

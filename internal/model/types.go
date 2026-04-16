package model

import "time"

type Endpoint struct {
	ID            int64       `json:"id" yaml:"id,omitempty"`
	Method        string      `json:"method" yaml:"method"`
	Path          string      `json:"path" yaml:"path"`
	FuncName      string      `json:"func_name" yaml:"func_name,omitempty"`
	SourceFile    string      `json:"source_file" yaml:"source_file,omitempty"`
	Tags          []string    `json:"tags" yaml:"tags,omitempty"`
	Params        []Parameter `json:"params" yaml:"params,omitempty"`
	HasTaskID     bool        `json:"has_task_id" yaml:"has_task_id"`
	Active        bool        `json:"active" yaml:"active"`
	DiscoveryHash string      `json:"discovery_hash" yaml:"discovery_hash,omitempty"`
	CreatedAt     time.Time   `json:"created_at" yaml:"created_at,omitempty"`
	UpdatedAt     time.Time   `json:"updated_at" yaml:"updated_at,omitempty"`
}

type Parameter struct {
	Name        string   `json:"name" yaml:"name"`
	In          string   `json:"in" yaml:"in"`
	Type        string   `json:"type" yaml:"type"`
	Required    bool     `json:"required" yaml:"required"`
	Default     string   `json:"default" yaml:"default,omitempty"`
	Enum        []string `json:"enum,omitempty" yaml:"enum,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
}

type EndpointCatalog struct {
	GeneratedAt time.Time  `json:"generated_at" yaml:"generated_at"`
	ProjectRoot string     `json:"project_root" yaml:"project_root"`
	RouterDirs  []string   `json:"router_dirs" yaml:"router_dirs,omitempty"`
	Endpoints   []Endpoint `json:"endpoints" yaml:"endpoints"`
}

type Run struct {
	ID          int64     `json:"id"`
	Mode        string    `json:"mode"`
	Status      string    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	EndpointCnt int       `json:"endpoint_count"`
	TotalCalls  int       `json:"total_calls"`
	SuccessCnt  int       `json:"success_count"`
	FailedCnt   int       `json:"failed_count"`
	TimeoutCnt  int       `json:"timeout_count"`
	AvgCostMs   int64     `json:"avg_cost_ms"`
	ReportPath  string    `json:"report_path"`
	Error       string    `json:"error"`
}

type CallRecord struct {
	ID             int64             `json:"id"`
	RunID          int64             `json:"run_id"`
	EndpointPath   string            `json:"endpoint_path"`
	Method         string            `json:"method"`
	SourceFile     string            `json:"source_file"`
	RequestPayload map[string]any    `json:"request_payload"`
	RequestHeaders map[string]string `json:"request_headers"`
	ResponseCode   int               `json:"response_code"`
	ResponseBody   string            `json:"response_body"`
	TaskID         string            `json:"task_id"`
	TaskStatus     int               `json:"task_status"`
	Success        bool              `json:"success"`
	ErrorMessage   string            `json:"error_message"`
	Attempt        int               `json:"attempt"`
	CostMs         int64             `json:"cost_ms"`
	CreatedAt      time.Time         `json:"created_at"`
	FinishedAt     time.Time         `json:"finished_at"`
}

type RunReport struct {
	RunID          int64            `json:"run_id"`
	Mode           string           `json:"mode"`
	StartedAt      time.Time        `json:"started_at"`
	FinishedAt     time.Time        `json:"finished_at"`
	EndpointCount  int              `json:"endpoint_count"`
	PlannedCalls   int              `json:"planned_calls"`
	SuccessCount   int              `json:"success_count"`
	FailedCount    int              `json:"failed_count"`
	TimeoutCount   int              `json:"timeout_count"`
	AvgCostMs      int64            `json:"avg_cost_ms"`
	FailureRatePct float64          `json:"failure_rate_pct"`
	TopFailures    []FailureSummary `json:"top_failures"`
	PerEndpoint    []EndpointReport `json:"per_endpoint"`
	Meta           map[string]any   `json:"meta,omitempty"`
}

type FailureSummary struct {
	Path   string `json:"path"`
	Count  int    `json:"count"`
	Reason string `json:"reason"`
}

type EndpointReport struct {
	Path         string  `json:"path"`
	Method       string  `json:"method"`
	Calls        int     `json:"calls"`
	SuccessCount int     `json:"success_count"`
	FailedCount  int     `json:"failed_count"`
	AvgCostMs    int64   `json:"avg_cost_ms"`
	FailureRate  float64 `json:"failure_rate"`
}

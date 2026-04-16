package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"api-tester/internal/model"
)

type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`CREATE TABLE IF NOT EXISTS endpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			method TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			func_name TEXT,
			source_file TEXT,
			tags_json TEXT,
			params_json TEXT,
			has_task_id INTEGER NOT NULL DEFAULT 0,
			active INTEGER NOT NULL DEFAULT 1,
			discovery_hash TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			mode TEXT NOT NULL,
			status TEXT NOT NULL,
			started_at DATETIME NOT NULL,
			finished_at DATETIME,
			endpoint_count INTEGER NOT NULL DEFAULT 0,
			total_calls INTEGER NOT NULL DEFAULT 0,
			success_count INTEGER NOT NULL DEFAULT 0,
			failed_count INTEGER NOT NULL DEFAULT 0,
			timeout_count INTEGER NOT NULL DEFAULT 0,
			avg_cost_ms INTEGER NOT NULL DEFAULT 0,
			report_path TEXT,
			error TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS call_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id INTEGER NOT NULL,
			endpoint_path TEXT NOT NULL,
			method TEXT NOT NULL,
			source_file TEXT,
			request_payload_json TEXT,
			request_headers_json TEXT,
			response_code INTEGER NOT NULL DEFAULT 0,
			response_body TEXT,
			task_id TEXT,
			task_status INTEGER NOT NULL DEFAULT -1,
			success INTEGER NOT NULL DEFAULT 0,
			error_message TEXT,
			attempt INTEGER NOT NULL DEFAULT 1,
			cost_ms INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			finished_at DATETIME,
			FOREIGN KEY(run_id) REFERENCES runs(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_call_records_run_id ON call_records(run_id);`,
		`CREATE INDEX IF NOT EXISTS idx_call_records_endpoint_path ON call_records(endpoint_path);`,
		`CREATE INDEX IF NOT EXISTS idx_call_records_task_id ON call_records(task_id);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate stmt failed: %w", err)
		}
	}
	return nil
}

func (s *Store) UpsertEndpoints(ctx context.Context, endpoints []model.Endpoint) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
	INSERT INTO endpoints(method, path, func_name, source_file, tags_json, params_json, has_task_id, active, discovery_hash, updated_at)
	VALUES(?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)
	ON CONFLICT(path) DO UPDATE SET
		method=excluded.method,
		func_name=excluded.func_name,
		source_file=excluded.source_file,
		tags_json=excluded.tags_json,
		params_json=excluded.params_json,
		has_task_id=excluded.has_task_id,
		active=excluded.active,
		discovery_hash=excluded.discovery_hash,
		updated_at=CURRENT_TIMESTAMP`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, ep := range endpoints {
		tagsJSON, _ := json.Marshal(ep.Tags)
		paramsJSON, _ := json.Marshal(ep.Params)
		if _, err := stmt.ExecContext(ctx,
			ep.Method, ep.Path, ep.FuncName, ep.SourceFile,
			string(tagsJSON), string(paramsJSON), boolToInt(ep.HasTaskID), boolToInt(ep.Active), ep.DiscoveryHash,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListEndpoints(ctx context.Context, activeOnly bool) ([]model.Endpoint, error) {
	query := `SELECT id, method, path, func_name, source_file, tags_json, params_json, has_task_id, active, discovery_hash, created_at, updated_at FROM endpoints`
	if activeOnly {
		query += ` WHERE active = 1`
	}
	query += ` ORDER BY path`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Endpoint
	for rows.Next() {
		var ep model.Endpoint
		var tagsJSON, paramsJSON string
		var hasTaskID, active int
		if err := rows.Scan(&ep.ID, &ep.Method, &ep.Path, &ep.FuncName, &ep.SourceFile, &tagsJSON, &paramsJSON, &hasTaskID, &active, &ep.DiscoveryHash, &ep.CreatedAt, &ep.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &ep.Tags)
		_ = json.Unmarshal([]byte(paramsJSON), &ep.Params)
		ep.HasTaskID = hasTaskID == 1
		ep.Active = active == 1
		out = append(out, ep)
	}
	return out, rows.Err()
}

func (s *Store) CreateRun(ctx context.Context, mode string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO runs(mode, status, started_at) VALUES(?,?,?)`, mode, "running", time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FinishRun(ctx context.Context, run model.Run) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET status=?, finished_at=?, endpoint_count=?, total_calls=?, success_count=?, failed_count=?, timeout_count=?, avg_cost_ms=?, report_path=?, error=? WHERE id=?`,
		run.Status, run.FinishedAt.UTC(), run.EndpointCnt, run.TotalCalls, run.SuccessCnt, run.FailedCnt, run.TimeoutCnt, run.AvgCostMs, run.ReportPath, run.Error, run.ID)
	return err
}

func (s *Store) InsertCallRecord(ctx context.Context, rec model.CallRecord) error {
	payloadJSON, _ := json.Marshal(rec.RequestPayload)
	headersJSON, _ := json.Marshal(rec.RequestHeaders)
	_, err := s.db.ExecContext(ctx, `INSERT INTO call_records(run_id, endpoint_path, method, source_file, request_payload_json, request_headers_json, response_code, response_body, task_id, task_status, success, error_message, attempt, cost_ms, created_at, finished_at)
	VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.RunID, rec.EndpointPath, rec.Method, rec.SourceFile, string(payloadJSON), string(headersJSON), rec.ResponseCode, rec.ResponseBody, rec.TaskID, rec.TaskStatus, boolToInt(rec.Success), rec.ErrorMessage, rec.Attempt, rec.CostMs, rec.CreatedAt.UTC(), rec.FinishedAt.UTC(),
	)
	return err
}

func (s *Store) ListRuns(ctx context.Context, limit int) ([]model.Run, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, mode, status, started_at, COALESCE(finished_at, started_at), endpoint_count, total_calls, success_count, failed_count, timeout_count, avg_cost_ms, COALESCE(report_path,''), COALESCE(error,'') FROM runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Run
	//for rows.Next() {
	//	var r model.Run
	//	if err := rows.Scan(&r.ID, &r.Mode, &r.Status, &r.StartedAt, &r.FinishedAt, &r.EndpointCnt, &r.TotalCalls, &r.SuccessCnt, &r.FailedCnt, &r.TimeoutCnt, &r.AvgCostMs, &r.ReportPath, &r.Error); err != nil {
	//		return nil, err
	//	}
	//	out = append(out, r)
	//}
	for rows.Next() {
		var r model.Run
		var startedAtRaw any
		var finishedAtRaw any

		if err := rows.Scan(
			&r.ID,
			&r.Mode,
			&r.Status,
			&startedAtRaw,
			&finishedAtRaw,
			&r.EndpointCnt,
			&r.TotalCalls,
			&r.SuccessCnt,
			&r.FailedCnt,
			&r.TimeoutCnt,
			&r.AvgCostMs,
			&r.ReportPath,
			&r.Error,
		); err != nil {
			return nil, err
		}

		r.StartedAt = scanSQLiteTime(startedAtRaw)
		r.FinishedAt = scanSQLiteTime(finishedAtRaw)

		out = append(out, r)
	}
	return out, rows.Err()
}

func scanSQLiteTime(v any) time.Time {
	switch x := v.(type) {
	case nil:
		return time.Time{}
	case time.Time:
		return x
	case string:
		return parseSQLiteTime(x)
	case []byte:
		return parseSQLiteTime(string(x))
	default:
		return parseSQLiteTime(fmt.Sprint(v))
	}
}

func parseSQLiteTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (s *Store) ListCallRecords(ctx context.Context, runID int64, q string, limit int) ([]model.CallRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id, run_id, endpoint_path, method, source_file, request_payload_json, request_headers_json, response_code, COALESCE(response_body,''), COALESCE(task_id,''), task_status, success, COALESCE(error_message,''), attempt, cost_ms, created_at, COALESCE(finished_at, created_at) FROM call_records WHERE 1=1`
	args := []any{}
	if runID > 0 {
		query += ` AND run_id = ?`
		args = append(args, runID)
	}
	if q != "" {
		query += ` AND (endpoint_path LIKE ? OR task_id LIKE ? OR error_message LIKE ?)`
		qq := "%" + q + "%"
		args = append(args, qq, qq, qq)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.CallRecord
	//for rows.Next() {
	//	var rec model.CallRecord
	//	var payloadJSON, headersJSON string
	//	var success int
	//	if err := rows.Scan(&rec.ID, &rec.RunID, &rec.EndpointPath, &rec.Method, &rec.SourceFile, &payloadJSON, &headersJSON, &rec.ResponseCode, &rec.ResponseBody, &rec.TaskID, &rec.TaskStatus, &success, &rec.ErrorMessage, &rec.Attempt, &rec.CostMs, &rec.CreatedAt, &rec.FinishedAt); err != nil {
	//		return nil, err
	//	}
	//	rec.Success = success == 1
	//	_ = json.Unmarshal([]byte(payloadJSON), &rec.RequestPayload)
	//	_ = json.Unmarshal([]byte(headersJSON), &rec.RequestHeaders)
	//	out = append(out, rec)
	//}
	for rows.Next() {
		var rec model.CallRecord
		var payloadJSON, headersJSON string
		var success int
		var createdAtRaw any
		var finishedAtRaw any

		if err := rows.Scan(
			&rec.ID,
			&rec.RunID,
			&rec.EndpointPath,
			&rec.Method,
			&rec.SourceFile,
			&payloadJSON,
			&headersJSON,
			&rec.ResponseCode,
			&rec.ResponseBody,
			&rec.TaskID,
			&rec.TaskStatus,
			&success,
			&rec.ErrorMessage,
			&rec.Attempt,
			&rec.CostMs,
			&createdAtRaw,
			&finishedAtRaw,
		); err != nil {
			return nil, err
		}

		rec.CreatedAt = scanSQLiteTime(createdAtRaw)
		rec.FinishedAt = scanSQLiteTime(finishedAtRaw)
		rec.Success = success == 1
		_ = json.Unmarshal([]byte(payloadJSON), &rec.RequestPayload)
		_ = json.Unmarshal([]byte(headersJSON), &rec.RequestHeaders)

		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) AggregateRunReport(ctx context.Context, runID int64) (*model.RunReport, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT endpoint_path, method, COUNT(*), SUM(CASE WHEN success=1 THEN 1 ELSE 0 END), SUM(CASE WHEN success=0 THEN 1 ELSE 0 END), AVG(cost_ms)
		FROM call_records
		WHERE run_id=?
		GROUP BY endpoint_path, method
		ORDER BY endpoint_path`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	report := &model.RunReport{RunID: runID}
	var totalCalls, successCalls, failedCalls int
	var totalCost float64
	for rows.Next() {
		var ep model.EndpointReport
		var avg sql.NullFloat64
		if err := rows.Scan(&ep.Path, &ep.Method, &ep.Calls, &ep.SuccessCount, &ep.FailedCount, &avg); err != nil {
			return nil, err
		}
		if avg.Valid {
			ep.AvgCostMs = int64(avg.Float64)
		}
		if ep.Calls > 0 {
			ep.FailureRate = float64(ep.FailedCount) * 100 / float64(ep.Calls)
		}
		report.PerEndpoint = append(report.PerEndpoint, ep)
		totalCalls += ep.Calls
		successCalls += ep.SuccessCount
		failedCalls += ep.FailedCount
		totalCost += float64(ep.AvgCostMs)
	}
	if totalCalls > 0 {
		report.AvgCostMs = int64(totalCost / float64(len(report.PerEndpoint)))
		report.FailureRatePct = float64(failedCalls) * 100 / float64(totalCalls)
	}
	report.PlannedCalls = totalCalls
	report.SuccessCount = successCalls
	report.FailedCount = failedCalls

	fRows, err := s.db.QueryContext(ctx, `SELECT endpoint_path, error_message, COUNT(*) FROM call_records WHERE run_id=? AND success=0 GROUP BY endpoint_path, error_message ORDER BY COUNT(*) DESC LIMIT 10`, runID)
	if err != nil {
		return nil, err
	}
	defer fRows.Close()
	for fRows.Next() {
		var fs model.FailureSummary
		if err := fRows.Scan(&fs.Path, &fs.Reason, &fs.Count); err != nil {
			return nil, err
		}
		report.TopFailures = append(report.TopFailures, fs)
	}
	return report, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

# API Tester B

Go service for full-project router scanning, smoke/stress execution, task polling, SQLite history, Prometheus metrics, and Grafana dashboards.

## What it does

- Scans **all router `.py` files** under configured directories, not just `public.py`
- Extracts endpoint path, HTTP method, function name, and `Form/Query/Body/File` parameters
- Generates payloads from heuristics plus config overrides
- Calls the API and, when a `task_id` style workflow is detected, polls `/api/public/task` until terminal status
- Stores every run and call into SQLite
- Exposes:
  - built-in dashboard: `http://localhost:18081`
  - metrics: `http://localhost:18081/metrics`
  - Prometheus: `http://localhost:19090`
  - Grafana: `http://localhost:13000` (`admin/admin`)

## Quick start

1. Put your Python project on the server.
2. Edit `configs/config.yaml`
   - `source.project_root`
   - `api.base_url`
   - `api.apikey`
   - add `overrides.by_path` values for endpoints that require real scene names / URLs
3. Edit `docker-compose.yaml`
   - replace `/absolute/path/to/your/python/project`
4. Start:

```bash
docker compose up -d --build
```

## Notes

- SQLite file is stored in `./data/api_tester.db`
- JSON run reports are stored in `./reports`
- The scanner is regex-based for stability and low dependencies. It is designed for FastAPI style route definitions.
- `disabled_paths` is where you should disable destructive admin endpoints such as MQ pause/resume.

## Config strategy

- `overrides.by_parameter_name` gives global defaults by field
- `overrides.by_path` lets you pin exact values for a specific route
- `disabled_paths` prevents calling dangerous routes
- `only_include_task_apis` can focus the job on task-producing APIs

## Suggested first production pass

- Keep `stress.enabled=false`
- Let smoke runs stabilize
- Add real sample payloads per path
- Then turn stress back on with small RPS and small per-endpoint rounds

## Directory summary

- `cmd/api-tester`: entrypoint
- `internal/scanner`: Python router scanner
- `internal/generator`: payload generation
- `internal/client`: API call + task polling
- `internal/runner`: concurrent smoke/stress engine
- `internal/storage`: SQLite persistence
- `internal/web`: built-in history UI
- `deploy/prometheus`, `deploy/grafana`: monitoring stack

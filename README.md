# API Tester B Full

这是完整替换版工程，已经按你的要求改成 **两步式**：

1. **单独导出 endpoints YAML**
2. **服务运行时只加载 YAML，不再依赖实时扫描项目**

同时默认按你给的 Postman / curl 示例走：

- 所有非 GET 请求统一用 `application/x-www-form-urlencoded`
- 默认请求头包含 `Accept: application/json`
- 默认参数优先采用你给的 example 风格
- 页面里直接展示接口响应体

## 目录

- `cmd/api-tester`：主服务
- `cmd/export-endpoints`：单独导出路由 YAML 的进程
- `configs/config.yaml`：主配置
- `configs/endpoints.generated.yaml`：导出的 endpoint 清单
- `internal/scanner`：扫描 Python 路由
- `internal/generator`：根据参数名生成 payload
- `internal/client`：发送 form 请求 + 轮询 task
- `internal/runner`：并发执行器
- `internal/storage`：SQLite
- `internal/web`：内置页面
- `deploy/prometheus` / `deploy/grafana`：监控

## 第一步：导出路由 YAML

先改 `configs/config.yaml`：

- `source.project_root`
- `source.router_dirs`

然后执行：

```bash
go run ./cmd/export-endpoints -config configs/config.yaml
```

或者 Docker：

```bash
docker compose run --rm endpoint-exporter
```

成功后会生成：

```bash
configs/endpoints.generated.yaml
```

## 第二步：启动服务

```bash
go run ./cmd/api-tester -config configs/config.yaml -mode serve
```

或者：

```bash
docker compose up -d --build api-tester prometheus grafana
```

## 页面

- Dashboard: `http://localhost:18081`
- Prometheus: `http://localhost:19090`
- Grafana: `http://localhost:13000`  
  账号密码：`admin / admin`

页面里支持：

- 查看 runs
- 查看 calls
- 查看 endpoints
- **手动触发 smoke / stress**
- 查看每次请求 payload
- 查看每次接口响应体 response body

## 配置重点

### 1. API 基础配置

```yaml
api:
  base_url: https://stg-api-enc.inaiai.com
  apikey: 你的 key
  apikey_header: Apikey
  headers:
    Accept: application/json
  task_status_path: /api/public/task
  task_status_method: GET
  task_status_id_field: task_id
  task_status_value_key: status
  success_statuses: [2]
  failure_statuses: [3, 4, 5]
  request_encoding: form
```

### 2. 两步式加载

```yaml
runner:
  scan_on_startup: false
```

```yaml
source:
  endpoint_yaml_path: configs/endpoints.generated.yaml
```

### 3. 默认 example 参数

当前内置默认重点覆盖：

- `first_image`
- `end_image`
- `source_path`
- `bid`
- `fee`
- `title`
- `notify_url`

你给的这个例子已经按同样思路内置到了 payload builder 里：

```bash
curl --location 'https://stg-api-enc.inaiai.com/api/public/generate/kiss/video' \
--header 'Content-Type: application/x-www-form-urlencoded' \
--header 'Accept: application/json' \
--header 'Apikey: xxx' \
--data-urlencode 'first_image=...' \
--data-urlencode 'end_image=...' \
--data-urlencode 'bid=...' \
--data-urlencode 'fee=10' \
--data-urlencode 'title=12312323123'
```

### 4. 特殊接口定制

通用默认值不够时，继续补：

```yaml
overrides:
  by_path:
    /api/public/generate/kiss/video:
      first_image:
        - https://...
      end_image:
        - https://...
      bid:
        - 898b0d60-da10-44e0-a7eb-46b3455878e5
      fee:
        - "10"
      title:
        - 12312323123
```

## 建议上线顺序

1. 先导出 YAML
2. 启动服务
3. 页面点一次 **Run Smoke**
4. 看失败接口
5. 继续补 `overrides.by_path`
6. 成功率稳定后再开 stress

## 注意

- `disabled_paths` 里继续放危险接口
- `task_id` 型接口才会自动继续轮询 `/api/public/task`
- 不是 task 模式的接口会按同步 HTTP 成功/失败统计

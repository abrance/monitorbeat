# P1.4 — HTTP Output 设计与实现

> 状态：进行中（设计已通过，待落码）

## 1. 背景与目标

`monitorbeat` 当前 outputs：`console`（stdout JSONL）、`file`（JSONL 滚动）。
P1.4 新增 `http`：将事件以 JSON POST 到外部 HTTP 端点。
该能力为后续对接 **VictoriaMetrics / OTEL Collector** 的预演。

**业务硬性需求**（来自 `docs/monitorbeat-三阶段实现计划.md`）：

- 输出 `application/json` 格式事件；
- 失败时 fallback 到本地文件，**不能丢事件**；
- 与现有 `console` / `file` 共存，可同时启用多 output。

## 2. 设计决策（已与用户确认）

| 维度 | 决策 | 理由 |
|------|------|------|
| Payload 形态 | 单事件一次 POST，body 为该事件 JSON | 简单；OTEL/HTTP receiver 通用；批量化可后置 |
| Content-Type | `application/json; charset=utf-8` | 标准 |
| 超时 | 单次请求 `timeout`（默认 5s）+ 整体 `context.Context` 由 engine 传入 | 解析而非校验，依赖 `ctx` |
| 重试 | 配置 `retry_max`（默认 2）；指数退避 100ms × 2^n | 不掩盖错误；失败即 fallback |
| 失败判定 | 网络错误、timeout、HTTP 非 2xx | 范围清晰 |
| Fallback | HTTP output 自身 `fallback_path`，**不**复用全局 `file` output | 避免多 output 时重复写入；保持职责单一 |
| Fallback 写入 | JSONL + 滚动（按 `fallback_max_size` / `fallback_max_backups`） | 与 file output 行为一致 |
| 鉴权 | 可选 `auth`：`bearer <token>` 或 `basic <user:pass>` | 简洁；TLS 走 `insecure_skip_verify` 显式 flag |
| TLS | 显式 `insecure_skip_verify` bool | 默认安全，opt-in 关闭校验 |
| 关闭 | `Close()` 关闭 fallback 文件句柄、刷新 | 资源清理 |
| 健康度 | `Name()` 返回 `"http"`；错误通过返回值传出 | 与现有契约一致 |

## 3. 配置 schema

挂在全局 `outputs` map 下，与 `console` / `file` 平级：

```yaml
outputs:
  http:
    url: "http://127.0.0.1:8080/v1/events"
    timeout: 5s
    retry_max: 2
    insecure_skip_verify: false
    headers:
      X-Source: monitorbeat
    auth:
      type: bearer
      token: secret
    fallback_path: /var/log/monitorbeat/http-fallback.jsonl
    fallback_max_size: 50
    fallback_max_backups: 3
```

## 4. 接口对齐

实现 `internal/output.Output`（`Publish(ctx, event) error` / `Close() error` / `Name() string` / `Init(cfg map[string]any) error`）：

- `Init` 仅解析自身配置（不做网络请求），保证 `engine` 阶段错误先行；
- `Publish` 走 HTTP 客户端，失败则序列化 event 追加到 fallback 文件；
- `Close` 关闭 fallback writer。

`Publish` 错误语义：仅当 HTTP 失败 **且** fallback 写入也失败时才返回 error。**任一成功即静默**（不丢事件 + 不刷错误日志噪音）。

## 5. 失败处理流程

```
Publish(event)
  ├─ serialize(event) -> jsonBytes
  ├─ POST with timeout
  │   ├─ 2xx      -> return nil
  │   └─ 非 2xx / 网络错 / ctx done
  │       └─ retry up to retry_max
  │           ├─ success -> return nil
  │           └─ exhausted -> writeToFallback()
  │               ├─ success -> return nil
  │               └─ fail    -> return error
```

## 6. 测试矩阵（TDD 锁定）

1. `Init` 缺 `url` → 返回 error。
2. `Init` `fallback_path` 不可写 → 返回 error。
3. `Publish` 200 OK → 一次 POST，fallback 文件未创建。
4. `Publish` 网络错（不可达端口）→ 走 fallback，文件含 1 条 JSONL，error 为 nil。
5. `Publish` 非 2xx（500）→ 走 fallback，error 为 nil。
6. `Publish` context canceled → 走 fallback，error 为 nil。
7. `Publish` HTTP 失败 **且** fallback 路径不存在且父目录不可写 → 返回 error。
8. `Publish` 成功一次后 1 次失败 → fallback 仍只写 1 条（不重复写成功事件）。
9. `Close` 后再 `Publish` → 失败（不 panic）。
10. 鉴权头（bearer）正确出现在外发请求中。

测试用 `httptest.NewServer` 隔离网络，**不**起真实服务器。

## 7. 文件改动清单

- `internal/output/http.go`（新）— 实现 + fallback writer。
- `internal/output/http_test.go`（新）— 上述 10 个 case。
- `configs/config.go` — 新增 `HTTPOutputConfig` struct + 解析。
- `configs/examples/outputs.yaml`（新）— 三个 output 同时启用的示例。
- `cmd/monitorbeat/main.go` — side-effect import `_ ".../internal/output"` 确保注册（当前注册方式需确认；若为手写 `if/else`，则新增 `case "http":`）。
- `README.md` — P1.4 状态从 ☐ 改 ☑，补 demo yaml。
- `docs/monitorbeat-三阶段实现计划.md` — P1.4 状态更新（无需改 spec 段，仅勾选）。

## 8. 验收

- `gofmt -l` 无输出；
- `go vet ./...` 通过；
- `go test ./...` 全绿，含本文件 §6 全部 case；
- `go build ./...` 通过；
- 端到端 smoke：起本地 netcat / httptest server，对 success / 500 / unreachable 三种情形各跑 30s，确认 fallback 文件按预期增长。

## 9. 范围外（明确不做）

- 批量 / 压缩上传（gzip、NDJSON 多事件流）—— P2 评估；
- mTLS / OAuth2 / OIDC —— P2；
- 熔断 / 限流 —— P2；
- 与 `file` output 共享 fallback —— 设计决策 §2 拒绝；
- 改动 `Output` 接口签名。

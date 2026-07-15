# monitorbeat

> 干净的内核采集器，从蓝鲸 `bkmonitorbeat` 剥离 GSE / CMDB / 多租户 等 BK 特有逻辑而来。

---

## 路线图

按三阶段交付，每阶段独立可演示：

| 阶段 | 目标 | 文档 |
|---|---|---|
| **P0 地基** | 仓库骨架 + 调度器 + 配置 + 1 个 output，1 个干净 task（basereport）跑通 | [三阶段实现计划](./docs/monitorbeat-三阶段实现计划.md) |
| **P1 三大场景** | 拨测 / 日志采集 / 脚本采集 三类核心能力全跑通 | 同上 |
| **P2 收尾** | 27 个 task 全部就位 + 零 libgse 依赖 + 完整文档/Dockerfile/CI | 同上 |

## 能力概览

详见 [产品能力清单](./docs/monitorbeat-能力清单.md)：拨测、日志采集、脚本采集、主机指标、进程指标、系统事件、容器、可观测自身 8 大类共 27 个 task。

---

## 状态

✅ **P1 三大场景全部完成** — 拨测 (P1.1) + 日志采集 (P1.2) + 脚本采集 (P1.3) 全跑通。

当前进度：

| 阶段 | 状态 |
|---|---|
| P0 地基 | ✅ basereport + scheduler + console/file output |
| P1.1 拨测 | ✅ ping / tcp / udp / http 全通过 |
| P1.2 日志采集 | ✅ keyword raw_log 模式，tail + regex capture |
| P1.3 脚本采集 | ✅ script 定期执行 shell + prometheus/custom 格式解析 |
| P1.4 HTTP 输出 | ✅ output.http 端到端跑通：JSON POST + 本地文件兜底 |

P1.4 模块详情：

| 模块 | 状态 |
|---|---|
| `configs/config.go` HTTPOutputConfig + HTTPAuthConfig + Clean() | 已实现，测试通过 |
| `internal/output/http.go` HTTPOutput (Publish / tryPost / writeFallback / rotateFallback) | 已实现，11 个测试全绿 |
| `cmd/monitorbeat/main.go` case "http" wiring + decodeHTTPOutputConfig | 已实现 |
| runtime wiring + demo | `configs/p1_http.yaml` 端到端冒烟通过：成功路径（python sink 收 3 POST） + 失败路径（9999 关闭 → `/tmp/p14-fallback.jsonl` 写入 3 行 JSONL） |

P1.2 模块详情：

| 模块 | 状态 |
|---|---|
| `configs/keyword.go` KeywordConfig | 已实现，测试通过 |
| `tasks/keyword/raw_event.go` 事件构造 | 已实现，测试通过 |
| `internal/regexp/extract` capture 提取 | 已实现，测试通过 |
| `internal/input/file` tail harvester | 已实现，测试通过 |
| `scheduler/keyword` 长驻调度器 | 已实现，测试通过 |
| `tasks/keyword/keyword.go` Gather + builder | 已实现，测试通过 |
| runtime wiring + demo | `cmd/monitorbeat/main.go` keyword scheduler dispatch 就绪，`configs/p1_keyword.yaml` 端到端冒烟通过（5 条 raw_log 事件） |

P1.3 模块详情：

| 模块 | 状态 |
|---|---|
| `configs/script.go` ScriptConfig | 已实现，测试通过 |
| `internal/script/exec` runner | 已实现，测试通过 |
| `internal/script/parse` parser (prometheus + custom) | 已实现，测试通过 |
| `tasks/script/script.go` Gather + builder | 已实现，测试通过 |
| runtime wiring + demo | `cmd/monitorbeat/main.go` 注册就绪，`configs/p1_script.yaml` 端到端冒烟通过（2 条 script_event） |

## 构建

本机 Go 工具链安装在 `/opt/go/1.25.12/bin/go`（官方 `go1.25.12`，已通过 `sha256sum` 校验）。仓库默认命令：

```bash
/opt/go/1.25.12/bin/go build ./...
/opt/go/1.25.12/bin/go test ./...
```

旧路径 `/opt/go/1.24.6/bin/go` 已经不再使用，文档不再保留该别名。

当前验证快照：

- `go build ./...`：通过
- `go vet ./...`：clean
- `go test ./...`：全部通过（17 个包通过，0 失败）
- `go test -race ./internal/output/... ./configs/... -run HTTP`：通过
- `gofmt -l tasks/script configs internal/script cmd/monitorbeat internal/output`：clean
- P1.1 端到端冒烟：`configs/p1_probe.yaml` + 本地 `nc` / `python3 -m http.server`，`monitorbeat -check` 返回 `config OK`，console 打印 `ping_event` / `tcp_event` / `udp_event` / `http_event` 四类事件
- P1.2 端到端冒烟：`configs/p1_keyword.yaml` tail `/tmp/demo.log`，regex `ERROR payment_id=(\d+) amount=(\d+\.\d+)`，5 条 `raw_log` 事件全部命中，fields 包含 `payment_id` / `amount`
- P1.3 端到端冒烟：`configs/p1_script.yaml` echo prometheus 指标，2 条 `script_event` 命中，metrics 包含 `demo_total=42` + `cost_ms`
- P1.4 端到端冒烟：`configs/p1_http.yaml` 成功路径（python HTTP sink 200）收 3 POST，header `Content-Type: application/json; charset=utf-8` + 自定义 `X-Source: monitorbeat-p14`；失败路径（9999 关闭）→ `/tmp/p14-fallback.jsonl` 写入 3 行合法 JSONL

## P1.1 拨测快速演示

```bash
# 1) 编译
/opt/go/1.25.12/bin/go build -o bin/monitorbeat ./cmd/monitorbeat

# 2) 配置自检
./bin/monitorbeat -config configs/p1_probe.yaml -check   # → config OK

# 3) 起本地监听（另开终端）
nc -k -l 127.0.0.1 9999
nc -u -k -l 127.0.0.1 9998
python3 -m http.server --bind 127.0.0.1 8080

# 4) 跑 daemon（默认周期 5s，~10s 即可看到完整一轮事件）
./bin/monitorbeat -config configs/p1_probe.yaml
```

## P1.2 日志关键字采集快速演示

```bash
# 1) 编译
/opt/go/1.25.12/bin/go build -o bin/monitorbeat ./cmd/monitorbeat

# 2) 配置自检
./bin/monitorbeat -config configs/p1_keyword.yaml -check   # → config OK

# 3) 准备日志源
rm -f /tmp/demo.log && touch /tmp/demo.log
for i in 1 2 3 4 5; do echo "ERROR payment_id=$i amount=1.${i}"; done >> /tmp/demo.log

# 4) 跑 daemon（~6s 即可看到 raw_log 事件）
./bin/monitorbeat -config configs/p1_keyword.yaml
# console 输出 raw_log 事件，fields 包含 payment_id / amount
```

## P1.3 脚本采集快速演示

```bash
# 1) 编译
/opt/go/1.25.12/bin/go build -o bin/monitorbeat ./cmd/monitorbeat

# 2) 配置自检
./bin/monitorbeat -config configs/p1_script.yaml -check   # → config OK

# 3) 跑 daemon（每 5s 执行一次 echo prometheus 指标）
./bin/monitorbeat -config configs/p1_script.yaml
# console 输出 script_event，metrics 包含 demo_total=42 + cost_ms
```

## P1.4 HTTP 输出快速演示

```bash
# 1) 编译
/opt/go/1.25.12/bin/go build -o bin/monitorbeat ./cmd/monitorbeat

# 2) 配置自检
./bin/monitorbeat -config configs/p1_http.yaml -check   # → config OK

# 3a) 成功路径 — 启动一个返回 200 的小 HTTP sink（另开终端）
cat > /tmp/sink.py <<'EOF'
from http.server import BaseHTTPRequestHandler, HTTPServer
class H(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get('Content-Length','0')); self.rfile.read(n)
        self.send_response(200); self.send_header('Content-Length','2'); self.end_headers()
        self.wfile.write(b'OK')
    def log_message(self, *a, **k): pass
HTTPServer(('127.0.0.1', 9999), H).serve_forever()
EOF
python3 /tmp/sink.py   # 监听 127.0.0.1:9999

# 3b) 跑 daemon
./bin/monitorbeat -config configs/p1_http.yaml
# sink 收到 POST：Content-Type: application/json; charset=utf-8 + X-Source: monitorbeat-p14

# 4) 失败路径 — 停掉 sink，daemon 写入 JSONL 兜底
# /tmp/p14-fallback.jsonl 落盘 3 行合法 JSON（basereport 事件）
```

## License

TBD

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

✅ **P2 Tier 1 全部完成** — exceptionbeat + processbeat + socketsnapshot + selfstats + gather_up_beat + dmesg + metricbeat 全跑通。

当前进度：

| 阶段 | 状态 |
|---|---|
| P0 地基 | ✅ basereport + scheduler + console/file output |
| P1.1 拨测 | ✅ ping / tcp / udp / http 全通过 |
| P1.2 日志采集 | ✅ keyword raw_log 模式，tail + regex capture |
| P1.3 脚本采集 | ✅ script 定期执行 shell + prometheus/custom 格式解析 |
| P1.4 HTTP 输出 | ✅ output.http 端到端跑通 |
| P2 基础设施 | ✅ heartbeat 心跳 (engine 内置 60s) |
| P2 采集任务 | ✅ exceptionbeat / processbeat / socketsnapshot / selfstats / gather_up_beat / dmesg / metricbeat |

已实现 task 列表 (14/27)：

| # | Task | Status |
|---|------|--------|
| 1 | basereport | ✅ |
| 2 | ping | ✅ |
| 3 | tcp | ✅ |
| 4 | udp | ✅ |
| 5 | http | ✅ |
| 6 | keyword | ✅ |
| 7 | script | ✅ |
| 8 | exceptionbeat | ✅ (corefile + oom + diskro + diskspace) |
| 9 | processbeat | ✅ (CPU/Mem/RSS/VMS/FD/Threads) |
| 10 | socketsnapshot | ✅ (TCP/UDP 连接快照) |
| 11 | selfstats | ✅ (Go runtime 指标) |
| 12 | gather_up_beat | ✅ (uptime + task_id) |
| 13 | dmesg | ✅ (14 种内核异常模式) |
| 14 | metricbeat | ✅ (轻量 prometheus pull)

当前验证快照：

- `go build ./...`：通过 (24 packages)
- `go vet ./...`：clean
- `go test ./...`：全部通过 (24 packages, 0 失败)
- `gofmt -l .`：clean
- `make build`：`bin/monitorbeat` 产出正常
- Docker: `Dockerfile` 就绪，多阶段构建 alpine ~20MB

P2 Demo 配置：

```bash
# exceptionbeat (磁盘 + OOM 检测)
./bin/monitorbeat -config configs/p2_exceptionbeat.yaml
# processbeat (进程性能快照)
./bin/monitorbeat -config configs/p2_processbeat.yaml
# socketsnapshot (连接快照)
./bin/monitorbeat -config configs/p2_socketsnapshot.yaml
# selfstats (自监控)
./bin/monitorbeat -config configs/p2_selfstats.yaml
# dmesg (内核异常，需要 root)
sudo ./bin/monitorbeat -config configs/p2_dmesg.yaml
# metricbeat (prometheus pull)
./bin/monitorbeat -config configs/p2_metricbeat.yaml
```

## 构建

```bash
make build        # 编译到 bin/monitorbeat
make test         # 运行单测
make vet          # go vet
make lint         # gofmt 检查
make docker       # 构建 Docker 镜像
```

本机 Go 工具链：`/opt/go/1.25.12/bin/go` (go1.25.12)。

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

## Web 服务 (monitorweb)

`monitorweb` 是 monitorbeat 的可视化层（P3）：一个独立 Go 二进制，作为
VictoriaMetrics 的 PromQL 查询代理 + 静态前端托管。**自身不存储数据**，只查 VM。

### 架构

```
monitorbeat ──http output(format:victoriametrics)──▶ VictoriaMetrics
                                                        ▲ PromQL
monitorweb (Go API 代理 + 静态 SPA) ──────────────────▶ React 仪表盘
```

agent 端**零代码改动**：只需在配置里加一个 `outputs` 指向 VM 即可（见下）。

### 构建与运行

```bash
# 1. 构建前端（产出 web/ui/dist，由 Go 托管）
make web-ui            # = npm install && npm run build（在 web/ui）

# 2. 构建 monitorweb 二进制
make monitorweb        # 产出 bin/monitorweb

# 或一次性构建前后端
make web

# 3. 运行
./bin/monitorweb -config web/configs/web.yaml
# 默认监听 0.0.0.0:8080，访问 http://127.0.0.1:8080/
```

配置 `web/configs/web.yaml`：

```yaml
listen: "0.0.0.0:8080"
victoriametrics:
  url: "http://vmtest-1-victoria-metrics-cluster-vmselect.bkbase-test.svc.cluster.local:8481/select/0/prometheus"
  timeout: 15s
ui_dir: "./web/ui/dist"          # 构建后的前端目录
```

### 接入 VictoriaMetrics（agent 端，零改动）

在 monitorbeat 配置加一个 `http` output，指向 VM 的 import 接口：

```yaml
outputs:
  - type: http
    url: "http://vmtest-1-victoria-metrics-cluster-vminsert.bkbase-test.svc.cluster.local:8480/insert/0/prometheus/api/v1/import"
    method: POST
    format: victoriametrics
    timeout: 10s
```

如果使用单节点 VictoriaMetrics，则把：

- `web/configs/web.yaml` 的 `victoriametrics.url` 改为 `http://127.0.0.1:8428`
- monitorbeat `outputs.url` 改为 `http://127.0.0.1:8428/api/v1/import`

启动单节点 VM（任选其一）：

```bash
# 二进制
./victoria-metrics-prod

# 或 docker
docker run -p 8428:8428 victoriametrics/victoria-metrics
```

### API 契约

Base `/api/v1`：

| 端点 | 说明 |
|---|---|
| `GET /hosts` | 主机清单（hostname/os/arch/last_seen） |
| `GET /host/:host/summary` | 主机当前指标快照 |
| `GET /query/range?host=&metric=&from=&to=&step=` | 时序查询（多 metric） |
| `GET /metrics/names?host=` | 可用指标名列表 |
| `GET /events?host=&type=&from=&to=&step=` | 异常/事件计数时序 |
| `GET /probes?host=&from=&to=&step=` | ping/tcp/http 成功率 + 延迟 |
| `GET /healthz` | 健康检查 |

完整设计见 [web 服务设计文档](./docs/web-service-design.md)。

> 说明：指标类（basereport/processbeat/metricbeat/selfstats）完整可视化；
> 结构化异常明细（exceptionbeat/dmesg/keyword）在 VM 形态下以"计数时序"呈现，
> 明细钻取为后续迭代。

## License

TBD

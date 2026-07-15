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

✅ **P1.2 日志采集任务已完成** — P1.1 拨测 + P1.2 keyword (raw_log) 日志关键字采集全跑通：`tasks/keyword` 单文件 tail + regex capture + 事件输出。

当前进度：

| 阶段 | 状态 |
|---|---|
| P0 地基 | ✅ basereport + scheduler + console/file output |
| P1.1 拨测 | ✅ ping / tcp / udp / http 全通过 |
| P1.2 日志采集 | ✅ keyword raw_log 模式，tail + regex capture |
| P1.3 脚本采集 | 待开发 |

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
- `go test ./...`：全部通过（16 个包，含 `tasks/keyword` 5 个用例，`scheduler/keyword` 3 个用例，`internal/input/file` 3 个用例，`internal/regexp/extract` 5 个用例）
- `gofmt -l tasks/keyword configs scheduler/keyword internal/input/file internal/regexp/extract cmd/monitorbeat`：clean
- P1.1 端到端冒烟：`configs/p1_probe.yaml` + 本地 `nc` / `python3 -m http.server`，`monitorbeat -check` 返回 `config OK`，console 打印 `ping_event` / `tcp_event` / `udp_event` / `http_event` 四类事件
- P1.2 端到端冒烟：`configs/p1_keyword.yaml` tail `/tmp/demo.log`，regex `ERROR payment_id=(\d+) amount=(\d+\.\d+)`，5 条 `raw_log` 事件全部命中，fields 包含 `payment_id` / `amount`

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

## License

TBD

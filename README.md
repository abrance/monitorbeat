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

✅ **P1.1 拨测任务已完成** — P0 地基 + P1.1 拨测四大任务（`ping` / `tcp` / `udp` / `http`）全跑通：底层采集 + 调度器 + engine + console output + admin/reloader 骨架均可编译。

当前 P1.1 进度：

| 模块 | 状态 |
|---|---|
| `tasks/probe` 事件模型 | 已实现，测试通过 |
| probe configs (`ping/tcp/udp/http`) | 已实现，测试通过 |
| `tasks/tcp` | 已实现，测试通过 |
| `tasks/udp` | 已实现，测试通过 |
| `tasks/http` | 已实现，测试通过 |
| `tasks/ping`（icmp + command 双后端） | 已实现，测试通过（icmp 测试在无 `CAP_NET_RAW` 环境 SKIP） |
| runtime wiring + P1 demo | `cmd/monitorbeat/main.go` 已挂载，demo 配置 `configs/p1_probe.yaml` 端到端冒烟通过 |

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
- `go test ./...`：全部通过；`tasks/ping` 5 个用例（4 PASS + 1 SKIP，ICMP 测试在无 raw socket 权限环境 SKIP）
- `gofmt -l tasks/ping cmd/monitorbeat configs`：clean
- 端到端冒烟：`configs/p1_probe.yaml` + 本地 `nc` / `python3 -m http.server`，`monitorbeat -check` 返回 `config OK`，8s 实地运行两轮采集，console 打印 `ping_event` / `tcp_event` / `udp_event` / `http_event` 四类事件

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

## License

TBD

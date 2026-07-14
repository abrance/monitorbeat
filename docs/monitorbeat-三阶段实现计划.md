# monitorbeat 三阶段编码实现计划

> 上游文档：[`monitorbeat-能力清单.md`](./monitorbeat-能力清单.md)
> 目标产物：独立仓库 `monitorbeat`，仅含干净的采集逻辑 + 可插拔的输出/事件总线。
> 设计原则：每阶段独立可演示（demo + benchmark 通过），不依赖后续阶段。

---

## 阶段总览

| 阶段 | 目标 | 可演示产物 | 拆 libgse 量 |
|---|---|---|---|
| **P0 地基** | 仓库骨架 + 调度器 + 配置 + 1 个干净 output，1 个干净 task 跑通 | `monitorbeat run -c demo.yaml`，定时采集 `basereport`，stdout 输出 | 替换 30% |
| **P1 三大场景** | 拨测 + 日志 + 脚本 三类核心能力全跑通 | `monitorbeat` 同时跑 ping/tcp/udp/http/keyword/script，stdout + HTTP 输出 | 替换 70% |
| **P2 收尾 + 彻底解耦** | 搬完剩余 task、重写 procconf/procsync/proccustom、删 libgse 依赖、文档/Dockerfile | 27 个 task 全可用，仓库零 `libgse` import | 替换 100% |

---

## 阶段 P0 — 地基（最高优先级）

**目标**：跑得起来，看得见数据，验证调度/事件/输出骨架稳定。

### P0.1 仓库初始化

- 新建 `monitorbeat/` 仓库，`go.mod` module path 暂定 `github.com/<org>/monitorbeat`
- 选 Go 版本：1.22（与 bkmonitorbeat 同代）
- 第三方依赖（从 bkmonitorbeat 抽）：
  - `github.com/emirpasic/gods`（treemap / dll，调度器用）
  - `github.com/roylee0704/gron`（cron 调度）
  - `github.com/shirou/gopsutil/v3`（basereport 等采集用）
  - `github.com/mdlayher/netlink`（网络采集）
  - `github.com/yusufpapurcu/wmi`（仅 Windows basereport）
  - `github.com/yumaojun03/dmidecode`（basereport 硬件信息）
  - `github.com/prometheus/common`（script 解析用）
- **不引入**：`libbeat`、`libgse`、`agent-message SDK`、`gse SDK`

### P0.2 目录骨架

```
monitorbeat/
├── cmd/monitorbeat/main.go           # 入口（替代 main.go + beater.go 的壳）
├── internal/
│   ├── engine/                       # 引擎：调度 + 事件总线 + 配置热重载（替代 beater）
│   ├── eventbus/                     # 事件总线 interface（替代 libgse/beat）
│   ├── output/                       # 输出 interface + console/file 实现
│   ├── host/                         # 空 host.Watcher + 可选 file identity
│   ├── reloader/                     # SIGUSR1 重载（移植 libgse/reloader）
│   └── stats/                        # 自监控指标（Prometheus 暴露）
├── define/                           # Event / TaskConfig / Task / Scheduler 接口（去掉 libbeat）
├── configs/                          # 全局 Config + basereport config
├── scheduler/
│   ├── daemon/                       # 时间堆 + queue + job（直接搬）
│   ├── cron/                         # gron
│   ├── checker/                      # 探测型轮询
│   ├── listen/                       # 监听型
│   └── base.go                       # BaseScheduler
├── tasks/basereport/                 # 第一个搬过来的 task（纯采集，零 BK 依赖）
├── tasks/factory.go                  # task 注册表
├── http/admin/                       # pprof 调试端（admin_addr=0.0.0.0:56060）
├── configs/demo.yaml                 # 演示配置
├── Makefile
├── Dockerfile
└── README.md
```

### P0.3 核心接口（首次定型，后续阶段不动契约）

```go
// define/event.go
type Event interface {
    AsMap() map[string]interface{}
    GetType() string
}

// define/task.go
type TaskConfig interface {
    GetType() string
    GetTaskID() int32
    GetIdent() string
    Clean() error
}

type Task interface {
    PreRun(ctx context.Context) error
    Run(ctx context.Context, e chan<- Event) error
    PostRun(ctx context.Context) error
    Stop()
    Wait()
    Reload(TaskConfig) error
}

// internal/output/output.go
type Output interface {
    Name() string
    Init(cfg map[string]any) error
    Publish(ctx context.Context, ev Event) error
    Close() error
}

// internal/eventbus/eventbus.go
type Bus interface {
    Publish(e Event)
    Subscribe() <-chan Event
    Close()
}
```

### P0.4 搬 basereport（最小可用 task）

- 直接拷 `pkg/bkmonitorbeat/tasks/basereport/`（collector 全套 + toolkit/storage）
- 替换 `libgse/common.MapStr` → `map[string]interface{}`
- 替换 `libgse/beat` 调用 → `internal/eventbus`
- `bk_collect_type` 字段保留为 `"basereport"`（与 BK 兼容，便于以后对接）
- `tasks/basereport/event.go`：剥除 `tasks.CmdbEventSender` 相关

### P0.5 输出：`console` + `file`

- `console`：JSON 行 → stdout，便于调试
- `file`：JSON 行 → 滚动文件，可配路径与最大体积
- 输出格式示例：

```json
{"timestamp":"2024-...","dataid":1001,"type":"basereport","bk_collect_type":"basereport","dimensions":{...},"metrics":{...}}
```

### P0.6 调度器搬 daemon + checker

- 直接搬 `scheduler/daemon/{scheduler,queue,job}.go`，把 `define.Event` 改成本仓的
- 直接搬 `scheduler/checker/scheduler.go`
- `taskfactory` 注册 `basereport` 一种

### P0.7 演示配置 `configs/demo.yaml`

```yaml
mode: daemon
admin_addr: 0.0.0.0:56060
event_buffer_size: 1024

basereport_task:
  enabled: true
  period: 30s
  dataid: 1001

output:
  - type: console
  - type: file
    path: /tmp/monitorbeat/events.log
    max_size_mb: 100
```

### P0.8 main.go 启动

- 命令行：`monitorbeat run -c <yaml>` / `monitorbeat check -c <yaml>`
- 信号：SIGINT/SIGTERM 优雅退出；SIGUSR1 触发配置热重载
- 健康检查：返回 0 时输出版本号 + 启动模式

### P0.9 验证（P0 出口标准）

- [ ] `go build ./...` 零 libgse / libbeat 引用
- [ ] `monitorbeat run -c configs/demo.yaml` 30s 内 stdout 出现 ≥ 1 条 basereport 事件
- [ ] `curl http://localhost:56060/debug/pprof/` 可访问
- [ ] 端到端冒烟：CPU/内存/磁盘/网络指标数值与同环境 `top`/`free`/`df` 对齐
- [ ] 单测：`scheduler/daemon` 时间推进、并发限制、reload 不丢失任务
- [ ] 性能基线：100 task 并发下事件丢失率 = 0、内存 < 200MB

---

## 阶段 P1 — 三大场景（次高优先级）

**目标**：把"拨测 / 日志采集 / 脚本采集"三大对外卖点全跑通。这是用户的核心使用场景。

### P1.1 拨测 4 件套：ping / tcp / udp / http

- 直接搬 `tasks/{ping,tcp,udp,http}/`
- 替换 `libgse/output/gse.Report(...)` → `internal/output.Publish(...)`
- `tasks/http/event.go` 中的 `CmdbEventSender` 引用剥除
- 验证：
  - `ping 127.0.0.1`：每 60s 出一条 `ping_event`
  - `tcp localhost:22` / `tcp 192.0.2.1:65530`（不可达）：超时字段正确
  - `udp`：本地起 `nc -ul 9999`，验证回应时长
  - `http https://example.com`：状态码、TLS 耗时、首字节耗时齐全

### P1.2 日志采集：keyword task

- 搬 `tasks/keyword/` 全部 + `tasks/keyword/input/file/` 全部 + `tasks/keyword/{processor,sender}/`
- `scheduler/keyword/scheduler.go` 同步搬
- 替换 sender 里 `bkcommon.MapStr` → `map[string]interface{}`
- 替换 sender 里 event 发送通道：从 `define.Event` 总线接到 `internal/output`
- 验证（用现成的 `findings.md` 提到的 `--container` 风格本地测试）：
  - 准备 demo 日志：`while true; do echo "ERROR payment_id=12345 amount=99.9"; sleep 0.1; done >> /tmp/demo.log`
  - 配置 `keyword_task` 监听 `/tmp/demo.log`，正则 `payment_id=(\d+) amount=(\d+\.\d+)`，ReportPeriod=10s
  - 10s 内 stdout 输出 ≥ 1 条 keyword 聚合事件，dimension 包含 `payment_id/amount`
  - 同时启用 `raw_log` 模式：输出每行匹配事件

### P1.3 脚本采集：script task

- 搬 `tasks/script/`（含 formatdata 系列）
- 替换 `libgse/common.MapStr` → `map[string]interface{}`
- 替换输出通道
- 验证：
  - 写一个返回 prometheus 文本的小脚本：

```bash
#!/bin/bash
echo '# HELP demo_total demo counter'
echo 'demo_total 42'
```

- 配置 script_task 执行它，period=15s
- 15s 内 stdout 输出 1 条 script_event，含 `demo_total=42`

### P1.4 输出加 HTTP（OpenTelemetry 风格 line protocol）

- `output.http`：POST JSON 到配置 endpoint，失败本地 fallback file
- 用 `output.console` + `output.file` + `output.http` 多输出并行验证
- `output.http` 是后续对接 VictoriaMetrics / Prom remote_write / OpenTelemetry Collector 的预演

### P1.5 验证（P1 出口标准）

- [ ] `go build ./...` 零 libgse / libbeat 引用
- [ ] demo 配置含全部 6 个 task（ping/tcp/udp/http/keyword/script）能同时跑
- [ ] 每个 task 在 1 分钟内至少产出 1 条事件
- [ ] keyword raw_log 模式与聚合模式可同时启用，输出事件类型不同（`raw_log` vs `keyword_event`）
- [ ] script 任务支持 stdout prom 文本格式 + 自定义 formatdata
- [ ] HTTP output：本地起 `nc -l 9999`，能看到 POST 请求体
- [ ] 单测覆盖率：拨测 ≥ 70%，keyword ≥ 80%（regex/file 行为）
- [ ] 文档：每个 task 至少 1 个 `examples/` yaml 片段

---

## 阶段 P2 — 收尾 + 彻底解耦

**目标**：剩余 20 个 task 全搬完，CMDB-bound 的 3 个重写为通用"进程配置源"，仓库零 libgse 引用。

### P2.1 搬剩余无依赖 task

| Task | 路径 | 备注 |
|---|---|---|
| `static` | tasks/static | 主机静态信息 |
| `selfstats` | tasks/selfstats | 采集器自监控，改用 internal/stats |
| `gather_up_beat` | tasks/gatherup.go | 改用 internal/output |
| `loginlog` | tasks/loginlog | utmp/wtmp |
| `shellhistory` | tasks/shellhistory | bash/zsh 历史 |
| `rpmpackage` | tasks/rpmpackage | RPM 包列表 |
| `dmesg` | tasks/dmesg | 内核环形缓冲 |
| `timesync` | tasks/timesync | NTP/chrony |
| `procstatus` | tasks/procstatus | 进程状态 |
| `procbin` | tasks/procbin | 进程二进制指纹 |
| `procsnapshot` | tasks/procsnapshot | 进程快照 |
| `socketsnapshot` | tasks/socketsnapshot | socket 快照 |
| `processbeat` | tasks/processbeat | 进程性能 |

### P2.2 重写 3 个 CMDB-bound task（重点）

**目标**：把 `/var/lib/gse/host/hostid` 读死路径换成通用"进程配置源"。

- 新抽象 `internal/procsource.Source`：

```go
type Source interface {
    // 周期性返回进程定义列表（CMDB / K8s API / 本地 YAML 都能接）
    List(ctx context.Context) ([]ProcessSpec, error)
    Name() string
}

type ProcessSpec struct {
    PID         string   // 匹配规则（按 binary name / cmdline regex / 端口）
    Binary      string
    Ports       []int
    Labels      map[string]string
}
```

- 内置实现：
  - `procsource.file` — 读 `process_specs.yaml`
  - `procsource.http` — 拉 HTTP endpoint 返回 JSON
  - `procsource.static` — 启动时单次加载
- `procconf` → 改成"消费 Source + 生成 `processbeat` 子任务"（与原行为对齐）
- `procsync` → "把 Source 结果同步到子配置文件目录"（可选）
- `proccustom` → 已经是"自定义"，只需把 hostid 来源换成 Source

### P2.3 搬 exceptionbeat

- 搬 `tasks/exceptionbeat/` 全套（含 4 个子 collector：corefile/oom/diskro/diskspace）
- 替换 `libgse/beat` → `internal/eventbus`
- 替换 `libgse/output/gse` → `internal/output`

### P2.4 搬 trap / kubeevent / metricbeat

- `trap` — 端口复用，sender 替换
- `kubeevent` — k8s client 不依赖 BK，但要重写 sender
- `metricbeat` — 评估是否整包搬 `bkmetricbeats`，如体量过大，先用本地 HTTP pull 占位

### P2.5 重写 engine / 心跳 / 自监控

- `internal/engine/engine.go` 替代 `beater/beater.go`：
  - 去 `tenant.Client` 多租户
  - 去 `libgse/output/gse.RegisterHostWatcher`
  - 心跳：内置自管，每 60s 向 output 发一条 `heartbeat_event`
- `internal/stats/`：用 promhttp 暴露 `/metrics`，指标命名重命名为 monitorbeat 命名空间
- 重载：保留 SIGUSR1，配置文件路径可配

### P2.6 文档与构建产物

- `README.md`：安装、快速开始、配置参考、task 列表
- `docs/task-reference/`：每个 task 一份 markdown（含字段表 + 1 个示例 YAML）
- `docs/architecture.md`：模块图、调度时序、数据流
- `docs/migration-from-bkmonitorbeat.md`：从 BK 迁移指南
- `Dockerfile`：多阶段构建，alpine 镜像，最终镜像 ≤ 50MB
- `Makefile`：build / test / lint / docker
- CI：GitHub Actions，至少 `go vet` + `go test ./...` + 编译多平台（linux/amd64, linux/arm64, darwin, windows）

### P2.7 验证（P2 出口标准 / 整体收尾）

- [ ] `grep -r "libgse" monitorbeat/` → 0 命中
- [ ] `grep -r "libbeat" monitorbeat/` → 0 命中
- [ ] `grep -r "/var/lib/gse" monitorbeat/` → 0 命中
- [ ] `grep -r "tenant\." monitorbeat/` → 0 命中
- [ ] `grep -r "cmdb" monitorbeat/` → 仅出现在配置注释或可选扩展
- [ ] 27 个 task 全部注册并能跑通至少 1 个 demo（用 Docker 跑全量）
- [ ] CI 通过：build + test + lint
- [ ] 镜像 ≤ 50MB
- [ ] 启动到首事件 < 2s
- [ ] 性能回归：与 bkmonitorbeat 同期对比 basereport CPU < 80%（同等采集频率）
- [ ] 文档完整：每个 task 有 reference，每个外部依赖有 why/how

---

## 优先级总结

| 阶段 | 优先级 | 价值 | 工作量 | 独立可演示 |
|---|---|---|---|---|
| **P0 地基** | ★★★★★ | 验证骨架可行，后续阶段依赖 | 5–7 人天 | ✅ demo 输出 basereport |
| **P1 三大场景** | ★★★★☆ | 用户核心卖点，验证输出+事件总线+调度 | 7–10 人天 | ✅ 拨测/日志/脚本齐跑 |
| **P2 收尾** | ★★★ | 完整能力 + 彻底解耦 | 10–15 人天 | ✅ 全 27 task + 零 libgse |

**推荐执行顺序**：P0 → P1 → P2。每阶段结束都要 demo + bench + review，再启动下一阶段，避免大爆炸。

---

## 风险与备选

| 风险 | 应对 |
|---|---|
| `metricbeat` 依赖 bkmetricbeats 子模块体量大 | P2 阶段先本地 HTTP pull 占位，metricbeat 列为 P2+ |
| `exceptionbeat` 子 collector 平台分支多 | 用 `//go:build` 保持原样，不重写平台分支 |
| `tasks/keyword` 文件 harvester 是体量最大单 task | P1 阶段独立 feature flag，单测优先 |
| `procconf/procsync` 重写为通用 Source 后，与原 BK 行为差距 | P2 提供 `procsource.cmdblegacy` 兼容包 |
# monitorbeat 产品能力清单

> 基于 `pkg/bkmonitorbeat` v3.73.4186+（commit bf734e5b）源码梳理。
> 目标：剥离蓝鲸 GSE / CMDB / 多租户 等 BK 特有逻辑，抽出"干净的采集内核" `monitorbeat` 仓库。

---

## 1. 调研范围与方法

- 入口：`pkg/bkmonitorbeat/{main.go,beater,scheduler,tasks,configs,define,utils}`
- 公共 SDK：`pkg/libgse`（输出 / 事件总线 / 存储 / 重载 / debug）
- 主机身份：`pkg/utils/host/watcher.go`（CMDB hostid 文件解析）
- 多租户：`pkg/bkmonitorbeat/tenant/`（gseagent IPC 配置下发）
- 注册机制：`beater/taskfactory/factory.go` + `beater/schedulerfactory/factory.go`，均通过 `init()` 注册到 map

Task 接口契约（来自 `tasks/task.go` + `define/task.go`）：

```go
type TaskConfig interface {
    GetType() string
    GetTaskID() int32
    GetIdent() string
    Clean() error
}

type Task interface {
    Run(ctx context.Context, e chan<- define.Event) error
    PreRun / PostRun / Stop / Wait / Reload
}

type Event interface {
    AsMapStr() map[string]interface{}
    GetType() string
    IgnoreCMDBLevel() bool
}
```

---

## 2. 当前 26 个 Task 的能力图谱

### 2.1 拨测 / 主动探测（5）

| Task ID | 路径 | 采集内容 | BK 依赖 |
|---|---|---|---|
| `ping` | `tasks/ping` | ICMP Ping（pinger.go） | 无 |
| `tcp` | `tasks/tcp` | TCP 拨号 + 握手 + 首字节耗时 | 无 |
| `udp` | `tasks/udp` | UDP 拨号 + 回应 | 无 |
| `http` | `tasks/http` | HTTP 多方法/header/body/redirect/TLS | 输出侧 `libgse/output/gse` |
| `snmptrap` | `tasks/trap` | snmptrapd 协议监听 + 解析（UDP/TCP） | 输出侧 |

### 2.2 日志采集（2 种模式，1 个 task）

| Task ID | 路径 | 采集内容 | BK 依赖 |
|---|---|---|---|
| `raw_log` | `tasks/keyword` + `input/file` | 文件扫描 / 滚动 / offset / encoding / regex 提取 | sender 走 `libgse/common` |
| `keyword` | 同上 | 上述 + 计数聚合 + 周期上报 | 同上 |

底层是独立 harvester（`tasks/keyword/input/file/`），支持文件编码、滚动、inactive、scan_frequency、exclude patterns。

### 2.3 脚本采集（1）

| Task ID | 路径 | 采集内容 | BK 依赖 |
|---|---|---|---|
| `script` | `tasks/script` | 周期性执行用户脚本/二进制，stdout 解析为 metric（Prometheus 文本协议 / 自定义 formatdata） | 输出走 `libgse/common` |

### 2.4 主机 / 进程 采集（13）

| Task ID | 路径 | 采集内容 | BK 依赖 |
|---|---|---|---|
| `basereport` | `tasks/basereport` | CPU/内存/磁盘/网络/负载/环境变量/IO/Swap（基于 gopsutil） | 仅工具别名 |
| `processbeat` | `tasks/processbeat` | 进程 PID/cmdline/exe/port/user/perf（基于 gopsutil） | hostID 路径 |
| `procconf` | `tasks/procconf` | 从 CMDB hostid 文件解析进程配置 → 生成子任务 | **强 CMDB** |
| `procsync` | `tasks/procsync` | 把 hostid 的进程配置同步到子配置目录 | **强 CMDB** |
| `proccustom` | `tasks/proccustom` | 用户自定义进程采集 | hostID |
| `procstatus` | `tasks/procstatus` | 进程状态（cmdline/memory/cpu/start_at） | 无 |
| `procbin` | `tasks/procbin` | 进程二进制指纹（hash/size 等） | 无 |
| `procsnapshot` | `tasks/procsnapshot` | 进程全量快照（pid 列表 + cmdline） | 无 |
| `socketsnapshot` | `tasks/socketsnapshot` | 系统 socket 列表快照 | 无 |
| `shellhistory` | `tasks/shellhistory` | 用户 shell 历史（bash/zsh） | 无 |
| `rpmpackage` | `tasks/rpmpackage` | RPM 包列表 | 无 |
| `static` | `tasks/static` | 主机静态信息（OS/内核/CPU 型号/dmidecode） | 无 |
| `selfstats` | `tasks/selfstats` | 采集器自监控（运行时长/任务数/失败率） | 仅输出侧 |

### 2.5 系统事件 / 异常（3）

| Task ID | 路径 | 采集内容 | BK 依赖 |
|---|---|---|---|
| `exceptionbeat` | `tasks/exceptionbeat` | corefile / OOM / diskro / diskspace 4 个子采集器 | `libgse/beat` + `libgse/output/gse` |
| `dmesg` | `tasks/dmesg` | 内核环形缓冲区解析 | 输出侧 |
| `timesync` | `tasks/timesync` | NTP/chrony 时间同步偏移 | 输出侧 |

### 2.6 容器 / K8s（1）

| Task ID | 路径 | 采集内容 | BK 依赖 |
|---|---|---|---|
| `kubeevent` | `tasks/kubeevent` | K8s watch 事件 → 上报 | 输出侧 |

### 2.7 兼容采集器（1）

| Task ID | 路径 | 采集内容 | BK 依赖 |
|---|---|---|---|
| `metricbeat` | `tasks/metricbeat` | 复用 bkmetricbeats（Prometheus Pull）模块 | **强 libgse + 子模块** |

### 2.8 辅助（3）

| Task ID | 路径 | 采集内容 | BK 依赖 |
|---|---|---|---|
| `loginlog` | `tasks/loginlog` | utmp/wtmp 登录日志 | 输出侧 |
| `gather_up_beat` | `tasks/gatherup.go` | 采集器自身上报（计数/成功率） | 输出侧 + tenant storage |
| `heart_beat` | `beater/configengine.go` | 全局心跳 + 子任务心跳 | 输出侧 |

---

## 3. 调度器层（核心可复用，零 BK 依赖）

四套调度器，全部干净 — 可直接搬：

| 调度器 | 文件 | 用途 |
|---|---|---|
| `daemon` | `scheduler/daemon/{scheduler,queue,job}.go` | 时间堆（treemap）+ cron-like 触发，最常用（默认 mode=daemon） |
| `cron` | `scheduler/cron/scheduler.go` | 基于 gron 的 cron 表达式调度（mode=cron） |
| `checker` | `scheduler/checker/scheduler.go` | 探测型任务的固定周期轮询（mode=check） |
| `listen` | `scheduler/listen/scheduler.go` | 监听型任务（trap/kubeevent/dmesg/metric） |
| `keyword` | `scheduler/keyword/scheduler.go` | 日志采集专用 input → processor → sender 流水线 |

第三方依赖：`emirpasic/gods`（treemap / dll）、`roylee0704/gron` — 与 BK 无关。

---

## 4. BK-specific 触碰点（必须剥离/重写）

### 4.1 输出层（最大头）
- `libgse/output/gse` — 走 domain socket 发给 gse agent
- `libgse/output/bkpipe*` / `libgse/output/bkpush` / `libgse/output/otlp` — 同样输出通道

替代方案：抽 `Output` interface，提供 stdout / console / file / HTTP / OpenTelemetry 实现。

### 4.2 多租户 / 配置下发通道
- `pkg/bkmonitorbeat/tenant/` — 走 `agent-message` SDK 从 gseagent 拉配置

替代方案：HTTP 文件 watcher + 本地热重载 + SIGUSR1 reload（`libgse/reloader` 已是此机制）。

### 4.3 主机身份（CMDB hostid）
- `pkg/utils/host/watcher.go` — 解析 `/var/lib/gse/host/hostid` JSON
- 字段：`bk_host_id` / `bk_cloud_id` / `bk_biz_id` / `layer` / `associations`

替代方案：`NewEmptyWatcher()` 已存在，可直接复用；可选注入 file-based 简单 identifier。

### 4.4 CMDB 多层级链路展开
- `tasks/cmdb.go` — `CmdbEventSender.DuplicateRecordByCMDBLevel`，按主机/服务实例层级复制事件

处理：删除或改为纯可选扩展。

### 4.5 采集器主进程心跳
- `beater/configengine.go` + `libgse/output/gse.RegisterHostWatcher` — 心跳与 GSE agent 绑定

替代：自管心跳。

### 4.6 模板渲染
- `support-files/templates/` — 节点管理下发的 BK 模板

替代：标准 YAML。

### 4.7 杂项
- `libgse/common`（部分别名 `MapStr = libbeat.MapStr`）— 保留工具函数，类型换 `map[string]interface{}`
- `libgse/beat`（事件总线）— 抽 `EventBus` interface，console 实现即可
- `libgse/storage`（本地 KV）— 直接搬，无 BK 业务
- `libgse/reloader`（SIGUSR1 配置热重载）— 直接搬
- `libgse/debug`（pprof 接口）— 直接搬

---

## 5. 可直接复用的"干净内核"

```
monitorbeat/
├── configs/         # 全 27 个 task config 结构 + 全局 Config（去掉 NodeID/BizID/CloudID/HostIDPath/CmdbLevel 等字段）
├── define/          # Event/Task/Status/Config 接口（去掉 libbeat 依赖）
├── scheduler/
│   ├── daemon/      # 全部 treemap 时间堆 + queue + job
│   ├── cron/        # gron
│   ├── checker/     # 探测型轮询
│   ├── listen/      # 监听型
│   └── keyword/     # 日志 input→processor→sender pipeline
├── tasks/           # 27 个采集 task
├── utils/           # atomic/cgroup/file/match/pipes/recover/reflect/tempfile/time/conv/...
├── internal/
│   ├── host/        # NewEmptyWatcher()（删 hostid 解析或降为可选）
│   ├── eventbus/    # 替代 libgse/beat
│   ├── output/      # 替代 libgse/output/gse：console/file/HTTP/OTLP 多实现
│   └── reloader/    # SIGUSR1 配置热重载
└── http/admin/      # pprof + debug（来自 libgse/debug）
```

---

## 6. 按"用户场景"重新归类（剥离后对外能力）

| 类别 | 能力 | 包含 task |
|---|---|---|
| **拨测** | 主动网络探测 | `ping` `tcp` `udp` `http` `snmptrap` |
| **日志采集** | 文件滚动 + 字段提取 + 关键字聚合 | `keyword`（raw_log + keyword 双模式） |
| **脚本采集** | 任意脚本/二进制 → 指标 | `script`（Prometheus 文本 / 自定义 formatdata） |
| **主机指标** | CPU/内存/磁盘/网络/IO/负载 | `basereport` `static` `selfstats` |
| **进程指标** | 进程性能/状态/二进制/端口 | `processbeat` `procstatus` `procbin` `procsnapshot` `socketsnapshot` |
| **进程配置同步** | 从配置生成子任务 | `procconf` `procsync` `proccustom` — 需重写：默认读 YAML/JSON |
| **系统事件** | 异常/内核/登录 | `exceptionbeat` `dmesg` `timesync` `loginlog` `shellhistory` `rpmpackage` |
| **容器** | K8s 事件 + Prometheus 拉取 | `kubeevent` `metricbeat` |
| **可观测自身** | 采集器自监控 | `gather_up_beat` `selfstats` `heart_beat`（内置自管） |
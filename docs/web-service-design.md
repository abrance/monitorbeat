# monitorbeat Web 服务 + 前端设计（P3）

> 状态：设计 + MVP 实现。数据路径选定 **对接 VictoriaMetrics**，前端 **React + Vite**。

## 1. 目标

为 monitorbeat 提供一个自包含的 Web 可视化层：

- 复用 monitorbeat 已有的 `http` output（`format: victoriametrics`）把指标写入 VictoriaMetrics（VM）。
- `monitorweb` 是一个独立 Go 二进制，作为 **VM PromQL 查询代理 + 静态前端服务**，自身不存储数据。
- 前端是 React + Vite SPA，构建产物由 Go 直接托管。
- agent 端**零代码改动**：只需在配置里加一个 `outputs` 指向 VM 即可。

## 2. 架构

```
┌─────────────┐   http output (format: victoriametrics)   ┌──────────────────┐
│ monitorbeat │ ───────────────────────────────────────▶ │ VictoriaMetrics  │
│  (agent)    │                                           │  (外部 TSDB)     │
└─────────────┘                                           └────────┬─────────┘
                                                              PromQL │ /api/v1/*
                                                                     │
                                                          ┌──────────▼─────────┐
                                                          │   monitorweb      │
                                                          │  (Go binary)      │
                                                          │  - VM 查询代理     │
                                                          │  - /api/v1 REST   │
                                                          │  - 托管 SPA        │
                                                          └────────┬─────────┘
                                                                   │  HTTP
                                                          ┌────────▼─────────┐
                                                          │  React SPA       │
                                                          │  (web/ui/dist)   │
                                                          └──────────────────┘
```

数据流：
1. agent 按现有管线把每个 `Event` 转成 VM import 格式（`__name__=metric`, 维度作为 label）。
2. VM 存储时序。
3. `monitorweb` 把前端请求翻译成 PromQL，转发 VM，整形后返回 JSON。
4. React 渲染仪表盘。

## 3. 数据模型映射（VM 侧）

monitorbeat `formatVictoriaMetrics` 规则：
- 有 `metrics` 的事件（basereport / processbeat / metricbeat / selfstats）：每个 metric 一行，`__name__=metric名`，label = `dimensions`（含 `hostname`, `os`, `platform`, `kernel_version`, `arch`）。
- 无 `metrics` 的事件（exceptionbeat / dmesg / keyword）：发射 `event类型` 为 `__name__`、value=1 的计数器，label = dimensions。结构化明细（磁盘列表、corefile 路径等）在此形态下丢失，仅保留"是否发生"。

已知维度 label：`hostname`, `os`, `platform`, `kernel_version`, `arch`。

已知关键指标（basereport）：`cpu_usage`, `mem_used_percent`, `mem_used_bytes`, `mem_available_bytes`, `disk_/_used_percent`（路径经 sanitize，`/`→`_`，如根分区=`disk___used_percent`）, `disk_<path>_used_bytes`, `load1`, `load5`, `load15`, `net_bytes_sent`, `net_bytes_recv`, `net_packets_sent`, `net_packets_recv`。

> MVP 范围：指标类（basereport/processbeat/metricbeat/selfstats）完整可视化；结构化异常明细（exceptionbeat/dmesg/keyword）以"计数时间序列"形式展示（是否发生 + 次数），明细钻取为后续迭代（可选 json 摄入通道）。

## 4. API 契约（`monitorweb` → 前端）

Base path: `/api/v1`。所有响应 `application/json`。

### 4.1 `GET /api/v1/hosts`
返回受监控主机清单。
```json
[
  {"hostname":"web-01","os":"linux","platform":"ubuntu","arch":"x86_64",
   "kernel_version":"5.15.0","last_seen":1718000000}
]
```
实现：`/api/v1/label/hostname/values` 取主机名；`max by (hostname) (timestamp(<任一basereport指标>))` 取 last_seen（秒）。聚合 os/platform/arch 取最近一次样本。

### 4.2 `GET /api/v1/host/:host/summary`
主机当前快照（instant query）。
```json
{"hostname":"web-01","ts":1718000000,
 "cpu_usage":12.3,"mem_used_percent":54.1,
 "disk_root_used_percent":61.0,"load1":0.5,
 "net_bytes_recv":123456,"net_bytes_sent":654321}
```
实现：对 `cpu_usage{hostname}`, `mem_used_percent{hostname}`, `disk___used_percent{hostname}`, `load1{hostname}` 等做 instant `query`，取最新值。

### 4.3 `GET /api/v1/query/range`
时序查询（支持多指标）。
Query: `?host=web-01&metric=cpu_usage&metric=mem_used_percent&from=1717990000&to=1718000000&step=60`
```json
[{"metric":"cpu_usage","unit":"%","points":[[1717990000,12.3],[1717990060,13.1]]},
 {"metric":"mem_used_percent","unit":"%","points":[[1717990000,54.1],...]}]
```
实现：对每个 `metric` 调 VM `/api/v1/query_range?query=<metric>{hostname="x"}&start=&end=&step=`，整形为 `[ts,value]`。

### 4.4 `GET /api/v1/metrics/names?host=web-01`
返回该主机可用指标名列表（用于前端动态发现）。
实现：VM `/api/v1/label/__name__/values`，按主机过滤（可选），排除纯事件计数器（可选）。

### 4.5 `GET /api/v1/events?host=&type=exceptionbeat_event&from=&to=&step=`
异常/事件计数时间序列。
```json
{"type":"exceptionbeat_event","points":[[1717990000,0],[1717990060,1]]}
```
实现：`sum by (hostname) (count_over_time(<type>{hostname="x"}[step]))` 或 `increase(<type>{hostname="x"}[step])`。

### 4.6 `GET /api/v1/probes?host=&from=&to=&step=`
探测结果（ping/tcp/udp/http）成功率 + 延迟。
实现：若 agent 发射了探测指标（如 `probe_up`、`probe_duration_seconds`），做 `avg(probe_up)` 与 `avg(probe_duration_seconds)` 的 range 查询。MVP 若指标未发射则前端优雅降级为空。

### 代理转发说明
`monitorweb` 持有 VM base URL（配置项）。每个 `/api/v1/*` handler 构造对应 PromQL，转发 `GET <vm>/api/v1/query` 或 `/api/v1/query_range` 或 `/api/v1/label/.../values`，解析 VM JSON（`{.data.result[]}`），整形为上述契约。超时/错误返回 502 + 错误体。

## 5. 目录结构（新增，不动现有代码）

```
cmd/monitorweb/main.go        # 启动：加载配置 → 建 VM client → 挂 API + SPA → 监听
web/
  config/config.go            # WebConfig 结构 + yaml 加载 + Clean()
  vm/client.go                # VictoriaMetrics PromQL HTTP 客户端（query/queryRange/labelValues）
  api/server.go               # 路由 + 各 handler（hosts/summary/range/names/events/probes）
  api/handlers_*.go           # 各端点实现
  ui/                         # React + Vite 前端（独立 package.json）
    package.json
    vite.config.ts            # base: './' 便于 Go 子路径托管
    index.html
    src/
      main.tsx
      api/client.ts           # fetch /api/v1/*
      pages/Overview.tsx      # 主机卡片网格
      pages/HostDetail.tsx    # 时序图 + 进程表 + 事件
      pages/Probes.tsx        # 探测状态
      components/*.tsx        # Chart, HostCard, StatCard...
      types.ts
    dist/                     # 构建产物（被 Go 托管）
web/configs/web.yaml          # 默认配置
```

## 6. 配置 `web/configs/web.yaml`

```yaml
listen: "0.0.0.0:8080"
victoriametrics:
  url: "http://vmtest-1-victoria-metrics-cluster-vmselect.bkbase-test.svc.cluster.local:8481/select/0/prometheus"
  timeout: 15s
ui_dir: "./web/ui/dist"      # 静态前端目录；也可 embed 进二进制
```

## 7. 前端页面（React + Vite + TS）

- **Overview**：主机卡片网格。每张卡显示 hostname、在线状态（last_seen 距今 < 2*采集周期 视为在线）、CPU/内存/磁盘 当前值 + 迷你 sparkline。顶部全局统计（主机数、异常数）。
- **HostDetail**（`/host/:hostname`）：
  - 顶部 StatCard：CPU%、内存%，磁盘%，load1/5/15。
  - 时序折线图（uplot 或 recharts）：可选指标多曲线。
  - 进程表（processbeat 指标，若可用）。
  - 事件/异常时间线（events 计数）。
- **Probes**：ping/tcp/http 成功率 + 延迟趋势。
- 技术：React 18 + Vite + TypeScript；图表用 `uplot`（轻量）或 `recharts`；`fetch` 调用 `/api/v1`；相对 base（`base: './'`）确保被 Go 子路径托管。

## 8. 构建与运行

```bash
# 1. 构建前端
cd web/ui && npm install && npm run build   # 产出 dist/

# 2. 构建并运行 web 服务
make monitorweb                              # go build ./cmd/monitorweb
./bin/monitorweb -config web/configs/web.yaml

# 3. agent 接入 VM（现有能力，零改动）
# 在 monitorbeat 配置加：
#   outputs:
#     - type: http
#       url: "http://vmtest-1-victoria-metrics-cluster-vminsert.bkbase-test.svc.cluster.local:8480/insert/0/prometheus/api/v1/import"
#       method: POST
#       format: victoriametrics
# 启动 VM：./victoria-metrics-prod  (或 docker run victoriametrics/victoria-metrics)
```

Makefile 增加：`web-ui`（npm build）、`monitorweb`（go build）、`web`（两者）。

## 9. 后续迭代（非 MVP）

- 结构化异常明细：增加 `json` 格式摄入通道（web 服务收 `/api/ingest` 存 SQLite/追加 VM 的 `logs`）。
- 认证：VM 与 web 间加 token；web 自身登录。
- 告警规则展示。
- 多 VM 数据源。

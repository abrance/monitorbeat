# monitorbeat Web 告警策略 + 邮件推送 + 告警确认设计 (P3)

> 状态：设计稿。面向 monitorweb (P3)，在现有 VM 查询代理架构上增加告警引擎。
> 不修改 monitorbeat agent 代码。

## 1. 目标

在 monitorweb 内建一个**自包含的告警引擎**，满足：

1. 用户通过 Web UI 配置告警规则（阈值规则）
2. 定期评估规则，从 VictoriaMetrics 查询指标，判断是否触发
3. 触发后推送邮件通知
4. 用户可在 Web UI 中标记告警为"处理中"，停止后续推送
5. 可选静默时长，静默期间不推送
6. 恢复后自动解除确认状态

不依赖外部服务（除 VictoriaMetrics 和 SMTP 外），不修改 agent。

## 2. 架构

```
                          ┌──────────────────────┐
  React SPA ──REST──▶     │     monitorweb        │
                          │                       │
                          │  /api/v1/alerts/*     │
                          │                       │
                          │  ┌─────────────────┐  │
                          │  │ Alert Evaluator  │──▶ PromQL query ▶ VictoriaMetrics
                          │  │ (background loop)│  │
                          │  │                 │  │
                          │  │ threshold check  │  │
                          │  │ state transition │  │
                          │  └────────┬────────┘  │
                          │           │ fire       │
                          │           ▼            │
                          │  ┌──────────────────┐  │
                          │  │   SMTP Sender    │──▶ Email
                          │  └──────────────────┘  │
                          │           │            │
                          │  ┌──────────────────┐  │
                          │  │  SQLite Store    │  │
                          │  │ alert_rules      │  │
                          │  │ alert_history    │  │
                          │  │ alert_state      │  │
                          │  └──────────────────┘  │
                          └────────────────────────┘
```

**数据流（评估周期）**：

```
1. 加载所有 enabled rules
2. 对每条 rule，构造 PromQL: <metric>{hostname="<host>"}(或全部主机)
3. 调 VM instant query，取当前值
4. 比较阈值，更新 alert_state:
   - 首次越界 → pending (记录开始时间)
   - pending > duration → firing → 写 history + 发邮件
   - 已 acknowledged → 跳过邮件
   - 已 silence_until → 跳过邮件
   - 值恢复 → recovered → 写 history + 发恢复邮件 + 清除 ack/silence
```

## 3. 数据模型

### 3.1 alert_rules（持久化，SQLite）

| 列 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增 |
| name | TEXT NOT NULL | 规则名称，如"CPU 过高告警" |
| enabled | INTEGER NOT NULL DEFAULT 1 | 1=启用, 0=禁用 |
| metric | TEXT NOT NULL | PromQL 指标名，如 `cpu_usage` |
| hostname | TEXT DEFAULT '' | 空=所有主机，非空=指定主机 |
| condition | TEXT NOT NULL | `gt` 或 `lt` |
| threshold | REAL NOT NULL | 阈值 |
| duration | INTEGER NOT NULL DEFAULT 0 | 持续秒数，0=立即触发 |
| description | TEXT DEFAULT '' | 规则描述 |
| created_at | TEXT NOT NULL | ISO 8601 |
| updated_at | TEXT NOT NULL | ISO 8601 |

UNIQUE(name)。

### 3.2 alert_history（持久化，SQLite）

| 列 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增 |
| rule_id | INTEGER NOT NULL | FK → alert_rules.id |
| rule_name | TEXT NOT NULL | 快照，规则改名后仍可追溯 |
| hostname | TEXT NOT NULL | 触发主机 |
| metric_value | REAL | 触发时的值 |
| state | TEXT NOT NULL | `firing` 或 `recovered` |
| message | TEXT | 详细描述 |
| acknowledged | INTEGER NOT NULL DEFAULT 0 | 0=未确认, 1=已确认 |
| triggered_at | TEXT NOT NULL | ISO 8601 |

INDEX(rule_id, triggered_at)。

### 3.3 alert_state（持久化 + 运行时缓存，SQLite）

状态仅三种：`ok` / `pending` / `firing`。确认和静默是独立字段，不增加状态种类。

| 列 | 类型 | 说明 |
|---|---|---|
| rule_id | INTEGER NOT NULL | PK part1 |
| hostname | TEXT NOT NULL | PK part2 |
| status | TEXT NOT NULL | `ok` / `pending` / `firing` |
| last_value | REAL | 最近一次评估值 |
| pending_since | TEXT | pending 开始时间 (ISO 8601) |
| firing_since | TEXT | firing 开始时间 (ISO 8601) |
| acknowledged_at | TEXT | 用户确认时间，NULL=未确认。非空时 evaluator 跳过邮件 |
| silence_until | TEXT | 静默截止时间，NULL=无静默。未到期时 evaluator 跳过邮件 |
| last_notified_at | TEXT | 上次邮件时间，用于速率控制 |

PRIMARY KEY(rule_id, hostname)。

## 4. 告警状态机

```
                      value > threshold              duration exceeded
              ┌──────────────────────┐    ┌───────────────────┐
              ▼                      │    │                   ▼
        ┌─────────┐            ┌──────────┐            ┌──────────┐
        │   ok    │──pending──▶│ pending  │──firing──▶│  firing  │──▶ 发邮件
        └─────────┘            └──────────┘            └────┬─────┘
              ▲                                              │
              │                         ┌────────────────────┤
              │                         │                    │
              │                   用户标记处理中          value <= threshold
              │                         │                    │
              │                         ▼                    ▼
              │                  acknowledged_at  ←──  clear ack
              │                  silence_until         + silence_until
              │                  跳过邮件                  + 发恢复邮件
              │                                              │
              │                 silence 到期( && 仍 firing )  │
              │                                              │
              └───────── value <= threshold ─────────────────┘
                 (任何状态 → ok, 清理 ack/silence)
```

## 5. 告警评估器 (Evaluator)

### 启动
- `cmd/monitorweb/main.go` 在 HTTP server 启动后，goroutine 启动 evaluator。
- 如果 SQLite 初始化失败，log warning 但不阻塞 monitorweb 启动（告警不可用，其余正常）。

### 评估循环
```
interval = config.alerts.eval_interval (默认 60s)
loop:
  sleep(interval)
  rules = store.GetEnabledRules()
  for rule in rules:
    if rule.hostname == "":
      query = rule.metric                                 // 全部主机
    else:
      query = fmt.Sprintf(`%s{hostname="%s"}`, rule.metric, rule.hostname)
    results = vm.Query(ctx, query)                        // instant query
    for vector in results:
      host = vector.Metric["hostname"]
      value = vector.Value[1]
      state = store.GetState(rule.id, host)               // 当前状态
      breached = (condition=="gt" && value > threshold) ||
                 (condition=="lt" && value < threshold)
      now = time.Now()
      switch state.status:
        case "ok":
          if breached:
            store.SetState(rule.id, host, "pending", value, pending_since=now)
        case "pending":
          if breached:
            if now - pending_since >= rule.duration:
              store.SetState(rule.id, host, "firing", value, firing_since=now)
              fire(rule, host, value)
            // else: remain pending
          else:
            store.SetState(rule.id, host, "ok", value)
        case "firing":
          if breached:
            shouldSend := true
            if state.acknowledged_at != nil:
              shouldSend = false              // 用户已确认，跳过邮件
            elif state.silence_until != nil && now < *state.silence_until:
              shouldSend = false              // 静默期，跳过邮件
            else:
              // rate limit: 距上次通知不足 5 分钟
              if state.last_notified_at != nil &&
                 now.Sub(*state.last_notified_at) < 5*time.Minute:
                shouldSend = false

            // silence 到期 → 清除静默，允许下周期发
            if state.silence_until != nil && now >= *state.silence_until:
              store.SetSilenceUntil(rule.id, host, nil)
              shouldSend = true               // 到期后允许发

            if shouldSend:
              fire(rule, host, value)
          else:
            recover(rule, host, value)
            store.ClearAck(rule.id, host)
            store.SetState(rule.id, host, "ok", value)
```

### fire() 动作
1. 写 alert_history (state="firing")
2. 发邮件
3. 更新 state.last_notified_at

### recover() 动作
1. 写 alert_history (state="recovered")
2. 发恢复通知邮件（可选，默认发）
3. 清除 state.acknowledged_at, state.silence_until

## 6. SMTP 邮件

### 配置 (`web/configs/web.yaml`)

```yaml
alerts:
  eval_interval: 60s
  db_path: "./data/alerts.db"          # SQLite 文件路径

smtp:
  host: "smtp.example.com"
  port: 587
  username: "alert@example.com"
  password: "${SMTP_PASSWORD}"         # 支持环境变量引用
  from: "monitorbeat <alert@example.com>"
  to: ["admin@example.com"]
  insecure: false                      # allow TLS without cert verification
```

### 邮件模板

Subject:
```
[MONITOR] FIRING: {rule_name} @ {hostname} - {metric_value}
[MONITOR] RECOVERED: {rule_name} @ {hostname} - {metric_value}
```

Body (HTML):
```html
<h2>告警通知</h2>
<table>
  <tr><td>规则</td><td>{rule_name}</td></tr>
  <tr><td>主机</td><td>{hostname}</td></tr>
  <tr><td>指标</td><td>{metric} = {value}</td></tr>
  <tr><td>条件</td><td>{condition} {threshold}</td></tr>
  <tr><td>状态</td><td>{state}</td></tr>
  <tr><td>时间</td><td>{time}</td></tr>
</table>
<p>请到 monitorweb 确认此告警：<a href="{web_url}">{web_url}</a></p>
```

### 速率控制

- 同一 rule + hostname 至少间隔 5 分钟推送一次（由 last_notified_at 控制）
- 被 acknowledged 或 silenced 的不推送
- SMTP 连接失败 log error 但不阻塞评估循环

## 7. API 端点

所有端点 `/api/v1/alerts/*`，返回 `application/json`。

### 7.1 规则 CRUD

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/v1/alerts/rules` | 列出所有规则（含当前 firing 状态和确认状态） |
| `POST` | `/api/v1/alerts/rules` | 创建规则 |
| `PUT` | `/api/v1/alerts/rules/{id}` | 更新规则（禁用/启用也用此接口） |
| `DELETE` | `/api/v1/alerts/rules/{id}` | 删除规则（同时清理相关 state 和 history） |

`POST/PUT` body:
```json
{
  "name": "CPU 过高告警",
  "enabled": true,
  "metric": "cpu_usage",
  "hostname": "",
  "condition": "gt",
  "threshold": 90,
  "duration": 60,
  "description": "CPU 使用率超过 90% 持续 60 秒"
}
```

`GET` response:
```json
[
  {
    "id": 1,
    "name": "CPU 过高告警",
    "enabled": true,
    "metric": "cpu_usage",
    "hostname": "",
    "condition": "gt",
    "threshold": 90,
    "duration": 60,
    "description": "...",
    "created_at": "2026-07-16T10:00:00Z",
    "updated_at": "2026-07-16T10:00:00Z",
    "states": [                     // 当前各主机状态
      {"hostname": "web-01", "status": "firing", "acknowledged": false, "silence_until": null, "last_value": 95.2},
      {"hostname": "web-02", "status": "ok", "acknowledged": false, "silence_until": null, "last_value": 45.1}
    ]
  }
]
```

### 7.2 告警确认

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/api/v1/alerts/acknowledge` | 确认告警（标记处理中） |

Body:
```json
{
  "rule_id": 1,
  "hostname": "web-01",
  "silence_hours": 0       // 可选：0=仅当前 firing, >0=静默N小时
}
```

响应: `{"status": "acknowledged"}`

### 7.3 告警历史

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/v1/alerts/history` | 历史记录（分页） |

Query params: `rule_id=N&hostname=xxx&state=firing&limit=20&offset=0`

响应:
```json
{
  "total": 100,
  "items": [
    {
      "id": 1,
      "rule_id": 1,
      "rule_name": "CPU 过高告警",
      "hostname": "web-01",
      "metric_value": 95.2,
      "state": "firing",
      "acknowledged": true,
      "triggered_at": "2026-07-16T10:30:00Z"
    }
  ]
}
```

### 7.4 当前告警状态

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/v1/alerts/status` | 所有规则的当前 firing 状态汇总 |

响应:
```json
{
  "firing_count": 2,
  "acknowledged_count": 1,
  "items": [
    {"rule_id": 1, "rule_name": "CPU 过高", "hostname": "web-01", "status": "firing", "acknowledged": false, "since": "..."},
    {"rule_id": 2, "rule_name": "内存不足", "hostname": "web-01", "status": "acknowledged", "acknowledged": true, "since": "..."}
  ]
}
```

## 8. 前端 (React SPA)

### 8.1 页面路由

| 路径 | 组件 | 说明 |
|---|---|---|
| `/alerts` | AlertRules | 告警规则列表 + 管理 |
| `/alerts/history` | AlertHistory | 告警历史 |

### 8.2 AlertRules 页面

- **规则列表**: 表格展示所有规则
  - 列: 名称, 指标, 条件, 阈值, 持续, 启禁开关, 当前状态(OK/Firing/Acknowledged/Disabled), 操作
  - 当前状态取自 state 汇总：如果任一主机 firing → 显示 "Firing" (红色), 如果全部 acked → "Acknowledged" (黄色), 全部 ok → "OK" (绿色), disabled → "Disabled" (灰色)
- **创建规则**: 按钮打开模态表单
- **编辑规则**: 行操作按钮打开编辑模态
- **禁用/启用**: inline toggle switch
- **删除**: 确认后删除
- **查看当前 firing 主机**: 展开行或 tooltip 显示每个主机的状态

### 8.3 AlertHistory 页面

- **历史列表**: 分页表格
  - 列: 规则名, 主机, 值, 状态(firing/recovered), 确认状态, 时间
- **筛选**: 按规则名、主机、状态(firing/recovered)、时间范围
- **确认操作**: 对 firing 状态且未确认的历史记录，可直接在此页面标记处理中

### 8.4 告警确认交互

- 在 AlertRules 页面，对状态为 "Firing" 的规则行，显示 "处理中" 按钮
- 点击按钮弹出确认对话框：默认 "仅当前 firing" + 可选 "静默 N 小时" 下拉
- 确认后，该规则对应主机的状态变为 "Acknowledged" (黄色)
- 规则行状态的 badge 从红色变为黄色

### 8.5 导航更新

Nav 组件增加 "Alerts" 链接，如果当前有 firing 告警，显示红色数字角标。

### 8.6 Types

```typescript
export interface AlertRule {
  id: number
  name: string
  enabled: boolean
  metric: string
  hostname: string
  condition: 'gt' | 'lt'
  threshold: number
  duration: number
  description: string
  created_at: string
  updated_at: string
  states: AlertState[]
}

export interface AlertState {
  hostname: string
  status: 'ok' | 'pending' | 'firing'    // 无独立 acknowledged 状态
  acknowledged: boolean                    // 由 acknowledged_at 是否非空推导
  silence_until: string | null
  last_value: number
}

export interface AlertHistoryItem {
  id: number
  rule_id: number
  rule_name: string
  hostname: string
  metric_value: number
  state: 'firing' | 'recovered'
  acknowledged: boolean
  triggered_at: string
}

export interface AlertStatus {
  firing_count: number
  acknowledged_count: number
  items: {
    rule_id: number
    rule_name: string
    hostname: string
    status: string
    acknowledged: boolean
    since: string
  }[]
}
```

## 9. 配置变更

### `web/config/config.go` 新增

```go
type WebConfig struct {
    Listen          string          `yaml:"listen"`
    VictoriaMetrics VictoriaMetrics `yaml:"victoriametrics"`
    Alert           AlertConfig     `yaml:"alerts"`
    SMTP            SMTPConfig      `yaml:"smtp"`
    UIDir           string          `yaml:"ui_dir"`
}

type AlertConfig struct {
    EvalInterval time.Duration `yaml:"eval_interval"`
    DBPath       string        `yaml:"db_path"`
}

type SMTPConfig struct {
    Host     string   `yaml:"host"`
    Port     int      `yaml:"port"`
    Username string   `yaml:"username"`
    Password string   `yaml:"password"`
    From     string   `yaml:"from"`
    To       []string `yaml:"to"`
    Insecure bool     `yaml:"insecure"`
}
```

### `web/configs/web.yaml` 新增 section

```yaml
alerts:
  eval_interval: 60s
  db_path: "./data/alerts.db"

smtp:
  host: "smtp.example.com"
  port: 587
  username: "alert@example.com"
  password: "${SMTP_PASSWORD}"
  from: "monitorbeat <alert@example.com>"
  to:
    - "admin@example.com"
  insecure: false
```

SMTP password 支持 `${ENV_VAR}` 语法，运行时替换。如果 smtp 段缺失或 host 为空，告警引擎启动但跳过邮件发送（仅记录到 history）。

## 10. 新增/修改文件清单

```
NEW  web/alerts/store.go          — SQLite 初始化 + CRUD (rules, history, state)
NEW  web/alerts/models.go         — Go structs (AlertRule, History, State)
NEW  web/alerts/evaluator.go      — 后台评估循环
NEW  web/alerts/evaluator_test.go — evaluator 单元测试
NEW  web/smtp/sender.go           — SMTP 邮件发送
NEW  web/smtp/sender_test.go      — SMTP 单元测试
NEW  web/api/alerts.go            — alert 相关 HTTP handlers
MOD  web/api/server.go            — 注册 /api/v1/alerts/* 路由
MOD  web/config/config.go         — 添加 AlertConfig, SMTPConfig
MOD  web/configs/web.yaml         — 添加 alert + smtp 配置
MOD  cmd/monitorweb/main.go       — 初始化 store, 启动 evaluator
MOD  go.mod / go.sum              — 添加 modernc.org/sqlite
NEW  web/ui/src/pages/Alerts.tsx           — 告警规则管理页
NEW  web/ui/src/pages/AlertHistory.tsx     — 告警历史页
MOD  web/ui/src/api/client.ts              — 添加 alert API 调用
MOD  web/ui/src/types.ts                   — 添加 alert 类型
MOD  web/ui/src/components/Nav.tsx         — 添加 Alerts 导航链接 + 未确认角标
MOD  web/ui/src/main.tsx                   — 添加 /alerts, /alerts/history 路由
```

## 11. Go 依赖

- `modernc.org/sqlite` — pure Go SQLite3 driver (no CGO)
- `database/sql` — Go stdlib

邮件用 `net/smtp`，模板用 `html/template`，均为 Go stdlib。

## 12. 安全考量

- **PromQL 注入**: metric 和 hostname 字段在 API handler 中做 `strings.ReplaceAll` 转义 `\` 和 `"`，复用现有 `labelMatch` 函数
- **SQL 注入**: 使用 `?` 参数化查询（`database/sql` 原生支持）
- **SMTP 密码**: 配置中通过 `${ENV_VAR}` 引用环境变量，不硬编码
- **TLS**: SMTP 默认要求 STARTTLS，`insecure: true` 跳过证书验证（仅用于调试）

## 13. 边界情况

| 场景 | 处理 |
|---|---|
| SQLite 文件目录不存在 | 自动创建目录 |
| SMTP 连接失败 | log error, 下一周期重试, 不阻塞评估 |
| 规则引用的指标在 VM 中不存在 | PromQL 返回空 → 视为未越界 (ok) |
| 多个主机同时触发同一规则 | 每个 hostname 独立 state, 各自发送邮件 |
| 规则删除后正在 firing | 清理 state 和 history, 不触发恢复通知 |
| 规则更新后(改阈值) pending 状态 | 重置对应 state 为 ok, 重新评估 |
| 指标偶尔缺失(采集间隙) | VM 返回空, evaluator 视为 ok, 不产生恢复通知(因为之前没 firing) |
| monitorweb 重启 | state 从 SQLite 恢复, 正在 firing 的规则在下一周期重新发通知（受 last_notified_at 速率控制） |

## 14. 非目标 (v1 不实现)

- 告警分组/聚合
- 多通知渠道 (Slack, Webhook, 钉钉等)
- 告警静默计划 (暂仅支持手动确认的静默时长)
- 告警升级策略
- Dashboard 内嵌告警注解
- Prometheus AlertManager 兼容

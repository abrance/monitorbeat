# 告警规则重构设计：PromQL-only 告警

## 背景

当前告警规则只能配置单指标名 + 阈值条件，无法支持维度过滤、聚合计算、窗口函数。

例如，无法表达：

```promql
packet_loss_percent{probe_type="ping",target="186.241.120.132"} > 0
```

```promql
avg_over_time(success{probe_type="http",target="https://monitorbeat.xiaoyxq.top/api/v1/healthz"}[5m]) < 1
```

而且当前 evaluator 仅按 `(rule_id, hostname)` 维护告警状态，ICMP/HTTP 拨测这种多目标场景下：
- 不能区分同一 host 上的多个 target。
- PromQL 恢复时返回空结果无法从历史 firing 实例恢复。

## 目标

- 告警规则只接受 PromQL 表达式作为告警条件，废弃 metric/condition/threshold/hostname 字段。
- 旧规则和历史数据不迁移、不保留，可直接丢失。
- 告警实例按 PromQL 返回 series 的 labels 生成 fingerprint，恢复通过差集触发。
- 提供测试查询 API 与前端按钮，便于用户验证表达式匹配范围。
- 邮件模板改为展示表达式 + 实例 labels。

## 范围

包含：
- 后端 Go 端告警模型、evaluator、API、邮件模板重构。
- 前端告警页面、API client、状态展示调整。
- 新增 `/api/v1/alerts/test-query` 与前端测试按钮。
- 删除旧字段相关前后端逻辑。

不包含：
- 旧规则数据迁移、自动转换。
- 多租户、权限。
- Webhook/钉钉/企业微信等新通知渠道。

## 设计

### 1. 规则模型

```go
type AlertRule struct {
    ID          int64        `json:"id"`
    Name        string       `json:"name"`
    Enabled     bool         `json:"enabled"`
    Expr        string       `json:"expr"`
    Duration    int64        `json:"duration"`
    Description string       `json:"description"`
    CreatedAt   time.Time    `json:"created_at"`
    UpdatedAt   time.Time    `json:"updated_at"`
    States      []AlertState `json:"states"`
}
```

`AlertState` 改为：

```go
type AlertState struct {
    RuleID         int64
    Fingerprint    string
    Labels         map[string]string
    Hostname       string
    Status         string
    LastValue      float64
    PendingSince   *time.Time
    FiringSince    *time.Time
    AcknowledgedAt *time.Time
    SilenceUntil   *time.Time
    LastNotifiedAt *time.Time
}
```

`Hostname` 字段冗余自 `Labels["hostname"]`，仅作 UI 便捷展示。

### 2. PromQL 表达式语义

- 表达式必须返回 vector，结果里每条 series 即一个当前异常实例。
- 返回空 vector = 没有异常。
- VM 查询失败 = 保持现有状态，不触发、不恢复，仅打日志。

### 3. 数据库 schema

破坏性迁移：检测到旧表时 drop 后重建。

```sql
CREATE TABLE alert_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 1,
    expr TEXT NOT NULL,
    duration INTEGER NOT NULL DEFAULT 0,
    description TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE alert_state (
    rule_id INTEGER NOT NULL,
    fingerprint TEXT NOT NULL,
    labels_json TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'ok',
    last_value REAL NOT NULL DEFAULT 0,
    pending_since TEXT,
    firing_since TEXT,
    acknowledged_at TEXT,
    silence_until TEXT,
    last_notified_at TEXT,
    PRIMARY KEY (rule_id, fingerprint)
);

CREATE TABLE alert_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL,
    rule_name TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    labels_json TEXT NOT NULL,
    expr TEXT NOT NULL,
    metric_value REAL NOT NULL DEFAULT 0,
    state TEXT NOT NULL,
    acknowledged INTEGER NOT NULL DEFAULT 0,
    triggered_at TEXT NOT NULL
);

CREATE INDEX idx_history_rule ON alert_history(rule_id, triggered_at);
```

### 4. Fingerprint

对 labels 按 key 排序后拼接 `key=value`，再 SHA-256：

```text
sha256("hostname=node-a\nprobe_type=http\ntarget=https://...\ntask_id=1002\n")
```

用于 `(rule_id, fingerprint)` 唯一区分告警实例。

### 5. Evaluator 状态流转

每轮 evaluate：

1. 查询 `rule.Expr`。
2. 构建当前异常集合 `current: { fingerprint -> (labels, value) }`。
3. 读取规则所有 `pending`/`firing` 状态 `historical`。
4. 差集：

   | 当前 | 历史 | 动作 |
   |---|---|---|
   | 有 | 无 | 写入 `pending`，记录 `pending_since` |
   | 有 | pending | 超过 `duration` → 写 `firing`，发触发邮件；否则保持 pending |
   | 有 | firing | 保持 firing，按现有限频和静默逻辑触发邮件 |
   | 无 | pending | 置为 ok |
   | 无 | firing | 写历史为 `recovered`，发恢复邮件，置为 ok |

5. 邮件跳过逻辑保留：
   - `AcknowledgedAt != nil` 且 `state == firing` 时跳过重复告警邮件。
   - `SilenceUntil != nil` 且仍在静默期内，跳过。
   - 重复告警限频：至少 5 分钟间隔。

### 6. 邮件模板

```text
规则：HTTP healthz failed
状态：FIRING / RECOVERED
表达式：success{probe_type="http",target="..."} == 0
实例：
  hostname=node-a
  probe_type=http
  target=https://...
  task_id=1002
当前值：1
时间：...
```

恢复邮件中当前值使用最后一次 firing 时的 `last_value`。

### 7. 测试查询 API

```http
POST /api/v1/alerts/test-query
```

请求：

```json
{
  "expr": "success{probe_type=\"http\"} == 0"
}
```

响应（成功）：

```json
{
  "result": [
    {
      "fingerprint": "...",
      "labels": {"hostname": "node-a", "probe_type": "http", "target": "..."},
      "value": 1
    }
  ]
}
```

响应（失败）：
- `400`：`{"error": "expr is required"}` 或 PromQL 语法错误。
- `502`：`{"error": "vm error: ..."}`。

实现复用现有 `vm.Client.Query`。

### 8. 处理中（acknowledge）

API：

```http
POST /api/v1/alerts/acknowledge
```

请求：

```json
{
  "rule_id": 1,
  "fingerprint": "abc...",
  "silence_hours": 1
}
```

响应：

```json
{"status": "acknowledged"}
```

按 `fingerprint` 写入 `acknowledged_at` 和 `silence_until`，并仅作用于 firing 实例。

### 9. UI

- 列表列：`名称 | PromQL | 持续(s) | 状态 | 启用 | 操作`
- 状态行汇总：
  - OK
  - Pending(n)
  - Firing(n)
  - 处理中(n)
- 弹窗字段：
  - 规则名称
  - PromQL textarea
  - 持续秒数
  - 描述
  - [测试查询] 按钮 + 结果预览
- 处理中弹窗对每个 firing 实例单独按钮，传递 `fingerprint`。
- 告警历史页面增加 `fingerprint`、`labels`、`expr` 显示。

### 10. 兼容与回滚

- 不保留旧 schema；升级后直接清理旧数据。
- 回滚需使用上版本镜像 + 上版本 config；不提供数据兼容。

## 错误处理

- PromQL 语法错误：API 返回 400，UI 在测试按钮处提示。
- VM 查询失败：evaluator 跳过本轮、不改变状态、记录 slog 错误。
- 同一规则被并发 evaluate：单 evaluator goroutine，无需锁。

## 测试计划

- evaluator 单测：
  - PromQL 返回 series → 新建 pending。
  - 持续超过 duration → 转 firing 并调用 fire。
  - PromQL 空 vector + 历史 firing → 调用 recover 并发恢复邮件。
  - PromQL 空 vector + 历史 pending → 置 ok。
  - VM 查询失败 → 不改状态。
- fingerprint 单测：
  - 同样 labels 同序生成一致 fingerprint。
  - 同样 labels 不同序生成一致 fingerprint。
- test-query API 单测：成功/400/502。
- SMTP sender 单测：模板包含表达式与实例 labels。
- 前端构建 + 关键交互的页面级测试。

## 实现步骤

1. 重写 `web/alerts/models.go`。
2. 重写 `web/alerts/store.go` schema 与 CRUD。
3. 重写 `web/alerts/evaluator.go`。
4. 修改 `web/api/alerts.go` 增删改 + test-query + acknowledge。
5. 修改 `web/smtp/sender.go` 模板。
6. 修改前端 `types.ts`、`Alerts.tsx`、`AlertHistory.tsx`、`api/client.ts`。
7. 补充单测与运行 `make test`、`make lint`。
8. `helm lint` 与 `helm template`。

## 验证

- `make lint`
- `make test`
- `helm lint ./deploy/helm/monitorbeat`
- `helm template monitorbeat ./deploy/helm/monitorbeat > /dev/null`
- 后端启动后手动验证：
  - 创建一条 `success{probe_type="http"} == 0` 规则。
  - 使用 test-query 验证实例匹配。
  - 模拟 Prometheus 返回异常 → 收到 firing 邮件。
  - 模拟恢复 → 收到 recovered 邮件。
  - 模拟 VM 故障 → 状态保持。
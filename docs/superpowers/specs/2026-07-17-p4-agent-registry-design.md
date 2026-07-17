# monitorbeat Agent Registry 设计 (P4)

> 状态：设计阶段，待实现。
> 面向 monitorbeat agent + monitorweb，在现有"agent → VM → web"架构上增加轻量发现层。
> 遵循"单二进制、单 SQLite 文件"原则，不新增独立服务。

---

## 1. 目标

当前 monitorweb 通过 `max by(hostname)(cpu_usage)` 隐式发现 agent 主机，存在以下缺口：

| 能力 | 现在 | 目标 |
|------|------|------|
| 知道哪些主机装了 agent | ✅ 隐式（有指标=有 agent） | ✅ 显式（主动注册） |
| agent 版本 | ❌ | ✅ |
| 跑了哪些 task | ❌（只能反推） | ✅ |
| 区分"agent 挂了"和"数据还没到" | ❌ | ✅（TTL 实时判定） |
| agent IP / K8s Node | ❌ | ✅ |
| 离线检测延迟 | ~2 采集周期（60-120s） | ≤ TTL（默认 90s） |

---

## 2. 总体架构

```
                    agent 侧                             服务端侧 (monitorweb 单二进制)
 ┌─────────────────────────────────┐    ┌──────────────────────────────────────────┐
 │ monitorbeat (DaemonSet)         │    │ monitorweb (Deployment)                   │
 │                                 │    │                                          │
 │  ┌──────────┐  ┌──────────────┐ │    │  ┌─────────────┐  ┌──────────────────┐  │
 │  │ Tasks    │─▶│ output.http  │─┼─VM─┼─▶│ VM PromQL   │  │  React SPA       │  │
 │  │ (采集)   │  │ → VM        │ │    │  │ 代理 + Alert │  │  (主机列表/详情)  │  │
 │  └──────────┘  └──────────────┘ │    │  └─────────────┘  └────────┬─────────┘  │
 │                                 │    │                            │             │
 │  ┌────────────────────────────┐ │    │  ┌──────────────────┐      │             │
 │  │ registry.Sender           │─┼─▶  │  │  Registry Store  │◀─────┘             │
 │  │ goroutine: POST heartbeat │ │    │  │  (SQLite agents) │                    │
 │  └────────────────────────────┘ │    │  └──────────────────┘                    │
 │                                 │    │                                          │
 │  Config:                        │    │  统一 SQLite (/data/monitorweb.db):       │
 │    registry:                    │    │    ├── alert_rules    (告警)              │
 │      url: http://monitorweb:8080│    │    ├── alert_history  (告警)              │
 │      interval: 30s              │    │    ├── alert_states   (告警)              │
 └─────────────────────────────────┘    │    └── agents         (注册发现,新增)     │
                                        └──────────────────────────────────────────┘
```

**关键决策**：
1. Registry 嵌入 monitorweb，不新增二进制
2. Agent 端用独立 goroutine 发送 heartbeat（不混入 Output 事件管线）
3. 统一 SQLite 文件，alert 和 registry 共享 `monitorweb.db`

---

## 3. 统一 SQLite 设计

### 3.1 文件与表

```
/data/monitorweb.db          ← 一个 PVC 挂载路径
  ├── alert_rules            (已有，alerts store)
  ├── alert_history          (已有，alerts store)
  ├── alert_states           (已有，alerts store)
  └── agents                 (新增，registry store)
```

### 3.2 共享连接

`cmd/monitorweb/main.go` 中一次 `sql.Open`，两个 Store 各维护自己的 migrate：

```go
func main() {
    db, err := openSQLite(cfg.DBPath)  // 统一的 db_path
    defer db.Close()

    // 同一连接，各自建表
    alertStore, _ := alerts.NewStore(db)
    regStore, _   := registry.NewStore(db, cfg.Registry.TTL)
}

func openSQLite(path string) (*sql.DB, error) {
    os.MkdirAll(filepath.Dir(path), 0755)
    db, err := sql.Open("sqlite", path)
    db.Exec("PRAGMA journal_mode=WAL")
    db.Exec("PRAGMA busy_timeout=5000")
    return db, err
}
```

---

## 4. Agent 侧设计

### 4.1 配置

`configs/config.go` 新增：

```go
type RegistryConfig struct {
    URL      string        `yaml:"url"`      // http://monitorweb:8080/api/v1/registry/heartbeat
    Interval time.Duration `yaml:"interval"` // 默认 30s
    Timeout  time.Duration `yaml:"timeout"`  // 默认 5s
}

type Config struct {
    // ...现有字段...
    Registry RegistryConfig `yaml:"registry"`  // 空对象=不启用
}
```

agent `configs/minimal.yaml` 示例：

```yaml
registry:
  url: http://monitorweb.monitorbeat.svc.cluster.local:8080/api/v1/registry/heartbeat
  interval: 30s
```

### 4.2 数据结构

```go
type AgentInfo struct {
    Hostname  string   `json:"hostname"`
    Version   string   `json:"version"`
    Tasks     []string `json:"tasks"`      // 当前注册的 task type
    IP        string   `json:"ip"`         // 出口 IP
    K8sNode   string   `json:"k8s_node,omitempty"`
    StartTime int64    `json:"start_time"` // unix 秒
}
```

### 4.3 Heartbeat Sender

新建 `tasks/registry/sender.go`，作为独立 goroutine 在 main.go 启动：

```go
type Sender struct {
    cfg     configs.RegistryConfig
    client  *http.Client
    info    AgentInfo
}

func New(cfg configs.RegistryConfig, version string, taskTypesFn func() []string) *Sender {
    return &Sender{
        cfg: cfg,
        client: &http.Client{Timeout: cfg.Timeout},
        info: AgentInfo{
            Hostname:  tasks.Hostname(),
            Version:   version,
            Tasks:     taskTypesFn(),
            IP:        resolveOutboundIP(),
            K8sNode:   os.Getenv("K8S_NODE"),
            StartTime: time.Now().Unix(),
        },
    }
}

func (s *Sender) Run(ctx context.Context) {
    ticker := time.NewTicker(s.cfg.Interval)
    defer ticker.Stop()
    // 启动时立即上报一次
    s.send(ctx)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            s.send(ctx)
        }
    }
}

func (s *Sender) send(ctx context.Context) {
    body, _ := json.Marshal(s.info)
    req, _ := http.NewRequestWithContext(ctx, "POST", s.cfg.URL, bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := s.client.Do(req)
    if err != nil {
        slog.Warn("registry heartbeat failed", "err", err)
        return
    }
    resp.Body.Close()
}
```

`cmd/monitorbeat/main.go` 启动代码：

```go
if cfg.Registry.URL != "" {
    regSvc := registry.New(cfg.Registry, version, func() []string {
        return tasks.RegisteredTypes()  // 返回当前所有已注册 task type
    })
    go regSvc.Run(ctx)
}
```

---

## 5. Monitorweb 侧设计

### 5.1 配置

`web/config/config.go`：

```go
type WebConfig struct {
    Listen          string          `yaml:"listen"`
    DBPath          string          `yaml:"db_path"`          // 统一 SQLite 路径
    VictoriaMetrics VictoriaMetrics `yaml:"victoriametrics"`
    Alert           AlertConfig     `yaml:"alerts"`
    SMTP            SMTPConfig      `yaml:"smtp"`
    Registry        RegistryConfig  `yaml:"registry"`
    UIDir           string          `yaml:"ui_dir"`
}

type RegistryConfig struct {
    TTL time.Duration `yaml:"ttl"` // 默认 90s
}

// AlertConfig 去掉 DBPath，统一走顶层 db_path
type AlertConfig struct {
    EvalInterval time.Duration `yaml:"eval_interval"`
}
```

`web/configs/web.yaml` 示例：

```yaml
db_path: /data/monitorweb.db

alerts:
  eval_interval: 60s

registry:
  ttl: 90s
```

### 5.2 SQLite Schema

```sql
CREATE TABLE IF NOT EXISTS agents (
    hostname   TEXT PRIMARY KEY,
    version    TEXT NOT NULL DEFAULT '',
    tasks      TEXT NOT NULL DEFAULT '',       -- JSON 数组字符串
    ip         TEXT NOT NULL DEFAULT '',
    k8s_node   TEXT NOT NULL DEFAULT '',
    start_time INTEGER NOT NULL DEFAULT 0,
    last_seen  INTEGER NOT NULL               -- unix 秒
);
CREATE INDEX IF NOT EXISTS idx_agents_last_seen ON agents(last_seen);
```

### 5.3 Registry Store

新建 `web/registry/store.go`：

```go
type Store struct {
    db  *sql.DB
    ttl time.Duration
}

func NewStore(db *sql.DB, ttl time.Duration) (*Store, error) {
    s := &Store{db: db, ttl: ttl}
    if err := s.migrate(); err != nil {
        return nil, fmt.Errorf("registry migrate: %w", err)
    }
    return s, nil
}

func (s *Store) migrate() error {
    _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS agents (...)`)
    return err
}

// Heartbeat UPSERT：存在则更新，不存在则插入
func (s *Store) Heartbeat(ctx context.Context, info AgentInfo) error {
    _, err := s.db.ExecContext(ctx, `
        INSERT INTO agents (hostname, version, tasks, ip, k8s_node, start_time, last_seen)
        VALUES (?, ?, ?, ?, ?, ?, unixepoch())
        ON CONFLICT(hostname) DO UPDATE SET
            version=excluded.version, tasks=excluded.tasks, ip=excluded.ip,
            k8s_node=excluded.k8s_node, start_time=excluded.start_time,
            last_seen=excluded.last_seen
    `, info.Hostname, info.Version, jsonTasks(info.Tasks),
       info.IP, info.K8sNode, info.StartTime)
    return err
}

// ListAgents 返回全部 agent（含 online 状态）
func (s *Store) ListAgents(ctx context.Context) ([]AgentInfo, error) {
    // SELECT * FROM agents ORDER BY hostname
    // 扫描时判断：online = (now - last_seen) < ttl
}

// CleanLoop 后台定期清理超时 agent
func (s *Store) CleanLoop(ctx context.Context, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C:
            cutoff := time.Now().Unix() - int64(s.ttl.Seconds())
            s.db.ExecContext(ctx, `DELETE FROM agents WHERE last_seen < ?`, cutoff)
        }
    }
}
```

### 5.4 HTTP Handlers

新建 `web/registry/handler.go`：

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/v1/registry/heartbeat` | agent 上报 → 204 |
| `GET` | `/api/v1/registry/agents` | 列表（含 online 状态） |
| `GET` | `/api/v1/registry/agents?online=true` | 仅在线 |

### 5.5 `cmd/monitorweb/main.go` 集成

```go
// 1. 打开统一 DB（替换原有 alerts.NewStore(cfg.Alert.DBPath)）
db, err := openSQLite(cfg.DBPath)

// 2. alert store 改用 *sql.DB
alertStore, err := alerts.NewStore(db)

// 3. registry store 共用同一连接
regStore, err := registry.NewStore(db, cfg.Registry.TTL)
go regStore.CleanLoop(ctx, cfg.Registry.CleanInterval)

// 4. 注入路由
regHandler := registry.NewHandler(regStore)
regHandler.RegisterRoutes(mux)
```

---

## 6. 前端集成

### 6.1 Agent 类型

```typescript
export interface Agent {
  hostname: string
  version: string
  tasks: string[]
  ip: string
  k8s_node: string
  start_time: number
  last_seen: number
  online: boolean
}
```

### 6.2 API 调用

```typescript
export const api = {
  agents: () => get<Agent[]>('/registry/agents'),
}
```

### 6.3 Overview 页

优先用 registry 数据源，降级到 VM hosts：

```tsx
const registry = useAsync(() => api.agents(), [])
const dataSource = registry.data ?? hosts.data
```

### 6.4 HostCard 增强

显示 version + task 标签：

```tsx
{agent.version && <span className="badge">{agent.version}</span>}
{agent.tasks?.slice(0, 4).map(t => <span key={t} className="task-tag">{t}</span>)}
```

---

## 7. API 契约

### `POST /api/v1/registry/heartbeat`

请求体：

```json
{
  "hostname": "web-01",
  "version": "v0.1.0",
  "tasks": ["basereport", "ping", "script", "keyword"],
  "ip": "10.42.0.15",
  "k8s_node": "k3s-node-3",
  "start_time": 1718000000
}
```

响应：`204 No Content`

### `GET /api/v1/registry/agents`

```json
[
  {
    "hostname": "web-01",
    "version": "v0.1.0",
    "tasks": ["basereport", "ping", "script", "keyword"],
    "ip": "10.42.0.15",
    "k8s_node": "k3s-node-3",
    "start_time": 1718000000,
    "last_seen": 1718000123,
    "online": true
  }
]
```

---

## 8. 文件变更清单

| 文件 | 操作 | 行数 |
|------|------|------|
| `configs/config.go` | 新增 `RegistryConfig` | +8 |
| `tasks/registry/models.go` | 新建 — AgentInfo | +25 |
| `tasks/registry/sender.go` | 新建 — Heartbeat sender | +90 |
| `tasks/factory.go` | 新增 `RegisteredTypes()` 辅助函数 | +10 |
| `cmd/monitorbeat/main.go` | 新增 registry 启动 | +10 |
| `web/config/config.go` | `DBPath` 统一 + `RegistryConfig` | +25 |
| `web/configs/web.yaml` | 更新配置示例 | +5 |
| `alerts/store.go` | 改为接收 `*sql.DB` 而非自建连接 | -10 |
| `web/registry/store.go` | 新建 — SQLite store | +100 |
| `web/registry/handler.go` | 新建 — HTTP handlers | +80 |
| `cmd/monitorweb/main.go` | 集成 registry + 统一 DB | +30 |
| `web/ui/src/types.ts` | 新增 `Agent` | +10 |
| `web/ui/src/api/client.ts` | 新增 `agents()` | +5 |
| `web/ui/src/pages/Overview.tsx` | 集成 registry | +15 |
| `web/ui/src/components/HostCard.tsx` | 增加 version/task 显示 | +10 |
| **合计** | | **~410 行** |

---

## 9. 实施顺序

| 步骤 | 内容 | 可验证 |
|------|------|--------|
| 1 | 统一 DB：修改 `alerts/store.go` 接收 `*sql.DB`，`web/config` 加 `DBPath` | `make test` 通过 |
| 2 | 新建 `web/registry/{store,handler}.go` | curl POST/GET 测通 |
| 3 | 集成到 `cmd/monitorweb/main.go` | 部署后心跳可达 |
| 4 | 新建 `tasks/registry/`（models+sender） | agent 配置 registry URL 后日志可见 |
| 5 | 集成到 `cmd/monitorbeat/main.go` | agent 启动后 monitorweb 可见 agent |
| 6 | 前端集成 | 页面显示 version/task 标签 |

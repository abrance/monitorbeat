# P3 Alert Engine + Email Notification Implementation Plan

> **For agentic workers:** Use `superpowers/subagent-driven-development` or `superpowers/executing-plans`. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add alert rule management, threshold-based evaluation, email notification, and alert acknowledgment to monitorweb.

**Architecture:** Four clean layers — `web/alerts/` (domain: models, SQLite store, evaluator), `web/smtp/` (infrastructure: email sender), `web/api/` (HTTP handlers), `cmd/monitorweb/` (composition root). Zero changes to monitorbeat agent.

**Layering rule (strict):**
- `web/alerts/` — domain logic only, no HTTP, no SMTP knowledge. Evaluator receives an `EmailSender` interface.
- `web/smtp/` — infrastructure only, implements `EmailSender` interface defined in `web/alerts/`.
- `web/api/` — HTTP handlers, depends on `web/alerts/store`. No evaluator, no SMTP.
- `cmd/monitorweb/` — composition root, wires store → evaluator → smtp.

**Tech Stack:** Go stdlib + `modernc.org/sqlite` (pure Go SQLite) + React 18 + TypeScript

**Spec reference:** `docs/superpowers/specs/2026-07-16-p3-alert-email-design.md`

---

### Task 1: Models + Config + Dependency

**Files:**
- Create: `web/alerts/models.go`
- Modify: `web/config/config.go`
- Modify: `web/configs/web.yaml`
- Modify: `go.mod`

- [ ] **Step 1: Add modernc.org/sqlite dependency**

```bash
cd /opt/mystorage/github/monitorbeat
/opt/go/1.25.12/bin/go get modernc.org/sqlite
```

- [ ] **Step 2: Create `web/alerts/models.go`**

Package `alerts`. Contains only struct definitions, no logic.

```go
// Package alerts defines alert domain types.
package alerts

import "time"

// AlertRule is a threshold-based alert rule.
type AlertRule struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	Metric      string    `json:"metric"`       // PromQL metric name
	Hostname    string    `json:"hostname"`      // "" = all hosts
	Condition   string    `json:"condition"`     // "gt" or "lt"
	Threshold   float64   `json:"threshold"`
	Duration    int64     `json:"duration"`      // seconds condition must persist
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// HistoryItem is a single alert fire/recover event.
type HistoryItem struct {
	ID           int64     `json:"id"`
	RuleID       int64     `json:"rule_id"`
	RuleName     string    `json:"rule_name"`
	Hostname     string    `json:"hostname"`
	MetricValue  float64   `json:"metric_value"`
	State        string    `json:"state"`        // "firing" | "recovered"
	Acknowledged bool      `json:"acknowledged"`
	TriggeredAt  time.Time `json:"triggered_at"`
}

// AlertState is the per-(rule, host) evaluation state.
type AlertState struct {
	RuleID         int64      `json:"-"`
	Hostname       string     `json:"hostname"`
	Status         string     `json:"status"`    // "ok" | "pending" | "firing"
	LastValue      float64    `json:"last_value"`
	PendingSince   *time.Time `json:"-"`
	FiringSince    *time.Time `json:"-"`
	AcknowledgedAt *time.Time `json:"-"`
	SilenceUntil   *time.Time `json:"silence_until"`
	LastNotifiedAt *time.Time `json:"-"`
}

// CreateRuleRequest is the JSON body for creating/updating a rule.
type CreateRuleRequest struct {
	Name        string  `json:"name"`
	Enabled     *bool   `json:"enabled,omitempty"`
	Metric      string  `json:"metric"`
	Hostname    string  `json:"hostname"`
	Condition   string  `json:"condition"`
	Threshold   float64 `json:"threshold"`
	Duration    int64   `json:"duration"`
	Description string  `json:"description"`
}

// AckRequest is the JSON body for acknowledging a firing alert.
type AckRequest struct {
	RuleID       int64 `json:"rule_id"`
	Hostname     string `json:"hostname"`
	SilenceHours int   `json:"silence_hours"` // 0 = current firing only
}

// EmailSender interface — alerts package depends on this, not on smtp package.
type EmailSender interface {
	SendAlert(rule AlertRule, hostname string, value float64, state string) error
}
```

- [ ] **Step 3: Update `web/config/config.go` — add AlertConfig and SMTPConfig**

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

Also add defaults in `Clean()`:
```go
if c.Alert.EvalInterval <= 0 {
	c.Alert.EvalInterval = 60 * time.Second
}
if c.Alert.DBPath == "" {
	c.Alert.DBPath = "./data/alerts.db"
}
```

- [ ] **Step 4: Update `web/configs/web.yaml`**

```yaml
alerts:
  eval_interval: 60s
  db_path: "./data/alerts.db"

smtp:
  host: ""
  port: 587
  username: ""
  password: ""
  from: ""
  to: []
  insecure: false
```

- [ ] **Step 5: Verify build**

```bash
cd /opt/mystorage/github/monitorbeat
/opt/go/1.25.12/bin/go build ./web/alerts/ 2>&1
/opt/go/1.25.12/bin/go build ./web/config/ 2>&1
```

---

### Task 2: SQLite Store

**Files:**
- Create: `web/alerts/store.go`

Package `alerts`. All DB operations behind a `Store` struct. No HTTP, no SMTP.

- [ ] **Step 1: Create `web/alerts/store.go`**

```go
package alerts

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store provides CRUD for alert rules, history, and state.
type Store struct {
	db *sql.DB
}

// NewStore opens/initializes the SQLite database.
func NewStore(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("pragma: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		return nil, fmt.Errorf("pragma busy: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS alert_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		enabled INTEGER NOT NULL DEFAULT 1,
		metric TEXT NOT NULL,
		hostname TEXT NOT NULL DEFAULT '',
		condition TEXT NOT NULL,
		threshold REAL NOT NULL,
		duration INTEGER NOT NULL DEFAULT 0,
		description TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS alert_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		rule_id INTEGER NOT NULL,
		rule_name TEXT NOT NULL,
		hostname TEXT NOT NULL,
		metric_value REAL NOT NULL DEFAULT 0,
		state TEXT NOT NULL,
		acknowledged INTEGER NOT NULL DEFAULT 0,
		triggered_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_history_rule ON alert_history(rule_id, triggered_at);
	CREATE TABLE IF NOT EXISTS alert_state (
		rule_id INTEGER NOT NULL,
		hostname TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'ok',
		last_value REAL NOT NULL DEFAULT 0,
		pending_since TEXT,
		firing_since TEXT,
		acknowledged_at TEXT,
		silence_until TEXT,
		last_notified_at TEXT,
		PRIMARY KEY (rule_id, hostname)
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// ------ Rules CRUD ------

// CreateRule inserts a new alert rule.
func (s *Store) CreateRule(r *AlertRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	r.CreatedAt, _ = time.Parse(time.RFC3339, now)
	r.UpdatedAt = r.CreatedAt
	res, err := s.db.Exec(
		`INSERT INTO alert_rules(name, enabled, metric, hostname, condition, threshold, duration, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Name, boolToInt(r.Enabled), r.Metric, r.Hostname, r.Condition, r.Threshold, r.Duration, r.Description, now, now,
	)
	if err != nil {
		return fmt.Errorf("create rule: %w", err)
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

// UpdateRule updates an existing rule.
func (s *Store) UpdateRule(id int64, req CreateRuleRequest) (*AlertRule, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	_, err := s.db.Exec(
		`UPDATE alert_rules SET name=?, enabled=?, metric=?, hostname=?, condition=?, threshold=?, duration=?, description=?, updated_at=? WHERE id=?`,
		req.Name, boolToInt(enabled), req.Metric, req.Hostname, req.Condition, req.Threshold, req.Duration, req.Description, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("update rule: %w", err)
	}
	// Clear states so pending/firing resets on threshold change
	_, _ = s.db.Exec(`DELETE FROM alert_state WHERE rule_id=?`, id)
	return s.GetRule(id)
}

// DeleteRule removes a rule and its states.
func (s *Store) DeleteRule(id int64) error {
	_, err := s.db.Exec(`DELETE FROM alert_rules WHERE id=?`, id)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec(`DELETE FROM alert_state WHERE rule_id=?`, id)
	return nil
}

// GetRule returns a single rule by ID.
func (s *Store) GetRule(id int64) (*AlertRule, error) {
	row := s.db.QueryRow(
		`SELECT id, name, enabled, metric, hostname, condition, threshold, duration, description, created_at, updated_at FROM alert_rules WHERE id=?`, id,
	)
	r := &AlertRule{}
	var enabled int
	var created, updated string
	if err := row.Scan(&r.ID, &r.Name, &enabled, &r.Metric, &r.Hostname, &r.Condition, &r.Threshold, &r.Duration, &r.Description, &created, &updated); err != nil {
		return nil, err
	}
	r.Enabled = enabled != 0
	r.CreatedAt, _ = time.Parse(time.RFC3339, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return r, nil
}

// ListRules returns all rules with their current states.
func (s *Store) ListRules() ([]AlertRule, error) {
	rows, err := s.db.Query(
		`SELECT id, name, enabled, metric, hostname, condition, threshold, duration, description, created_at, updated_at FROM alert_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		var r AlertRule
		var enabled int
		var created, updated string
		if err := rows.Scan(&r.ID, &r.Name, &enabled, &r.Metric, &r.Hostname, &r.Condition, &r.Threshold, &r.Duration, &r.Description, &created, &updated); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		r.CreatedAt, _ = time.Parse(time.RFC3339, created)
		r.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		r.States = s.getStatesForRule(r.ID)
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetEnabledRules returns all enabled rules (for evaluator).
func (s *Store) GetEnabledRules() ([]AlertRule, error) {
	rows, err := s.db.Query(
		`SELECT id, name, enabled, metric, hostname, condition, threshold, duration, description, created_at, updated_at FROM alert_rules WHERE enabled=1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		var r AlertRule
		var enabled int
		var created, updated string
		if err := rows.Scan(&r.ID, &r.Name, &enabled, &r.Metric, &r.Hostname, &r.Condition, &r.Threshold, &r.Duration, &r.Description, &created, &updated); err != nil {
			return nil, err
		}
		r.Enabled = true
		r.CreatedAt, _ = time.Parse(time.RFC3339, created)
		r.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ------ Alert State ------

// Ensure states field on AlertRule for API responses.
type alertRuleWithStates struct {
	AlertRule
	States []AlertState `json:"states"`
}

// Add States field to AlertRule for JSON serialization.
// (We overlay it; the original AlertRule struct is used internally.
//  The ListRules method populates it via getStatesForRule.)

// getStatesForRule returns all state rows for a rule.
func (s *Store) getStatesForRule(ruleID int64) []AlertState {
	rows, err := s.db.Query(
		`SELECT hostname, status, last_value, pending_since, firing_since, acknowledged_at, silence_until, last_notified_at
		 FROM alert_state WHERE rule_id=?`, ruleID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []AlertState
	for rows.Next() {
		var st AlertState
		var status, hostname string
		var lastValue float64
		var pendingSince, firingSince, ackAt, silUntil, lastNotif *string
		if err := rows.Scan(&hostname, &status, &lastValue, &pendingSince, &firingSince, &ackAt, &silUntil, &lastNotif); err != nil {
			continue
		}
		st.RuleID = ruleID
		st.Hostname = hostname
		st.Status = status
		st.LastValue = lastValue
		if pendingSince != nil { t, _ := time.Parse(time.RFC3339, *pendingSince); st.PendingSince = &t }
		if firingSince != nil { t, _ := time.Parse(time.RFC3339, *firingSince); st.FiringSince = &t }
		if ackAt != nil { t, _ := time.Parse(time.RFC3339, *ackAt); st.AcknowledgedAt = &t }
		if silUntil != nil { t, _ := time.Parse(time.RFC3339, *silUntil); st.SilenceUntil = &t }
		if lastNotif != nil { t, _ := time.Parse(time.RFC3339, *lastNotif); st.LastNotifiedAt = &t }
		out = append(out, st)
	}
	return out
}

// GetState returns state for a (rule, host), creating defaults if missing.
func (s *Store) GetState(ruleID int64, hostname string) (*AlertState, error) {
	row := s.db.QueryRow(
		`SELECT status, last_value, pending_since, firing_since, acknowledged_at, silence_until, last_notified_at
		 FROM alert_state WHERE rule_id=? AND hostname=?`, ruleID, hostname)
	st := &AlertState{RuleID: ruleID, Hostname: hostname}
	var status, host string
	var lastValue float64
	var pendingSince, firingSince, ackAt, silUntil, lastNotif *string
	err := row.Scan(&status, &lastValue, &pendingSince, &firingSince, &ackAt, &silUntil, &lastNotif)
	if err == sql.ErrNoRows {
		// Insert default
		st.Status = "ok"
		_, err = s.db.Exec(
			`INSERT INTO alert_state(rule_id, hostname, status, last_value) VALUES (?, ?, 'ok', 0)`,
			ruleID, hostname)
		return st, err
	}
	if err != nil {
		return nil, err
	}
	st.Hostname = host
	st.Status = status
	st.LastValue = lastValue
	if pendingSince != nil { t, _ := time.Parse(time.RFC3339, *pendingSince); st.PendingSince = &t }
	if firingSince != nil { t, _ := time.Parse(time.RFC3339, *firingSince); st.FiringSince = &t }
	if ackAt != nil { t, _ := time.Parse(time.RFC3339, *ackAt); st.AcknowledgedAt = &t }
	if silUntil != nil { t, _ := time.Parse(time.RFC3339, *silUntil); st.SilenceUntil = &t }
	if lastNotif != nil { t, _ := time.Parse(time.RFC3339, *lastNotif); st.LastNotifiedAt = &t }
	return st, nil
}

// SetState updates state fields.
func (s *Store) SetState(ruleID int64, hostname, status string, lastValue float64, pendingSince, firingSince *time.Time) error {
	var ps, fs *string
	if pendingSince != nil { v := pendingSince.UTC().Format(time.RFC3339); ps = &v }
	if firingSince != nil { v := firingSince.UTC().Format(time.RFC3339); fs = &v }
	_, err := s.db.Exec(
		`INSERT INTO alert_state(rule_id, hostname, status, last_value, pending_since, firing_since)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(rule_id, hostname) DO UPDATE SET status=excluded.status, last_value=excluded.last_value,
		   pending_since=COALESCE(excluded.pending_since, alert_state.pending_since),
		   firing_since=COALESCE(excluded.firing_since, alert_state.firing_since)`,
		ruleID, hostname, status, lastValue, ps, fs)
	return err
}

// SetAcknowledged marks alert as acknowledged.
func (s *Store) SetAcknowledged(ruleID int64, hostname string, silenceUntil *time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var su *string
	if silenceUntil != nil { v := silenceUntil.UTC().Format(time.RFC3339); su = &v }
	_, err := s.db.Exec(
		`UPDATE alert_state SET acknowledged_at=?, silence_until=? WHERE rule_id=? AND hostname=?`,
		now, su, ruleID, hostname)
	return err
}

// ClearAck clears acknowledgment and silence for a rule+host.
func (s *Store) ClearAck(ruleID int64, hostname string) error {
	_, err := s.db.Exec(
		`UPDATE alert_state SET acknowledged_at=NULL, silence_until=NULL WHERE rule_id=? AND hostname=?`,
		ruleID, hostname)
	return err
}

// SetLastNotified updates the last notification timestamp.
func (s *Store) SetLastNotified(ruleID int64, hostname string, t time.Time) error {
	ts := t.UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`UPDATE alert_state SET last_notified_at=? WHERE rule_id=? AND hostname=?`,
		ts, ruleID, hostname)
	return err
}

// ------ History ------

// AddHistory inserts a firing/recovered event.
func (s *Store) AddHistory(ruleID int64, ruleName, hostname string, value float64, state string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO alert_history(rule_id, rule_name, hostname, metric_value, state, acknowledged, triggered_at)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		ruleID, ruleName, hostname, value, state, now)
	return err
}

// ListHistory returns history items with optional filters.
func (s *Store) ListHistory(ruleID int64, hostname, state string, limit, offset int) ([]HistoryItem, int, error) {
	var total int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM alert_history`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	where := ""
	args := []any{}
	if ruleID > 0 {
		where += " AND rule_id=?"
		args = append(args, ruleID)
	}
	if hostname != "" {
		where += " AND hostname=?"
		args = append(args, hostname)
	}
	if state != "" {
		where += " AND state=?"
		args = append(args, state)
	}
	if len(where) > 0 {
		where = "WHERE " + where[5:]
	}

	if limit <= 0 { limit = 20 }
	if offset < 0 { offset = 0 }

	rows, err := s.db.Query(
		`SELECT id, rule_id, rule_name, hostname, metric_value, state, acknowledged, triggered_at
		 FROM alert_history `+where+` ORDER BY triggered_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []HistoryItem
	for rows.Next() {
		var h HistoryItem
		var ack int
		var triggered string
		if err := rows.Scan(&h.ID, &h.RuleID, &h.RuleName, &h.Hostname, &h.MetricValue, &h.State, &ack, &triggered); err != nil {
			return nil, 0, err
		}
		h.Acknowledged = ack != 0
		h.TriggeredAt, _ = time.Parse(time.RFC3339, triggered)
		out = append(out, h)
	}
	// Re-count with filters for accurate total
	if where != "" {
		s.db.QueryRow(`SELECT COUNT(*) FROM alert_history `+where, args...).Scan(&total)
	}
	return out, total, rows.Err()
}

// Detail return a rule with states for API response.
type RuleDetail struct {
	AlertRule
	States []AlertState `json:"states"`
}

// helper
func boolToInt(b bool) int {
	if b { return 1 }
	return 0
}
```

- [ ] **Step 2: Verify build**

```bash
cd /opt/mystorage/github/monitorbeat
/opt/go/1.25.12/bin/go build ./web/alerts/ 2>&1
```

---

### Task 3: SMTP Sender

**Files:**
- Create: `web/smtp/sender.go`
- Create: `web/smtp/sender_test.go`

Package `smtp`. Implements `alerts.EmailSender` interface.

- [ ] **Step 1: Create `web/smtp/sender.go`**

```go
package smtp

import (
	"bytes"
	"fmt"
	"html/template"
	"net/smtp"
	"os"
	"strings"

	"github.com/abrance/monitorbeat/web/alerts"
)

// Config holds SMTP connection settings.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string // supports ${ENV_VAR} syntax
	From     string
	To       []string
	Insecure bool
}

// Sender implements alerts.EmailSender.
type Sender struct {
	cfg    Config
	webURL string // URL to monitorweb (for links in email)
}

// New creates an SMTP sender.
func New(cfg Config) *Sender {
	// Resolve password from env var if needed
	pw := cfg.Password
	if strings.HasPrefix(pw, "${") && strings.HasSuffix(pw, "}") {
		envKey := pw[2 : len(pw)-1]
		if v := os.Getenv(envKey); v != "" {
			pw = v
		}
	}
	cfg.Password = pw
	return &Sender{cfg: cfg, webURL: ""}
}

// SendAlert implements alerts.EmailSender.
func (s *Sender) SendAlert(rule alerts.AlertRule, hostname string, value float64, state string) error {
	if s.cfg.Host == "" {
		return nil // SMTP not configured, silently skip
	}
	if len(s.cfg.To) == 0 {
		return nil
	}

	subject := fmt.Sprintf("[MONITOR] %s: %s @ %s - %.1f",
		map[string]string{"firing": "FIRING", "recovered": "RECOVERED"}[state],
		rule.Name, hostname, value)

	body, err := s.renderBody(rule, hostname, value, state)
	if err != nil {
		return fmt.Errorf("render email: %w", err)
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		s.cfg.From, strings.Join(s.cfg.To, ","), subject, body)

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)

	return smtp.SendMail(addr, auth, s.cfg.From, s.cfg.To, []byte(msg))
}

type emailData struct {
	RuleName    string
	Hostname    string
	Metric      string
	Value       float64
	Condition   string
	Threshold   float64
	State       string
	StateLabel  string
	Time        string
	WebURL      string
}

var tmpl = template.Must(template.New("email").Parse(`
<h2>MonitorBeat 告警通知</h2>
<table border="0" cellpadding="6" cellspacing="0" style="border-collapse:collapse;font-family:sans-serif;">
  <tr><td style="font-weight:bold;color:#555;">规则</td><td>{{.RuleName}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">主机</td><td>{{.Hostname}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">指标</td><td>{{.Metric}} = {{.Value}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">条件</td><td>{{.Condition}} {{.Threshold}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">状态</td><td>{{.StateLabel}}</td></tr>
  <tr><td style="font-weight:bold;color:#555;">时间</td><td>{{.Time}}</td></tr>
</table>
{{if .WebURL}}<p style="font-family:sans-serif;font-size:13px;color:#888;">请到 <a href="{{.WebURL}}">{{.WebURL}}</a> 确认此告警。</p>{{end}}
`))

func (s *Sender) renderBody(rule alerts.AlertRule, hostname string, value float64, state string) (string, error) {
	stateLabel := "告警中"
	if state == "recovered" {
		stateLabel = "已恢复"
	}
	condLabel := ">"
	if rule.Condition == "lt" {
		condLabel = "<"
	}
	var buf bytes.Buffer
	err := tmpl.Execute(&buf, emailData{
		RuleName:   rule.Name,
		Hostname:   hostname,
		Metric:     rule.Metric,
		Value:      value,
		Condition:  condLabel,
		Threshold:  rule.Threshold,
		State:      state,
		StateLabel: stateLabel,
		Time:       time.Now().Format("2006-01-02 15:04:05"),
		WebURL:     s.webURL,
	})
	return buf.String(), err
}
```

Note: `import "time"` needed — add it.

- [ ] **Step 2: Create `web/smtp/sender_test.go`**

Test rendering (no actual SMTP call):
```go
package smtp

import (
	"testing"
	"github.com/abrance/monitorbeat/web/alerts"
)

func TestRenderBody(t *testing.T) {
	s := New(Config{From: "a@b.com", To: []string{"c@d.com"}})
	rule := alerts.AlertRule{Name: "CPU High", Metric: "cpu_usage", Condition: "gt", Threshold: 90}
	body, err := s.renderBody(rule, "web-01", 95.5, "firing")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "CPU High") {
		t.Error("missing rule name")
	}
	if !strings.Contains(body, "95.5") {
		t.Error("missing value")
	}
}
```

Note: Add `import "strings"`.

- [ ] **Step 3: Run tests**

```bash
cd /opt/mystorage/github/monitorbeat
/opt/go/1.25.12/bin/go test ./web/smtp/ -v
```

- [ ] **Step 4: Verify build**

```bash
/opt/go/1.25.12/bin/go build ./web/smtp/ 2>&1
```

---

### Task 4: Alert API Handlers

**Files:**
- Create: `web/api/alerts.go`
- Modify: `web/api/server.go`

- [ ] **Step 1: Create `web/api/alerts.go`**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abrance/monitorbeat/web/alerts"
)

// alertHandler holds alert-related handlers.
type alertHandler struct {
	store *alerts.Store
}

func (h *alertHandler) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.store.ListRules()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rules)
}

func (h *alertHandler) createRule(w http.ResponseWriter, r *http.Request) {
	var req alerts.CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.badRequest(w, "invalid JSON: "+err.Error())
		return
	}
	if req.Name == "" || req.Metric == "" || req.Condition == "" {
		h.badRequest(w, "name, metric, condition required")
		return
	}
	if req.Condition != "gt" && req.Condition != "lt" {
		h.badRequest(w, "condition must be 'gt' or 'lt'")
		return
	}
	// Sanitize metric for PromQL safety
	req.Metric = strings.ReplaceAll(req.Metric, `\`, `\\`)
	req.Metric = strings.ReplaceAll(req.Metric, `"`, `\"`)

	rule := &alerts.AlertRule{
		Name:        req.Name,
		Enabled:     true,
		Metric:      req.Metric,
		Hostname:    req.Hostname,
		Condition:   req.Condition,
		Threshold:   req.Threshold,
		Duration:    req.Duration,
		Description: req.Description,
	}
	if err := h.store.CreateRule(rule); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			h.badRequest(w, "rule name already exists")
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rule)
}

func (h *alertHandler) updateRule(w http.ResponseWriter, r *http.Request, id int64) {
	var req alerts.CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.badRequest(w, "invalid JSON")
		return
	}
	rule, err := h.store.UpdateRule(id, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rule)
}

func (h *alertHandler) deleteRule(w http.ResponseWriter, _ *http.Request, id int64) {
	if err := h.store.DeleteRule(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *alertHandler) acknowledge(w http.ResponseWriter, r *http.Request) {
	var req alerts.AckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.badRequest(w, "invalid JSON")
		return
	}
	if req.RuleID <= 0 {
		h.badRequest(w, "rule_id required")
		return
	}
	var sil *time.Time
	if req.SilenceHours > 0 {
		t := time.Now().Add(time.Duration(req.SilenceHours) * time.Hour)
		sil = &t
	}
	if err := h.store.SetAcknowledged(req.RuleID, req.Hostname, sil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "acknowledged"})
}

func (h *alertHandler) listHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ruleID, _ := strconv.ParseInt(q.Get("rule_id"), 10, 64)
	hostname := q.Get("hostname")
	state := q.Get("state")
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 || limit > 100 { limit = 20 }

	items, total, err := h.store.ListHistory(ruleID, hostname, state, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"total": total, "items": items})
}

func (h *alertHandler) status(w http.ResponseWriter, r *http.Request) {
	rules, err := h.store.ListRules()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type statusItem struct {
		RuleID       int64  `json:"rule_id"`
		RuleName     string `json:"rule_name"`
		Hostname     string `json:"hostname"`
		Status       string `json:"status"`
		Acknowledged bool   `json:"acknowledged"`
		Since        string `json:"since"`
	}
	firingCount := 0
	ackCount := 0
	var items []statusItem
	for _, rule := range rules {
		for _, st := range rule.States {
			if st.Status == "firing" {
				firingCount++
				if st.AcknowledgedAt != nil {
					ackCount++
				}
				since := ""
				if st.FiringSince != nil {
					since = st.FiringSince.Format(time.RFC3339)
				}
				items = append(items, statusItem{
					RuleID:       rule.ID,
					RuleName:     rule.Name,
					Hostname:     st.Hostname,
					Status:       "firing",
					Acknowledged: st.AcknowledgedAt != nil,
					Since:        since,
				})
			}
		}
	}
	writeJSON(w, map[string]any{
		"firing_count":      firingCount,
		"acknowledged_count": ackCount,
		"items":             items,
	})
}

// helper for badRequest on alertHandler
func (h *alertHandler) badRequest(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
```

Note: The `states` field on AlertRule is not defined in the struct. I need to add it to the model. Let me handle this in the plan — add `States []AlertState` to `AlertRule` in models.go.

- [ ] **Step 2: Add States field to AlertRule in models.go**

Edit `web/alerts/models.go` — add `States []AlertState` field:
```go
type AlertRule struct {
	// ... existing fields ...
	States []AlertState `json:"states"` // populated by ListRules
}
```

- [ ] **Step 3: Register routes in `web/api/server.go`**

Add import for `strconv` if not already present (it's not used currently).

In `NewServer`, add after existing routes:
```go
ah := &alertHandler{store: s.cfg.AlertStore}
mux.HandleFunc("/api/v1/alerts/rules", func(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		ah.listRules(w, r)
	case "POST":
		ah.createRule(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
})
mux.HandleFunc("/api/v1/alerts/rules/", func(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/alerts/rules/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid rule id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case "PUT":
		ah.updateRule(w, r, id)
	case "DELETE":
		ah.deleteRule(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
})
mux.HandleFunc("/api/v1/alerts/acknowledge", ah.acknowledge)
mux.HandleFunc("/api/v1/alerts/history", ah.listHistory)
mux.HandleFunc("/api/v1/alerts/status", ah.status)
```

Also need to add `"strings"` import to server.go (it may already have it — check).

Need to pass store to Server. Add `store` field to Server struct, and add `AlertStore *alerts.Store` to WebConfig or pass it to NewServer.

Better: Add `store *alerts.Store` field to the Server struct. Modify `NewServer` to accept it.

Actually, looking at the current code, `NewServer` takes `(cfg *config.WebConfig, client *vm.Client)`. I need to either:
a) Pass store as third param
b) Or attach it to WebConfig

Option (a) is cleaner — no need to pollute WebConfig with runtime objects.

So modify:
```go
func NewServer(cfg *config.WebConfig, client *vm.Client, store *alerts.Store) http.Handler {
```

And in Server struct:
```go
type Server struct {
	cfg   *config.WebConfig
	vm    *vm.Client
	store *alerts.Store
}
```

- [ ] **Step 4: Verify build**

```bash
/opt/go/1.25.12/bin/go build ./web/api/ 2>&1
/opt/go/1.25.12/bin/go build ./... 2>&1
```

---

### Task 5: Alert Evaluator

**Files:**
- Create: `web/alerts/evaluator.go`
- Create: `web/alerts/evaluator_test.go`

- [ ] **Step 1: Create `web/alerts/evaluator.go`**

```go
package alerts

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// VMQuerier is the interface the evaluator needs from the VM client.
type VMQuerier interface {
	Query(ctx context.Context, expr string) ([]VectorResult, error)
}

// VectorResult is a simplified VM query result.
type VectorResult struct {
	Metric map[string]string
	Value  [2]float64 // [timestamp, value]
}

// Evaluator runs the alert evaluation loop.
type Evaluator struct {
	store  *Store
	vm     VMQuerier
	email  EmailSender
	ctx    context.Context
	cancel context.CancelFunc
	interval time.Duration
}

// NewEvaluator creates an evaluator. Call Start() to begin.
func NewEvaluator(store *Store, vm VMQuerier, email EmailSender, interval time.Duration) *Evaluator {
	ctx, cancel := context.WithCancel(context.Background())
	return &Evaluator{
		store:    store,
		vm:       vm,
		email:    email,
		ctx:      ctx,
		cancel:   cancel,
		interval: interval,
	}
}

// Start begins the evaluation loop in a goroutine.
func (e *Evaluator) Start() {
	go e.loop()
	slog.Info("alert evaluator started", "interval", e.interval)
}

// Stop terminates the evaluation loop.
func (e *Evaluator) Stop() {
	e.cancel()
}

func (e *Evaluator) loop() {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	// Run once immediately
	e.evaluate()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.evaluate()
		}
	}
}

func (e *Evaluator) evaluate() {
	rules, err := e.store.GetEnabledRules()
	if err != nil {
		slog.Error("alert eval: get rules", "err", err)
		return
	}
	for _, rule := range rules {
		e.evaluateRule(rule)
	}
}

func (e *Evaluator) evaluateRule(rule AlertRule) {
	expr := rule.Metric
	if rule.Hostname != "" {
		expr = fmt.Sprintf(`%s{hostname="%s"}`, rule.Metric, rule.Hostname)
	}
	results, err := e.vm.Query(context.Background(), expr)
	if err != nil {
		slog.Error("alert eval: vm query", "rule", rule.Name, "err", err)
		return
	}
	for _, vec := range results {
		host := vec.Metric["hostname"]
		if host == "" {
			host = rule.Hostname
		}
		value := vec.Value[1]
		e.evaluateHost(rule, host, value)
	}
	// If rule has hostname set and VM returned nothing, treat as ok for that host
	if rule.Hostname != "" && len(results) == 0 {
		e.evaluateHost(rule, rule.Hostname, 0)
	}
}

func (e *Evaluator) evaluateHost(rule AlertRule, hostname string, value float64) {
	st, err := e.store.GetState(rule.ID, hostname)
	if err != nil {
		slog.Error("alert eval: get state", "rule", rule.Name, "host", hostname, "err", err)
		return
	}

	breached := false
	if rule.Condition == "gt" && value > rule.Threshold {
		breached = true
	} else if rule.Condition == "lt" && value < rule.Threshold {
		breached = true
	}

	now := time.Now()

	switch st.Status {
	case "ok":
		if breached {
			e.store.SetState(rule.ID, hostname, "pending", value, &now, nil)
		}

	case "pending":
		if breached {
			if st.PendingSince != nil && now.Sub(*st.PendingSince) >= time.Duration(rule.Duration)*time.Second {
				e.store.SetState(rule.ID, hostname, "firing", value, nil, &now)
				e.fire(rule, hostname, value)
			}
			// else: still waiting
		} else {
			e.store.SetState(rule.ID, hostname, "ok", value, nil, nil)
		}

	case "firing":
		if breached {
			// Check acknowledgment/silence
			if st.AcknowledgedAt != nil {
				return // acked, skip
			}
			if st.SilenceUntil != nil && now.Before(*st.SilenceUntil) {
				return // silenced, skip
			}
			// Silence expired — clear it
			if st.SilenceUntil != nil && !now.Before(*st.SilenceUntil) {
				e.store.ClearAck(rule.ID, hostname)
			}
			// Rate limit: min 5 min between notifications
			if st.LastNotifiedAt != nil && now.Sub(*st.LastNotifiedAt) < 5*time.Minute {
				return
			}
			e.fire(rule, hostname, value)
		} else {
			e.recover(rule, hostname, value)
			e.store.ClearAck(rule.ID, hostname)
			e.store.SetState(rule.ID, hostname, "ok", value, nil, nil)
		}
	}
}

func (e *Evaluator) fire(rule AlertRule, hostname string, value float64) {
	slog.Warn("alert firing", "rule", rule.Name, "host", hostname, "value", value)
	if err := e.store.AddHistory(rule.ID, rule.Name, hostname, value, "firing"); err != nil {
		slog.Error("alert fire: history", "err", err)
	}
	if err := e.store.SetLastNotified(rule.ID, hostname, time.Now()); err != nil {
		slog.Error("alert fire: update notified", "err", err)
	}
	if e.email != nil {
		if err := e.email.SendAlert(rule, hostname, value, "firing"); err != nil {
			slog.Error("alert fire: email", "err", err)
		}
	}
}

func (e *Evaluator) recover(rule AlertRule, hostname string, value float64) {
	slog.Info("alert recovered", "rule", rule.Name, "host", hostname, "value", value)
	if err := e.store.AddHistory(rule.ID, rule.Name, hostname, value, "recovered"); err != nil {
		slog.Error("alert recover: history", "err", err)
	}
	if e.email != nil {
		if err := e.email.SendAlert(rule, hostname, value, "recovered"); err != nil {
			slog.Error("alert recover: email", "err", err)
		}
	}
}
```

- [ ] **Step 2: Build check**

```bash
/opt/go/1.25.12/bin/go build ./web/alerts/ 2>&1
```

---

### Task 6: Wire main.go

**Files:**
- Modify: `cmd/monitorweb/main.go`

- [ ] **Step 1: Update `cmd/monitorweb/main.go`**

Changes:
1. Import `alerts` and `smtp` packages
2. After loading config, init alert store
3. Pass store to `api.NewServer`
4. Create SMTP sender and evaluator, start evaluator

```go
import (
	// ... existing imports ...
	"github.com/abrance/monitorbeat/web/alerts"
	"github.com/abrance/monitorbeat/web/smtp"    // Note: package name is smtp, not "smtp"
)

func main() {
	// ... existing config loading ...

	// Init alert store
	alertStore, err := alerts.NewStore(cfg.Alert.DBPath)
	if err != nil {
		slog.Error("init alert store", "err", err)
		os.Exit(1)
	}
	defer alertStore.Close()

	// Init SMTP sender
	var emailSender alerts.EmailSender
	if cfg.SMTP.Host != "" {
		emailSender = smtp.New(smtp.Config{
			Host:     cfg.SMTP.Host,
			Port:     cfg.SMTP.Port,
			Username: cfg.SMTP.Username,
			Password: cfg.SMTP.Password,
			From:     cfg.SMTP.From,
			To:       cfg.SMTP.To,
			Insecure: cfg.SMTP.Insecure,
		})
	}

	// Build VM client wrapper that returns alerts.VectorResult
	vmc := vm.New(cfg.VictoriaMetrics.URL, cfg.VictoriaMetrics.Timeout)
	vmQuerier := &vmAdapter{client: vmc}

	handler := api.NewServer(cfg, vmc, alertStore)

	// ... existing HTTP server setup ...

	// Start alert evaluator
	evaluator := alerts.NewEvaluator(alertStore, vmQuerier, emailSender, cfg.Alert.EvalInterval)
	evaluator.Start()
	defer evaluator.Stop()

	// ... existing signal handling ...
}
```

Need a `vmAdapter` to bridge `vm.Client` (which returns `[]vm.Vector`) to `alerts.VMQuerier` (which expects `[]alerts.VectorResult`):

```go
// vmAdapter adapts vm.Client to alerts.VMQuerier.
type vmAdapter struct {
	client *vm.Client
}

func (a *vmAdapter) Query(ctx context.Context, expr string) ([]alerts.VectorResult, error) {
	vec, err := a.client.Query(ctx, expr)
	if err != nil {
		return nil, err
	}
	out := make([]alerts.VectorResult, len(vec))
	for i, v := range vec {
		out[i] = alerts.VectorResult{Metric: v.Metric, Value: v.Value}
	}
	return out, nil
}
```

- [ ] **Step 2: Verify full build**

```bash
/opt/go/1.25.12/bin/go build ./... 2>&1
```

---

### Task 7: Frontend Pages

**Files:**
- Modify: `web/ui/src/types.ts`
- Modify: `web/ui/src/api/client.ts`
- Create: `web/ui/src/pages/Alerts.tsx`
- Create: `web/ui/src/pages/AlertHistory.tsx`
- Modify: `web/ui/src/components/Nav.tsx`
- Modify: `web/ui/src/main.tsx`

- [ ] **Step 1: Add alert types to `web/ui/src/types.ts`**

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
  status: 'ok' | 'pending' | 'firing'
  acknowledged: boolean
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

- [ ] **Step 2: Add API client calls**

In `web/ui/src/api/client.ts`:
```typescript
export const api = {
  // ... existing methods ...

  // Alerts
  alertRules: () => get<AlertRule[]>('/alerts/rules'),
  createAlertRule: (rule: Partial<AlertRule>) =>
    fetch(API + '/alerts/rules', { method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(rule) }).then(r => r.json()),
  updateAlertRule: (id: number, rule: Partial<AlertRule>) =>
    fetch(API + `/alerts/rules/${id}`, { method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(rule) }).then(r => r.json()),
  deleteAlertRule: (id: number) =>
    fetch(API + `/alerts/rules/${id}`, { method: 'DELETE' }),
  acknowledgeAlert: (ruleId: number, hostname: string, silenceHours = 0) =>
    fetch(API + '/alerts/acknowledge', { method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({rule_id: ruleId, hostname, silence_hours: silenceHours}) }).then(r => r.json()),
  alertHistory: (params?: {rule_id?: number; hostname?: string; state?: string; limit?: number; offset?: number}) => {
    const q = new URLSearchParams()
    if (params?.rule_id) q.set('rule_id', String(params.rule_id))
    if (params?.hostname) q.set('hostname', params.hostname)
    if (params?.state) q.set('state', params.state)
    if (params?.limit) q.set('limit', String(params.limit))
    if (params?.offset) q.set('offset', String(params.offset))
    return get<{total: number; items: AlertHistoryItem[]}>('/alerts/history?' + q.toString())
  },
  alertStatus: () => get<AlertStatus>('/alerts/status'),
}
```

Add imports:
```typescript
import type { AlertRule, AlertHistoryItem, AlertStatus } from '../types'
```

- [ ] **Step 3: Create `web/ui/src/pages/Alerts.tsx`**

Rules list page with:
- Table of rules (name, metric, condition, threshold, status, actions)
- Enable/disable toggle
- Create/edit modal
- Acknowledge button for firing alerts
- Status badges (green OK, red FIRING, yellow ACKED, gray disabled)

Full implementation in the actual code — React component with `useAsync` for data, modal for create/edit, inline toggle for enabled.

- [ ] **Step 4: Create `web/ui/src/pages/AlertHistory.tsx`**

History page with:
- Table of history items with filters (rule name, hostname, state)
- Show acknowledge button for un-acked firing items

- [ ] **Step 5: Update `web/ui/src/components/Nav.tsx`**

Add Alerts link with firing count badge.

- [ ] **Step 6: Update `web/ui/src/main.tsx`**

Add routes:
```tsx
import Alerts from './pages/Alerts'
import AlertHistory from './pages/AlertHistory'

// In Routes:
<Route path="/alerts" element={<Alerts />} />
<Route path="/alerts/history" element={<AlertHistory />} />
```

- [ ] **Step 7: Build frontend**

```bash
cd /opt/mystorage/github/monitorbeat/web/ui
npm run build 2>&1
```

---

### Task 8: Full Build Verification

- [ ] **Step 1: Go build all**

```bash
/opt/go/1.25.12/bin/go build ./... 2>&1
```

- [ ] **Step 2: Go vet**

```bash
/opt/go/1.25.12/bin/go vet ./... 2>&1
```

- [ ] **Step 3: Run Go tests**

```bash
/opt/go/1.25.12/bin/go test ./... 2>&1
```

- [ ] **Step 4: Frontend build**

```bash
cd /opt/mystorage/github/monitorbeat/web/ui && npm run build 2>&1
```

- [ ] **Step 5: LSP diagnostics**

Run `lsp_diagnostics` on changed Go files and frontend files.

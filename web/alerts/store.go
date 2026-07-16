package alerts

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store provides CRUD operations for alert rules, history, and state.
// All methods are safe for single-goroutine access from the evaluator.
type Store struct {
	db *sql.DB
}

// NewStore opens or initializes the SQLite database at dbPath.
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
		return nil, fmt.Errorf("pragma wal: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		return nil, fmt.Errorf("pragma busy_timeout: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error { return s.db.Close() }

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

// ---------------------------------------------------------------------------
// Rules CRUD
// ---------------------------------------------------------------------------

// CreateRule inserts a new alert rule and populates r.ID, r.CreatedAt, r.UpdatedAt.
func (s *Store) CreateRule(r *AlertRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	r.CreatedAt, _ = time.Parse(time.RFC3339, now)
	r.UpdatedAt = r.CreatedAt
	res, err := s.db.Exec(
		`INSERT INTO alert_rules(name, enabled, metric, hostname, condition, threshold, duration, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Name, boolToInt(r.Enabled), r.Metric, r.Hostname, r.Condition,
		r.Threshold, r.Duration, r.Description, now, now,
	)
	if err != nil {
		return fmt.Errorf("create rule: %w", err)
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

// UpdateRule updates an existing rule and resets its alert_states
// (since threshold/condition changes invalidate pending/firing).
func (s *Store) UpdateRule(id int64, req CreateRuleRequest) (*AlertRule, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	_, err := s.db.Exec(
		`UPDATE alert_rules SET name=?, enabled=?, metric=?, hostname=?, condition=?, threshold=?, duration=?, description=?, updated_at=?
		 WHERE id=?`,
		req.Name, boolToInt(enabled), req.Metric, req.Hostname, req.Condition,
		req.Threshold, req.Duration, req.Description, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("update rule: %w", err)
	}
	// Reset states — threshold change invalidates pending/firing
	_, _ = s.db.Exec(`DELETE FROM alert_state WHERE rule_id=?`, id)
	return s.GetRule(id)
}

// DeleteRule removes a rule and its associated state rows.
func (s *Store) DeleteRule(id int64) error {
	_, err := s.db.Exec(`DELETE FROM alert_rules WHERE id=?`, id)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec(`DELETE FROM alert_state WHERE rule_id=?`, id)
	return nil
}

// GetRule returns a single rule by ID. Returns sql.ErrNoRows if not found.
func (s *Store) GetRule(id int64) (*AlertRule, error) {
	row := s.db.QueryRow(
		`SELECT id, name, enabled, metric, hostname, condition, threshold, duration, description, created_at, updated_at
		 FROM alert_rules WHERE id=?`, id,
	)
	r := &AlertRule{}
	var enabled int
	var created, updated string
	if err := row.Scan(
		&r.ID, &r.Name, &enabled, &r.Metric, &r.Hostname, &r.Condition,
		&r.Threshold, &r.Duration, &r.Description, &created, &updated,
	); err != nil {
		return nil, err
	}
	r.Enabled = enabled != 0
	r.CreatedAt, _ = time.Parse(time.RFC3339, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return r, nil
}

// ListRules returns all rules ordered by ID, with States populated.
func (s *Store) ListRules() ([]AlertRule, error) {
	rows, err := s.db.Query(
		`SELECT id, name, enabled, metric, hostname, condition, threshold, duration, description, created_at, updated_at
		 FROM alert_rules ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AlertRule{}
	for rows.Next() {
		var r AlertRule
		var enabled int
		var created, updated string
		if err := rows.Scan(
			&r.ID, &r.Name, &enabled, &r.Metric, &r.Hostname, &r.Condition,
			&r.Threshold, &r.Duration, &r.Description, &created, &updated,
		); err != nil {
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

// GetEnabledRules returns only enabled rules (for evaluator consumption).
func (s *Store) GetEnabledRules() ([]AlertRule, error) {
	rows, err := s.db.Query(
		`SELECT id, name, enabled, metric, hostname, condition, threshold, duration, description, created_at, updated_at
		 FROM alert_rules WHERE enabled=1 ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AlertRule{}
	for rows.Next() {
		var r AlertRule
		var enabled int
		var created, updated string
		if err := rows.Scan(
			&r.ID, &r.Name, &enabled, &r.Metric, &r.Hostname, &r.Condition,
			&r.Threshold, &r.Duration, &r.Description, &created, &updated,
		); err != nil {
			return nil, err
		}
		r.Enabled = true
		r.CreatedAt, _ = time.Parse(time.RFC3339, created)
		r.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Alert State
// ---------------------------------------------------------------------------

func (s *Store) getStatesForRule(ruleID int64) []AlertState {
	rows, err := s.db.Query(
		`SELECT hostname, status, last_value, pending_since, firing_since,
		        acknowledged_at, silence_until, last_notified_at
		 FROM alert_state WHERE rule_id=?`, ruleID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := []AlertState{}
	for rows.Next() {
		st := AlertState{RuleID: ruleID}
		var hostname, status string
		var lastValue float64
		var pendingSince, firingSince, ackAt, silUntil, lastNotif *string
		if err := rows.Scan(
			&hostname, &status, &lastValue,
			&pendingSince, &firingSince, &ackAt, &silUntil, &lastNotif,
		); err != nil {
			continue
		}
		st.Hostname = hostname
		st.Status = status
		st.LastValue = lastValue
		st.PendingSince = parsePtrTime(pendingSince)
		st.FiringSince = parsePtrTime(firingSince)
		st.AcknowledgedAt = parsePtrTime(ackAt)
		st.SilenceUntil = parsePtrTime(silUntil)
		st.LastNotifiedAt = parsePtrTime(lastNotif)
		out = append(out, st)
	}
	return out
}

// GetState returns the current state for a (rule, host), creating a default
// "ok" row if none exists.
func (s *Store) GetState(ruleID int64, hostname string) (*AlertState, error) {
	row := s.db.QueryRow(
		`SELECT status, last_value, pending_since, firing_since,
		        acknowledged_at, silence_until, last_notified_at
		 FROM alert_state WHERE rule_id=? AND hostname=?`, ruleID, hostname,
	)
	st := &AlertState{RuleID: ruleID, Hostname: hostname}
	var status string
	var lastValue float64
	var pendingSince, firingSince, ackAt, silUntil, lastNotif *string
	err := row.Scan(
		&status, &lastValue,
		&pendingSince, &firingSince, &ackAt, &silUntil, &lastNotif,
	)
	if err == sql.ErrNoRows {
		st.Status = "ok"
		_, err = s.db.Exec(
			`INSERT INTO alert_state(rule_id, hostname, status, last_value) VALUES (?, ?, 'ok', 0)`,
			ruleID, hostname,
		)
		return st, err
	}
	if err != nil {
		return nil, err
	}
	st.Status = status
	st.LastValue = lastValue
	st.PendingSince = parsePtrTime(pendingSince)
	st.FiringSince = parsePtrTime(firingSince)
	st.AcknowledgedAt = parsePtrTime(ackAt)
	st.SilenceUntil = parsePtrTime(silUntil)
	st.LastNotifiedAt = parsePtrTime(lastNotif)
	return st, nil
}

// SetState updates the status, last_value, and optionally pending_since or
// firing_since for a (rule, host). Uses upsert to handle first-time insert.
func (s *Store) SetState(ruleID int64, hostname, status string, lastValue float64, pendingSince, firingSince *time.Time) error {
	var ps, fs *string
	if pendingSince != nil {
		v := pendingSince.UTC().Format(time.RFC3339)
		ps = &v
	}
	if firingSince != nil {
		v := firingSince.UTC().Format(time.RFC3339)
		fs = &v
	}
	_, err := s.db.Exec(
		`INSERT INTO alert_state(rule_id, hostname, status, last_value, pending_since, firing_since)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(rule_id, hostname) DO UPDATE SET
		   status=excluded.status, last_value=excluded.last_value,
		   pending_since=COALESCE(excluded.pending_since, alert_state.pending_since),
		   firing_since=COALESCE(excluded.firing_since, alert_state.firing_since)`,
		ruleID, hostname, status, lastValue, ps, fs,
	)
	return err
}

// SetAcknowledged marks a firing alert as acknowledged, optionally with a
// silence deadline.
func (s *Store) SetAcknowledged(ruleID int64, hostname string, silenceUntil *time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var su *string
	if silenceUntil != nil {
		v := silenceUntil.UTC().Format(time.RFC3339)
		su = &v
	}
	_, err := s.db.Exec(
		`UPDATE alert_state SET acknowledged_at=?, silence_until=? WHERE rule_id=? AND hostname=?`,
		now, su, ruleID, hostname,
	)
	return err
}

// ClearAck clears acknowledgment and silence fields.
func (s *Store) ClearAck(ruleID int64, hostname string) error {
	_, err := s.db.Exec(
		`UPDATE alert_state SET acknowledged_at=NULL, silence_until=NULL WHERE rule_id=? AND hostname=?`,
		ruleID, hostname,
	)
	return err
}

// SetLastNotified records the last notification timestamp for rate-limiting.
func (s *Store) SetLastNotified(ruleID int64, hostname string, t time.Time) error {
	ts := t.UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`UPDATE alert_state SET last_notified_at=? WHERE rule_id=? AND hostname=?`,
		ts, ruleID, hostname,
	)
	return err
}

// ---------------------------------------------------------------------------
// Alert History
// ---------------------------------------------------------------------------

// AddHistory records a firing or recovered event.
func (s *Store) AddHistory(ruleID int64, ruleName, hostname string, value float64, state string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO alert_history(rule_id, rule_name, hostname, metric_value, state, acknowledged, triggered_at)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		ruleID, ruleName, hostname, value, state, now,
	)
	return err
}

// ListHistory returns paginated history items with optional filters.
// Returns (items, totalCount, error).
func (s *Store) ListHistory(ruleID int64, hostname, state string, limit, offset int) ([]HistoryItem, int, error) {
	// Build WHERE clause
	var where string
	var args []any
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
	if where != "" {
		where = "WHERE " + where[5:] // strip leading " AND "
	}

	// Count
	var total int
	countSQL := "SELECT COUNT(*) FROM alert_history " + where
	err := s.db.QueryRow(countSQL, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// Query
	queryArgs := make([]any, 0, len(args)+2)
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, limit, offset)

	rows, err := s.db.Query(
		`SELECT id, rule_id, rule_name, hostname, metric_value, state, acknowledged, triggered_at
		 FROM alert_history `+where+` ORDER BY triggered_at DESC LIMIT ? OFFSET ?`,
		queryArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []HistoryItem{}
	for rows.Next() {
		var h HistoryItem
		var ack int
		var triggered string
		if err := rows.Scan(
			&h.ID, &h.RuleID, &h.RuleName, &h.Hostname,
			&h.MetricValue, &h.State, &ack, &triggered,
		); err != nil {
			return nil, 0, err
		}
		h.Acknowledged = ack != 0
		h.TriggeredAt, _ = time.Parse(time.RFC3339, triggered)
		out = append(out, h)
	}
	return out, total, rows.Err()
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parsePtrTime(s *string) *time.Time {
	if s == nil {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil
	}
	return &t
}

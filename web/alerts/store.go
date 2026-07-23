// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License

package alerts

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Store provides CRUD operations for alert rules, history, and state.
//
// Schema is destructive across versions: NewStore drops any pre-existing
// alert_rules / alert_state / alert_history tables and recreates them
// under the new PromQL-only schema. The caller is responsible for the
// *sql.DB lifecycle (PRAGMA setup, etc.) — Store only manages its own
// schema.
type Store struct {
	db *sql.DB
}

// NewStore initializes the alert store, applying the destructive schema
// migration.
func NewStore(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("alert migrate: %w", err)
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }

// migrate wipes legacy alert tables and installs the new PromQL-only
// schema. There is no compatibility with the pre-PromQL schema on purpose.
func (s *Store) migrate() error {
	schema := `
	DROP TABLE IF EXISTS alert_rules;
	DROP TABLE IF EXISTS alert_state;
	DROP TABLE IF EXISTS alert_history;

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
	`
	_, err := s.db.Exec(schema)
	return err
}

// ---------------------------------------------------------------------------
// Rules CRUD
// ---------------------------------------------------------------------------

// CreateRule inserts a new rule and populates r.ID, r.CreatedAt, r.UpdatedAt.
func (s *Store) CreateRule(r *AlertRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	r.CreatedAt = NewTime(parseRFC3339OrZero(now))
	r.UpdatedAt = r.CreatedAt
	res, err := s.db.Exec(
		`INSERT INTO alert_rules(name, enabled, expr, duration, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Name, boolToInt(r.Enabled), r.Expr,
		r.Duration, r.Description, now, now,
	)
	if err != nil {
		return fmt.Errorf("create rule: %w", err)
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

// UpdateRule updates an existing rule and resets its alert_states (since
// expression changes invalidate pending/firing).
func (s *Store) UpdateRule(id int64, req CreateRuleRequest) (*AlertRule, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	_, err := s.db.Exec(
		`UPDATE alert_rules SET name=?, enabled=?, expr=?, duration=?, description=?, updated_at=?
		 WHERE id=?`,
		req.Name, boolToInt(enabled), req.Expr, req.Duration, req.Description, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("update rule: %w", err)
	}
	// Expression or threshold changes invalidate pending/firing state.
	_, _ = s.db.Exec(`DELETE FROM alert_state WHERE rule_id=?`, id)
	return s.GetRule(id)
}

// DeleteRule removes a rule and its associated state rows.
func (s *Store) DeleteRule(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM alert_rules WHERE id=?`, id); err != nil {
		return err
	}
	_, _ = s.db.Exec(`DELETE FROM alert_state WHERE rule_id=?`, id)
	_, _ = s.db.Exec(`DELETE FROM alert_history WHERE rule_id=?`, id)
	return nil
}

// GetRule returns a single rule by ID. Returns sql.ErrNoRows if not found.
func (s *Store) GetRule(id int64) (*AlertRule, error) {
	row := s.db.QueryRow(
		`SELECT id, name, enabled, expr, duration, description, created_at, updated_at
		 FROM alert_rules WHERE id=?`, id,
	)
	r := &AlertRule{}
	var enabled int
	var created, updated string
	if err := row.Scan(
		&r.ID, &r.Name, &enabled, &r.Expr, &r.Duration, &r.Description, &created, &updated,
	); err != nil {
		return nil, err
	}
	r.Enabled = enabled != 0
	r.CreatedAt = NewTime(parseRFC3339OrZero(created))
	r.UpdatedAt = NewTime(parseRFC3339OrZero(updated))
	return r, nil
}

// ListRules returns all rules ordered by ID, with States populated.
func (s *Store) ListRules() ([]AlertRule, error) {
	rows, err := s.db.Query(
		`SELECT id, name, enabled, expr, duration, description, created_at, updated_at
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
			&r.ID, &r.Name, &enabled, &r.Expr, &r.Duration, &r.Description, &created, &updated,
		); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		r.CreatedAt = NewTime(parseRFC3339OrZero(created))
		r.UpdatedAt = NewTime(parseRFC3339OrZero(updated))
		r.States = s.getStatesForRule(r.ID)
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetEnabledRules returns only enabled rules (for evaluator consumption).
func (s *Store) GetEnabledRules() ([]AlertRule, error) {
	rows, err := s.db.Query(
		`SELECT id, name, enabled, expr, duration, description, created_at, updated_at
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
			&r.ID, &r.Name, &enabled, &r.Expr, &r.Duration, &r.Description, &created, &updated,
		); err != nil {
			return nil, err
		}
		r.Enabled = true
		r.CreatedAt = NewTime(parseRFC3339OrZero(created))
		r.UpdatedAt = NewTime(parseRFC3339OrZero(updated))
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Alert State
// ---------------------------------------------------------------------------

func (s *Store) getStatesForRule(ruleID int64) []AlertState {
	rows, err := s.db.Query(
		`SELECT fingerprint, labels_json, status, last_value, pending_since, firing_since,
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
		var labelsJSON string
		var pendingSince, firingSince, ackAt, silUntil, lastNotif *string
		if err := rows.Scan(
			&st.Fingerprint, &labelsJSON, &st.Status, &st.LastValue,
			&pendingSince, &firingSince, &ackAt, &silUntil, &lastNotif,
		); err != nil {
			continue
		}
		st.Labels = decodeLabels(labelsJSON)
		st.Hostname = st.Labels["hostname"]
		st.PendingSince = parsePtrTime(pendingSince)
		st.FiringSince = parsePtrTime(firingSince)
		st.AcknowledgedAt = parsePtrTime(ackAt)
		st.SilenceUntil = parsePtrTime(silUntil)
		st.LastNotifiedAt = parsePtrTime(lastNotif)
		out = append(out, st)
	}
	return out
}

// GetActiveStatesForRule returns all non-OK states (pending + firing) for
// the evaluator diff logic.
func (s *Store) GetActiveStatesForRule(ruleID int64) ([]AlertState, error) {
	rows, err := s.db.Query(
		`SELECT fingerprint, labels_json, status, last_value, pending_since, firing_since,
		        acknowledged_at, silence_until, last_notified_at
		 FROM alert_state WHERE rule_id=? AND status IN ('pending','firing')`, ruleID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AlertState{}
	for rows.Next() {
		st := AlertState{RuleID: ruleID}
		var labelsJSON string
		var pendingSince, firingSince, ackAt, silUntil, lastNotif *string
		if err := rows.Scan(
			&st.Fingerprint, &labelsJSON, &st.Status, &st.LastValue,
			&pendingSince, &firingSince, &ackAt, &silUntil, &lastNotif,
		); err != nil {
			return nil, err
		}
		st.Labels = decodeLabels(labelsJSON)
		st.Hostname = st.Labels["hostname"]
		st.PendingSince = parsePtrTime(pendingSince)
		st.FiringSince = parsePtrTime(firingSince)
		st.AcknowledgedAt = parsePtrTime(ackAt)
		st.SilenceUntil = parsePtrTime(silUntil)
		st.LastNotifiedAt = parsePtrTime(lastNotif)
		out = append(out, st)
	}
	return out, rows.Err()
}

// UpsertState inserts or updates the state row for (ruleID, fingerprint).
func (s *Store) UpsertState(ruleID int64, fingerprint string, labels map[string]string,
	status string, lastValue float64,
	pendingSince, firingSince *time.Time,
) error {
	labelsJSON := encodeLabels(labels)
	ps := formatPtrTime(pendingSince)
	fs := formatPtrTime(firingSince)
	_, err := s.db.Exec(
		`INSERT INTO alert_state(rule_id, fingerprint, labels_json, status, last_value, pending_since, firing_since)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(rule_id, fingerprint) DO UPDATE SET
		   labels_json=excluded.labels_json,
		   status=excluded.status,
		   last_value=excluded.last_value,
		   pending_since=excluded.pending_since,
		   firing_since=excluded.firing_since`,
		ruleID, fingerprint, labelsJSON, status, lastValue, ps, fs,
	)
	if err != nil {
		return fmt.Errorf("upsert state: %w", err)
	}
	return nil
}

// SetAcknowledged marks a firing alert as acknowledged, optionally with a
// silence deadline. Operates on (rule_id, fingerprint).
func (s *Store) SetAcknowledged(ruleID int64, fingerprint string, silenceUntil *time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	su := formatPtrTime(silenceUntil)
	_, err := s.db.Exec(
		`UPDATE alert_state SET acknowledged_at=?, silence_until=? WHERE rule_id=? AND fingerprint=?`,
		now, su, ruleID, fingerprint,
	)
	return err
}

// ClearAck clears acknowledgment and silence fields for an instance.
func (s *Store) ClearAck(ruleID int64, fingerprint string) error {
	_, err := s.db.Exec(
		`UPDATE alert_state SET acknowledged_at=NULL, silence_until=NULL WHERE rule_id=? AND fingerprint=?`,
		ruleID, fingerprint,
	)
	return err
}

// SetLastNotified records the last notification timestamp for rate-limiting.
func (s *Store) SetLastNotified(ruleID int64, fingerprint string, t time.Time) error {
	ts := t.UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`UPDATE alert_state SET last_notified_at=? WHERE rule_id=? AND fingerprint=?`,
		ts, ruleID, fingerprint,
	)
	return err
}

// ---------------------------------------------------------------------------
// Alert History
// ---------------------------------------------------------------------------

// AddHistory records a firing or recovered event.
func (s *Store) AddHistory(ruleID int64, ruleName, fingerprint string,
	labels map[string]string, expr string, value float64, state string,
) error {
	now := time.Now().UTC().Format(time.RFC3339)
	labelsJSON := encodeLabels(labels)
	_, err := s.db.Exec(
		`INSERT INTO alert_history(rule_id, rule_name, fingerprint, labels_json, expr, metric_value, state, acknowledged, triggered_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		ruleID, ruleName, fingerprint, labelsJSON, expr, value, state, now,
	)
	return err
}

// ListHistory returns paginated history items with optional filters.
func (s *Store) ListHistory(ruleID int64, fingerprint, state string, limit, offset int) ([]HistoryItem, int, error) {
	var where string
	var args []any
	if ruleID > 0 {
		where += " AND rule_id=?"
		args = append(args, ruleID)
	}
	if fingerprint != "" {
		where += " AND fingerprint=?"
		args = append(args, fingerprint)
	}
	if state != "" {
		where += " AND state=?"
		args = append(args, state)
	}
	if where != "" {
		where = "WHERE " + where[5:] // strip leading " AND "
	}

	var total int
	countSQL := "SELECT COUNT(*) FROM alert_history " + where
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	queryArgs := make([]any, 0, len(args)+2)
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, limit, offset)

	rows, err := s.db.Query(
		`SELECT id, rule_id, rule_name, fingerprint, labels_json, expr, metric_value, state, acknowledged, triggered_at
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
		var labelsJSON, triggered string
		var ack int
		if err := rows.Scan(
			&h.ID, &h.RuleID, &h.RuleName, &h.Fingerprint, &labelsJSON, &h.Expr,
			&h.MetricValue, &h.State, &ack, &triggered,
		); err != nil {
			return nil, 0, err
		}
		h.Labels = decodeLabels(labelsJSON)
		h.Hostname = h.Labels["hostname"]
		h.Acknowledged = ack != 0
		h.TriggeredAt = NewTime(parseRFC3339OrZero(triggered))
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

func parsePtrTime(s *string) *Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil
	}
	out := NewTime(t)
	return &out
}

func formatPtrTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func parseRFC3339OrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// encodeLabels turns a label map into a stable JSON string. We sort keys
// so the encoding is deterministic, which keeps alert_state rows
// idempotent even if Go's map iteration order changes.
func encodeLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf strings.Builder
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(labels[k])
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.String()
}

func decodeLabels(s string) map[string]string {
	out := map[string]string{}
	if s == "" {
		return out
	}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

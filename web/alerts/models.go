// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License

// Package alerts defines alert domain types and interfaces.
//
// Layering: this package owns the EmailSender interface that the smtp package
// implements, and depends on web/vm for PromQL query results so the
// evaluator can drive VictoriaMetrics directly without an adapter layer.
package alerts

import (
	"context"

	"github.com/abrance/monitorbeat/web/vm"
)

// AlertRule is a PromQL-driven alert rule. The Expr returns the set of
// "currently breaching" series; each series becomes one alert instance.
type AlertRule struct {
	ID          int64        `json:"id"`
	Name        string       `json:"name"`
	Enabled     bool         `json:"enabled"`
	Expr        string       `json:"expr"`
	Duration    int64        `json:"duration"` // seconds condition must persist
	Description string       `json:"description"`
	CreatedAt   Time         `json:"created_at"`
	UpdatedAt   Time         `json:"updated_at"`
	States      []AlertState `json:"states"` // populated by ListRules
}

// AlertState tracks the per-rule evaluation state for a single alert
// instance, identified by Fingerprint (the hash of the series labels).
type AlertState struct {
	RuleID         int64             `json:"-"`
	Fingerprint    string            `json:"fingerprint"`
	Labels         map[string]string `json:"labels"`
	Hostname       string            `json:"hostname"` // convenience copy of labels["hostname"]
	Status         string            `json:"status"`   // "ok" | "pending" | "firing"
	LastValue      float64           `json:"last_value"`
	PendingSince   *Time             `json:"-"`
	FiringSince    *Time             `json:"-"`
	AcknowledgedAt *Time             `json:"-"`
	SilenceUntil   *Time             `json:"silence_until"`
	LastNotifiedAt *Time             `json:"-"`
}

// HistoryItem records a single firing or recovered event for an alert
// instance.
type HistoryItem struct {
	ID           int64             `json:"id"`
	RuleID       int64             `json:"rule_id"`
	RuleName     string            `json:"rule_name"`
	Fingerprint  string            `json:"fingerprint"`
	Labels       map[string]string `json:"labels"`
	Hostname     string            `json:"hostname"` // convenience copy of labels["hostname"]
	Expr         string            `json:"expr"`
	MetricValue  float64           `json:"metric_value"`
	State        string            `json:"state"` // "firing" | "recovered"
	Acknowledged bool              `json:"acknowledged"`
	TriggeredAt  Time              `json:"triggered_at"`
}

// CreateRuleRequest is the JSON body for creating or updating a rule.
type CreateRuleRequest struct {
	Name        string `json:"name"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Expr        string `json:"expr"`
	Duration    int64  `json:"duration"`
	Description string `json:"description"`
}

// AckRequest is the JSON body for acknowledging a firing instance.
type AckRequest struct {
	RuleID       int64  `json:"rule_id"`
	Fingerprint  string `json:"fingerprint"`
	SilenceHours int    `json:"silence_hours"` // 0 = acknowledge current firing only
}

// TestQueryRequest is the JSON body for the test-query endpoint.
type TestQueryRequest struct {
	Expr string `json:"expr"`
}

// TestQueryInstance is one matching series for the test-query endpoint.
type TestQueryInstance struct {
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels"`
	Value       float64           `json:"value"`
}

// TestQueryResponse is the JSON body returned by the test-query endpoint.
type TestQueryResponse struct {
	Result []TestQueryInstance `json:"result"`
}

// EmailSender is the interface for sending alert notifications.
// Implementations: smtp.Sender.
type EmailSender interface {
	SendAlert(rule AlertRule, hostname string, labels map[string]string, value float64, state string) error
}

// VMQuerier is the minimal VM query interface the evaluator needs.
// Implemented by web/vm.Client.
type VMQuerier interface {
	Query(ctx context.Context, expr string) ([]vm.Vector, error)
}

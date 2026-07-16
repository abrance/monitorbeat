// Package alerts defines alert domain types and interfaces.
//
// Layering: This package defines the EmailSender and VMQuerier interfaces
// that infrastructure packages (smtp, vm) implement. No direct dependency
// on HTTP, SMTP, or storage details.
package alerts

import (
	"context"
	"time"
)

// AlertRule is a threshold-based alert rule.
type AlertRule struct {
	ID          int64        `json:"id"`
	Name        string       `json:"name"`
	Enabled     bool         `json:"enabled"`
	Metric      string       `json:"metric"`    // PromQL metric name
	Hostname    string       `json:"hostname"`  // "" = all hosts
	Condition   string       `json:"condition"` // "gt" or "lt"
	Threshold   float64      `json:"threshold"`
	Duration    int64        `json:"duration"` // seconds condition must persist
	Description string       `json:"description"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	States      []AlertState `json:"states"` // populated by ListRules
}

// HistoryItem is a single alert fire or recover event.
type HistoryItem struct {
	ID           int64     `json:"id"`
	RuleID       int64     `json:"rule_id"`
	RuleName     string    `json:"rule_name"`
	Hostname     string    `json:"hostname"`
	MetricValue  float64   `json:"metric_value"`
	State        string    `json:"state"` // "firing" | "recovered"
	Acknowledged bool      `json:"acknowledged"`
	TriggeredAt  time.Time `json:"triggered_at"`
}

// AlertState is the per-(rule, host) evaluation state.
//
// Status values: "ok" | "pending" | "firing". There is no separate
// "acknowledged" status — acknowledgment is tracked via AcknowledgedAt.
type AlertState struct {
	RuleID         int64      `json:"-"`
	Hostname       string     `json:"hostname"`
	Status         string     `json:"status"` // "ok" | "pending" | "firing"
	LastValue      float64    `json:"last_value"`
	PendingSince   *time.Time `json:"-"`
	FiringSince    *time.Time `json:"-"`
	AcknowledgedAt *time.Time `json:"-"` // non-nil = user acknowledged
	SilenceUntil   *time.Time `json:"silence_until"`
	LastNotifiedAt *time.Time `json:"-"`
}

// CreateRuleRequest is the JSON body for creating or updating a rule.
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
	RuleID       int64  `json:"rule_id"`
	Hostname     string `json:"hostname"`
	SilenceHours int    `json:"silence_hours"` // 0 = acknowledge current firing only
}

// EmailSender is the interface for sending alert notifications.
// Implementations: smtp.Sender.
type EmailSender interface {
	SendAlert(rule AlertRule, hostname string, value float64, state string) error
}

// VMQuerier is the minimal VM query interface the evaluator needs.
// Bridge from vm.Client via vmAdapter in main.go.
type VMQuerier interface {
	Query(ctx context.Context, expr string) ([]VectorResult, error)
}

// VectorResult is a simplified VM instant query result.
type VectorResult struct {
	Metric map[string]string
	Value  [2]float64 // [timestamp, value]
}

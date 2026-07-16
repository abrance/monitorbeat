package alerts

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Evaluator runs the alert evaluation loop in a background goroutine.
// It periodically fetches enabled rules, queries VM for each rule's
// metric, compares against thresholds, and triggers notifications.
type Evaluator struct {
	store    *Store
	vm       VMQuerier
	email    EmailSender
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewEvaluator creates an evaluator. Call Start() to begin the loop.
func NewEvaluator(store *Store, vm VMQuerier, email EmailSender, interval time.Duration) *Evaluator {
	ctx, cancel := context.WithCancel(context.Background())
	return &Evaluator{
		store:    store,
		vm:       vm,
		email:    email,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins the evaluation loop in a background goroutine.
func (e *Evaluator) Start() {
	go e.loop()
	slog.Info("alert evaluator started", "interval", e.interval)
}

// Stop terminates the evaluation loop.
func (e *Evaluator) Stop() {
	e.cancel()
}

func (e *Evaluator) loop() {
	// Run once immediately, then every interval
	e.evaluate()
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
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
	if len(results) == 0 {
		// No data — check if rule has fixed hostname to evaluate as "not breached"
		if rule.Hostname != "" {
			e.evaluateHost(rule, rule.Hostname, 0)
		}
		return
	}
	for _, vec := range results {
		host := vec.Metric["hostname"]
		if host == "" {
			host = rule.Hostname
		}
		if host == "" {
			continue // can't determine host
		}
		e.evaluateHost(rule, host, vec.Value[1])
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
			since := st.PendingSince
			if since == nil {
				since = &now
			}
			if now.Sub(*since) >= time.Duration(rule.Duration)*time.Second {
				e.store.SetState(rule.ID, hostname, "firing", value, nil, &now)
				e.fire(rule, hostname, value)
			}
		} else {
			e.store.SetState(rule.ID, hostname, "ok", value, nil, nil)
		}

	case "firing":
		if breached {
			// Acknowledged — skip email
			if st.AcknowledgedAt != nil {
				return
			}
			// Silenced — skip email
			if st.SilenceUntil != nil && now.Before(*st.SilenceUntil) {
				return
			}
			// Silence expired — clear it
			if st.SilenceUntil != nil && !now.Before(*st.SilenceUntil) {
				e.store.ClearAck(rule.ID, hostname)
			}
			// Rate limit: at least 5 min between notifications
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

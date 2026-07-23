// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License

package alerts

import (
	"context"
	"log/slog"
	"time"
)

// Evaluator runs the alert evaluation loop in a background goroutine.
// Each tick it queries every enabled rule's PromQL expression, then
// reconciles the result against the rule's persisted alert state rows
// (keyed by series fingerprint).
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
	// Run once immediately, then every interval.
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

// instance is the in-memory view of a single alert instance returned by
// a rule's PromQL expression this tick.
type instance struct {
	fingerprint string
	labels      map[string]string
	value       float64
}

// evaluateRule reconciles rule.Expr's current series against the rule's
// persisted pending/firing state rows. State transitions:
//
//	current   historical   action
//	yes       none         write pending
//	yes       pending      if duration elapsed → firing + fire()
//	                       else stay pending (refresh last_value)
//	yes       firing       respect ack/silence/rate-limit; fire() if due
//	no        pending      write ok
//	no        firing       recover() + write ok
func (e *Evaluator) evaluateRule(rule AlertRule) {
	results, err := e.vm.Query(context.Background(), rule.Expr)
	if err != nil {
		slog.Error("alert eval: vm query", "rule", rule.Name, "err", err)
		return // VM failure: keep current state
	}

	current := make(map[string]instance, len(results))
	for _, vec := range results {
		labels := vec.Metric
		if labels == nil {
			labels = map[string]string{}
		}
		fp := Fingerprint(labels)
		current[fp] = instance{
			fingerprint: fp,
			labels:      labels,
			value:       vec.Value[1],
		}
	}

	historical, err := e.store.GetActiveStatesForRule(rule.ID)
	if err != nil {
		slog.Error("alert eval: get states", "rule", rule.Name, "err", err)
		return
	}

	now := time.Now()

	for fp, inst := range current {
		h := findState(historical, fp)
		switch {
		case h == nil:
			// New breaching series: enter pending. A duration of zero
			// skips pending and fires on this same tick (matches
			// Prometheus alert rule `for: 0s` semantics).
			if rule.Duration == 0 {
				if err := e.store.UpsertState(rule.ID, fp, inst.labels, "firing", inst.value, nil, &now); err != nil {
					slog.Error("alert eval: upsert firing", "rule", rule.Name, "err", err)
				}
				e.fire(rule, inst)
			} else {
				if err := e.store.UpsertState(rule.ID, fp, inst.labels, "pending", inst.value, &now, nil); err != nil {
					slog.Error("alert eval: upsert pending", "rule", rule.Name, "err", err)
				}
			}
		case h.Status == "pending":
			pendingSince := now
			if h.PendingSince != nil && !h.PendingSince.IsZero() {
				pendingSince = h.PendingSince.Time
			}
			if now.Sub(pendingSince) >= time.Duration(rule.Duration)*time.Second {
				if err := e.store.UpsertState(rule.ID, fp, inst.labels, "firing", inst.value, nil, &now); err != nil {
					slog.Error("alert eval: upsert firing", "rule", rule.Name, "err", err)
				}
				e.fire(rule, inst)
			} else {
				if err := e.store.UpsertState(rule.ID, fp, inst.labels, "pending", inst.value, &pendingSince, nil); err != nil {
					slog.Error("alert eval: refresh pending", "rule", rule.Name, "err", err)
				}
			}
		case h.Status == "firing":
			firingSince := now
			if h.FiringSince != nil && !h.FiringSince.IsZero() {
				firingSince = h.FiringSince.Time
			}
			if h.AcknowledgedAt != nil && !h.AcknowledgedAt.IsZero() {
				if err := e.store.UpsertState(rule.ID, fp, inst.labels, "firing", inst.value, nil, &firingSince); err != nil {
					slog.Error("alert eval: refresh firing (ack)", "rule", rule.Name, "err", err)
				}
				continue
			}
			if h.SilenceUntil != nil && !h.SilenceUntil.IsZero() && now.Before(h.SilenceUntil.Time) {
				if err := e.store.UpsertState(rule.ID, fp, inst.labels, "firing", inst.value, nil, &firingSince); err != nil {
					slog.Error("alert eval: refresh firing (silence)", "rule", rule.Name, "err", err)
				}
				continue
			}
			if h.SilenceUntil != nil && !h.SilenceUntil.IsZero() && !now.Before(h.SilenceUntil.Time) {
				_ = e.store.ClearAck(rule.ID, fp)
			}
			if h.LastNotifiedAt != nil && !h.LastNotifiedAt.IsZero() && now.Sub(h.LastNotifiedAt.Time) < 5*time.Minute {
				if err := e.store.UpsertState(rule.ID, fp, inst.labels, "firing", inst.value, nil, &firingSince); err != nil {
					slog.Error("alert eval: refresh firing (rate-limit)", "rule", rule.Name, "err", err)
				}
				continue
			}
			if err := e.store.UpsertState(rule.ID, fp, inst.labels, "firing", inst.value, nil, &firingSince); err != nil {
				slog.Error("alert eval: refresh firing", "rule", rule.Name, "err", err)
			}
			e.fire(rule, inst)
		}
	}

	// Historical series no longer present: resolve them.
	for i := range historical {
		h := &historical[i]
		if _, ok := current[h.Fingerprint]; ok {
			continue
		}
		switch h.Status {
		case "pending":
			if err := e.store.UpsertState(rule.ID, h.Fingerprint, h.Labels, "ok", h.LastValue, nil, nil); err != nil {
				slog.Error("alert eval: clear pending", "rule", rule.Name, "err", err)
			}
		case "firing":
			recoverValue := h.LastValue
			recoverLabels := h.Labels
			if err := e.store.UpsertState(rule.ID, h.Fingerprint, h.Labels, "ok", recoverValue, nil, nil); err != nil {
				slog.Error("alert eval: clear firing", "rule", rule.Name, "err", err)
			}
			_ = e.store.ClearAck(rule.ID, h.Fingerprint)
			e.recover(rule, recoverLabels, recoverValue)
		}
	}
}

func findState(states []AlertState, fp string) *AlertState {
	for i := range states {
		if states[i].Fingerprint == fp {
			return &states[i]
		}
	}
	return nil
}

func (e *Evaluator) fire(rule AlertRule, inst instance) {
	hostname := inst.labels["hostname"]
	slog.Warn("alert firing",
		"rule", rule.Name,
		"host", hostname,
		"fingerprint", inst.fingerprint,
		"value", inst.value,
	)
	if err := e.store.AddHistory(rule.ID, rule.Name, inst.fingerprint, inst.labels, rule.Expr, inst.value, "firing"); err != nil {
		slog.Error("alert fire: history", "err", err)
	}
	if err := e.store.SetLastNotified(rule.ID, inst.fingerprint, time.Now()); err != nil {
		slog.Error("alert fire: update notified", "err", err)
	}
	if e.email != nil {
		if err := e.email.SendAlert(rule, hostname, inst.labels, inst.value, "firing"); err != nil {
			slog.Error("alert fire: email", "err", err)
		}
	}
}

func (e *Evaluator) recover(rule AlertRule, labels map[string]string, value float64) {
	hostname := labels["hostname"]
	slog.Info("alert recovered",
		"rule", rule.Name,
		"host", hostname,
		"value", value,
	)
	fp := Fingerprint(labels)
	if err := e.store.AddHistory(rule.ID, rule.Name, fp, labels, rule.Expr, value, "recovered"); err != nil {
		slog.Error("alert recover: history", "err", err)
	}
	if e.email != nil {
		if err := e.email.SendAlert(rule, hostname, labels, value, "recovered"); err != nil {
			slog.Error("alert recover: email", "err", err)
		}
	}
}

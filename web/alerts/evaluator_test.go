// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License

package alerts

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/abrance/monitorbeat/web/vm"
)

// --- fakes ---

type fakeQuerier struct {
	mu    sync.Mutex
	reply []vm.Vector
	err   error
	calls int
}

func (f *fakeQuerier) Query(_ context.Context, _ string) ([]vm.Vector, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]vm.Vector, len(f.reply))
	copy(out, f.reply)
	return out, nil
}

type fakeSender struct {
	mu       sync.Mutex
	fires    []sendCall
	recovers []sendCall
}

type sendCall struct {
	RuleName string
	Hostname string
	State    string
}

func (f *fakeSender) SendAlert(rule AlertRule, hostname string, _ map[string]string, _ float64, state string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := sendCall{RuleName: rule.Name, Hostname: hostname, State: state}
	if state == "firing" {
		f.fires = append(f.fires, call)
	} else if state == "recovered" {
		f.recovers = append(f.recovers, call)
	}
	return nil
}

// openStore builds an in-memory store and applies the destructive schema.
func openStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := NewStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s
}

func newRule(t *testing.T, store *Store, expr string, duration int64) AlertRule {
	t.Helper()
	rule := &AlertRule{
		Name:     "test-rule",
		Enabled:  true,
		Expr:     expr,
		Duration: duration,
	}
	if err := store.CreateRule(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	return *rule
}

func vec(host, target string, val float64) vm.Vector {
	now := float64(time.Now().Unix())
	return vm.Vector{
		Metric: map[string]string{"hostname": host, "target": target},
		Value:  [2]float64{now, val},
	}
}

// --- tests ---

func TestEvaluatorFiringThenRecoveredSendsBothEmails(t *testing.T) {
	store := openStore(t)
	q := &fakeQuerier{reply: []vm.Vector{vec("node-a", "https://x", 0)}}
	s := &fakeSender{}
	e := NewEvaluator(store, q, s, time.Hour)

	rule := newRule(t, store, "x == 0", 0)

	// Tick 1: series present → firing + email.
	e.evaluate()
	if len(s.fires) != 1 {
		t.Fatalf("expected 1 fire, got %d", len(s.fires))
	}
	if len(s.recovers) != 0 {
		t.Fatalf("expected no recover yet, got %d", len(s.recovers))
	}

	// Tick 2: series gone → recovered + email (key fix: even though PromQL
	// returns empty, the diff against historical firing must still trigger).
	q.reply = nil
	e.evaluate()
	if len(s.recovers) != 1 {
		t.Fatalf("expected 1 recover, got %d", len(s.recovers))
	}
	if s.recovers[0].RuleName != rule.Name || s.recovers[0].State != "recovered" {
		t.Fatalf("unexpected recover call: %+v", s.recovers[0])
	}

	// History should now be empty (state row gone / set to ok).
	historical, err := store.GetActiveStatesForRule(rule.ID)
	if err != nil {
		t.Fatalf("get active states: %v", err)
	}
	if len(historical) != 0 {
		t.Fatalf("expected no active states after recover, got %d", len(historical))
	}
}

func TestEvaluatorPendingRespectsDuration(t *testing.T) {
	store := openStore(t)
	q := &fakeQuerier{reply: []vm.Vector{vec("node-a", "https://x", 0)}}
	s := &fakeSender{}
	// 1-hour duration so the first tick can never cross it.
	e := NewEvaluator(store, q, s, time.Hour)

	rule := newRule(t, store, "x == 0", 3600)

	e.evaluate()
	if len(s.fires) != 0 {
		t.Fatalf("expected no fire yet (still pending), got %d", len(s.fires))
	}
	historical, err := store.GetActiveStatesForRule(rule.ID)
	if err != nil {
		t.Fatalf("get active states: %v", err)
	}
	if len(historical) != 1 || historical[0].Status != "pending" {
		t.Fatalf("expected 1 pending state, got %+v", historical)
	}
}

func TestEvaluatorPendingDurationElapsedFires(t *testing.T) {
	store := openStore(t)
	q := &fakeQuerier{reply: []vm.Vector{vec("node-a", "https://x", 0)}}
	s := &fakeSender{}
	e := NewEvaluator(store, q, s, time.Hour)

	_ = newRule(t, store, "x == 0", 0)

	// duration=0 means "fire immediately", which is what NewEvaluator's
	// "now - pending_since >= 0" check ensures.
	e.evaluate()
	if len(s.fires) != 1 {
		t.Fatalf("expected 1 fire, got %d", len(s.fires))
	}
}

func TestEvaluatorVMErrorPreservesState(t *testing.T) {
	store := openStore(t)
	q := &fakeQuerier{err: errors.New("vm offline")}
	s := &fakeSender{}

	rule := newRule(t, store, "x == 0", 0)

	// Seed an active firing row directly to verify it survives a VM error.
	fp := Fingerprint(map[string]string{"hostname": "node-a"})
	if err := store.UpsertState(rule.ID, fp, map[string]string{"hostname": "node-a"}, "firing", 1.0, nil, ptrTime(time.Now())); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	e := NewEvaluator(store, q, s, time.Hour)
	e.evaluate()

	historical, err := store.GetActiveStatesForRule(rule.ID)
	if err != nil {
		t.Fatalf("get active states: %v", err)
	}
	if len(historical) != 1 || historical[0].Status != "firing" {
		t.Fatalf("VM error must preserve state, got %+v", historical)
	}
	if len(s.fires) != 0 || len(s.recovers) != 0 {
		t.Fatalf("VM error must not send emails, fires=%d recovers=%d", len(s.fires), len(s.recovers))
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

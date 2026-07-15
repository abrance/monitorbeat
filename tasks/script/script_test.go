// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package script

import (
	"context"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

func TestGather_Run_Echo(t *testing.T) {
	cfg := &configs.ScriptConfig{
		BaseTaskParam: configs.BaseTaskParam{TaskID: 100, Enabled: true, Timeout: 5 * time.Second},
		Command:       "echo 'demo_total 42'",
		Format:        "prometheus",
	}
	_ = cfg.Clean()

	g := New(cfg).(*Gather)

	ch := make(chan define.Event, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	g.Run(ctx, ch)

	select {
	case ev := <-ch:
		if ev.GetType() != ScriptEventType {
			t.Fatalf("event type = %q", ev.GetType())
		}
		data := ev.GetData().(map[string]any)
		metrics := data["metrics"].(map[string]float64)
		if metrics["demo_total"] != 42 {
			t.Fatalf("demo_total = %v, want 42", metrics["demo_total"])
		}
		if metrics["cost_ms"] < 0 {
			t.Fatalf("cost_ms should be >= 0, got %v", metrics["cost_ms"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event received")
	}
}

func TestGather_FailScript(t *testing.T) {
	cfg := &configs.ScriptConfig{
		BaseTaskParam: configs.BaseTaskParam{TaskID: 101, Enabled: true, Timeout: 5 * time.Second},
		Command:       "exit 1",
		Format:        "prometheus",
	}
	_ = cfg.Clean()

	g := New(cfg).(*Gather)
	ch := make(chan define.Event, 4)
	ctx := context.Background()
	g.Run(ctx, ch)

	select {
	case ev := <-ch:
		data := ev.GetData().(map[string]any)
		if ev.GetType() != ScriptEventType {
			t.Fatalf("event type = %q", ev.GetType())
		}
		if data["error"] == nil || data["error"] == "" {
			t.Fatal("expected error field on script failure")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event received")
	}
}

func TestGather_InvalidConfig(t *testing.T) {
	cfg := &configs.ScriptConfig{}
	if _, err := builder(cfg); err == nil {
		t.Fatal("expected error for empty command")
	}
}

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package dmesg

import (
	"context"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

func TestRun_ProducesEvent(t *testing.T) {
	cfg := &configs.DmesgConfig{
		BaseTaskParam: configs.BaseTaskParam{
			TaskID:  9001,
			Enabled: true,
			Period:  60 * time.Second,
			Timeout: 10 * time.Second,
		},
	}
	if err := cfg.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}

	g := New(cfg).(*Gather)
	ch := make(chan define.Event, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	g.Run(ctx, ch)

	select {
	case ev := <-ch:
		if ev.GetType() != EventType {
			t.Fatalf("event type = %q, want %q", ev.GetType(), EventType)
		}
		data := ev.GetData().(map[string]any)
		exceptions, ok := data["exceptions"].([]matchResult)
		if !ok {
			t.Fatal("exceptions field missing or wrong type")
		}
		metrics, ok := data["metrics"].(map[string]float64)
		if !ok {
			t.Fatal("metrics field missing or wrong type")
		}
		if total, ok := metrics["total"]; !ok || int(total) != len(exceptions) {
			t.Errorf("total = %v, want %d", total, len(exceptions))
		}
		t.Logf("found %d dmesg exceptions", len(exceptions))
		for _, m := range exceptions {
			t.Logf("  %s: %s", m.Name, m.Message)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("no event received")
	}
}

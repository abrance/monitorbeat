// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package selfstats

import (
	"context"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

func TestRun_ProducesEvent(t *testing.T) {
	cfg := &configs.SelfStatsConfig{
		BaseTaskParam: configs.BaseTaskParam{
			TaskID:  8001,
			Enabled: true,
			Period:  60 * time.Second,
		},
	}
	if err := cfg.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}

	g := New(cfg).(*Gather)
	ch := make(chan define.Event, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	g.Run(ctx, ch)

	select {
	case ev := <-ch:
		if ev.GetType() != EventType {
			t.Fatalf("event type = %q, want %q", ev.GetType(), EventType)
		}
		data := ev.GetData().(map[string]any)
		metrics, ok := data["metrics"].(map[string]float64)
		if !ok {
			t.Fatal("metrics field missing")
		}
		for _, k := range []string{"num_goroutine", "num_cpu", "heap_alloc_mb", "num_gc"} {
			if _, ok := metrics[k]; !ok {
				t.Errorf("missing metric: %s", k)
			}
		}
		if _, ok := data["go_version"]; !ok {
			t.Error("missing field: go_version")
		}
		t.Logf("goroutines=%v, heap_alloc=%.2fMB, num_gc=%v",
			metrics["num_goroutine"], metrics["heap_alloc_mb"], metrics["num_gc"])
	case <-time.After(3 * time.Second):
		t.Fatal("no event received")
	}
}

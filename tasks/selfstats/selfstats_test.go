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
		for _, k := range []string{"num_goroutine", "num_cpu", "heap_alloc_mb", "num_gc", "go_version"} {
			if _, ok := data[k]; !ok {
				t.Errorf("missing field: %s", k)
			}
		}
		t.Logf("goroutines=%v, heap_alloc=%.2fMB, num_gc=%v",
			data["num_goroutine"], data["heap_alloc_mb"], data["num_gc"])
	case <-time.After(3 * time.Second):
		t.Fatal("no event received")
	}
}

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package gatherupbeat

import (
	"context"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

func TestRun_ProducesEvent(t *testing.T) {
	cfg := &configs.GatherUpBeatConfig{
		BaseTaskParam: configs.BaseTaskParam{
			TaskID:  8002,
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
		if _, ok := data["uptime_sec"]; !ok {
			t.Error("uptime_sec missing")
		}
		if v, ok := data["task_id"]; !ok || v.(int32) != 8002 {
			t.Errorf("task_id = %v, want 8002", v)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event received")
	}
}

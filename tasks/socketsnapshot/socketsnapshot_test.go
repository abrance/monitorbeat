// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package socketsnapshot

import (
	"context"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/tasks"
)

func TestBuilder_InvalidConfig(t *testing.T) {
	_, err := tasks.Build(&configs.BasereportConfig{})
	if err == nil {
		t.Fatal("expected error for wrong config type")
	}
}

func TestRun_ProducesEvent(t *testing.T) {
	cfg := &configs.SocketSnapshotConfig{
		BaseTaskParam: configs.BaseTaskParam{
			TaskID:  7001,
			Enabled: true,
			Period:  60 * time.Second,
		},
	}
	if err := cfg.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}

	g := New(cfg).(*Gather)
	ch := make(chan define.Event, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	g.Run(ctx, ch)

	select {
	case ev := <-ch:
		if ev.GetType() != EventType {
			t.Fatalf("event type = %q, want %q", ev.GetType(), EventType)
		}
		data := ev.GetData().(map[string]any)
		conns, ok := data["connections"].([]connInfo)
		if !ok {
			t.Fatal("connections field missing or wrong type")
		}
		metrics, ok := data["metrics"].(map[string]float64)
		if !ok {
			t.Fatal("metrics field missing")
		}
		if total, ok := metrics["total"]; !ok || int(total) != len(conns) {
			t.Errorf("total = %v, want %d", total, len(conns))
		}
		if v, ok := metrics["cost_ms"]; !ok || v < 0 {
			t.Errorf("cost_ms invalid: %v", v)
		}
		t.Logf("collected %d connections", len(conns))
		if len(conns) > 0 {
			t.Logf("first: pid=%d proto=%s status=%s %s:%d -> %s:%d",
				conns[0].PID, conns[0].Protocol, conns[0].Status,
				conns[0].LocalIP, conns[0].Port,
				conns[0].RemoteIP, conns[0].RemotePort)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("no event received")
	}
}

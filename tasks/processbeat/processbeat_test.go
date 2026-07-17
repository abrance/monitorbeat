// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package processbeat

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
	cfg := &configs.ProcessbeatConfig{
		BaseTaskParam: configs.BaseTaskParam{
			TaskID:  6001,
			Enabled: true,
			Period:  30 * time.Second,
		},
		TopN: 10,
		// 不设过滤条件，采集所有进程
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
		procs, ok := data["processes"].([]procInfo)
		if !ok {
			t.Fatal("processes field missing or wrong type")
		}
		if len(procs) == 0 {
			t.Fatal("expected at least 1 process")
		}
		metrics, ok := data["metrics"].(map[string]float64)
		if !ok {
			t.Fatal("metrics field missing")
		}
		if total, ok := metrics["total"]; !ok || int(total) != len(procs) {
			t.Errorf("total = %v, want %d", total, len(procs))
		}
		if v, ok := metrics["cost_ms"]; !ok || v < 0 {
			t.Errorf("cost_ms invalid: %v", v)
		}
		t.Logf("collected %d processes, top entry: pid=%d name=%s cpu=%.2f",
			len(procs), procs[0].PID, procs[0].Name, procs[0].CPUPercent)
	case <-time.After(8 * time.Second):
		t.Fatal("no event received")
	}
}

func TestRun_NameFilter(t *testing.T) {
	cfg := &configs.ProcessbeatConfig{
		BaseTaskParam: configs.BaseTaskParam{
			TaskID:  6002,
			Enabled: true,
			Period:  30 * time.Second,
		},
		ProcessNames: []string{"monitorbeat"}, // 过滤只匹配我们自己
		TopN:         5,
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
		data := ev.GetData().(map[string]any)
		procs := data["processes"].([]procInfo)
		// 应该至少能找到 monitorbeat 测试进程自身
		t.Logf("matched %d processes with name 'monitorbeat'", len(procs))
		for _, p := range procs {
			if p.Name != "monitorbeat" {
				t.Errorf("unexpected process name: %s", p.Name)
			}
		}
	case <-time.After(8 * time.Second):
		t.Fatal("no event received")
	}
}

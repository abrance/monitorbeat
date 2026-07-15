// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/define"
)

// mockOutput 捕获 Publish 调用，用于测试。
type mockOutput struct {
	mu     sync.Mutex
	events []define.Event
}

func (m *mockOutput) Name() string                { return "mock" }
func (m *mockOutput) Init(_ map[string]any) error { return nil }
func (m *mockOutput) Close() error                { return nil }
func (m *mockOutput) Publish(_ context.Context, ev define.Event) error {
	m.mu.Lock()
	m.events = append(m.events, ev)
	m.mu.Unlock()
	return nil
}

func (m *mockOutput) heartbeatEvents() []define.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []define.Event
	for _, ev := range m.events {
		if ev.GetType() == "heartbeat_event" {
			out = append(out, ev)
		}
	}
	return out
}

func TestHeartbeat(t *testing.T) {
	out := &mockOutput{}

	eng := New(8, "0.1.0-test", 50*time.Millisecond)
	eng.AddOutput(out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx) }()

	// 等 3 个 tick 周期，应有 ≥ 2 条心跳
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	heartbeats := out.heartbeatEvents()
	if len(heartbeats) < 2 {
		t.Fatalf("expected >= 2 heartbeats, got %d", len(heartbeats))
	}

	// 检查心跳数据字段
	last := heartbeats[len(heartbeats)-1]
	data, ok := last.GetData().(map[string]any)
	if !ok {
		t.Fatal("heartbeat data is not map[string]any")
	}
	if v, ok := data["version"]; !ok || v != "0.1.0-test" {
		t.Errorf("version = %v", data["version"])
	}
	if v, ok := data["uptime_sec"]; !ok {
		t.Error("uptime_sec missing")
	} else if v.(float64) <= 0 {
		t.Errorf("uptime_sec = %v, want > 0", v)
	}
	if v, ok := data["go_version"]; !ok || v == "" {
		t.Error("go_version missing or empty")
	}
}

func TestHeartbeatDisabled(t *testing.T) {
	out := &mockOutput{}

	eng := New(8, "0.1.0-test", 0) // interval=0 禁用心跳
	eng.AddOutput(out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	heartbeats := out.heartbeatEvents()
	if len(heartbeats) != 0 {
		t.Fatalf("expected 0 heartbeats, got %d", len(heartbeats))
	}
}

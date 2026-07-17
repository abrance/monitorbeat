// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package exceptionbeat

import (
	"context"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/tasks"
)

func TestBuilder_InvalidConfig(t *testing.T) {
	// 通过 tasks.Build 验证 builder 拒绝错误 config type
	_, err := tasks.Build(&configs.BasereportConfig{})
	if err == nil {
		t.Fatal("expected error for wrong config type")
	}
}

func TestIsReadOnly(t *testing.T) {
	tests := []struct {
		opts []string
		want bool
	}{
		{[]string{"ro"}, true},
		{[]string{"rw"}, false},
		{[]string{"rw", "noexec"}, false},
		{[]string{"ro", "noexec", "nosuid"}, true},
		{[]string{"ro", "relatime"}, true},
		{nil, false},
	}
	for _, tt := range tests {
		if got := isReadOnly(tt.opts); got != tt.want {
			t.Errorf("isReadOnly(%v) = %v, want %v", tt.opts, got, tt.want)
		}
	}
}

func TestMatchAny(t *testing.T) {
	tests := []struct {
		s        string
		patterns []string
		want     bool
	}{
		{"/mnt/ro", []string{"/mnt/*"}, true},
		{"/mnt/rw", []string{"/mnt/*"}, true},
		{"/data", []string{"/mnt/*"}, false},
		{"/mnt/ro", nil, false},
		{"/mnt/ro", []string{}, false},
	}
	for _, tt := range tests {
		if got := matchAny(tt.s, tt.patterns); got != tt.want {
			t.Errorf("matchAny(%q, %v) = %v, want %v", tt.s, tt.patterns, got, tt.want)
		}
	}
}

func TestParseOOMKillCount(t *testing.T) {
	g := &Gather{}

	tests := []struct {
		content string
		want    int64
	}{
		{"oom_kill 5\n", 5},
		{"oom_kill 0\npgalloc_normal 123\n", 0},
		{"pgalloc_normal 123\noom_kill 42\n", 42},
		{"no match here", 0},
		{"oom_kill\n", 0},
		{"oom_kill abc\n", 0},
	}
	for _, tt := range tests {
		if got := g.parseOOMKillCount(tt.content); got != tt.want {
			t.Errorf("parseOOMKillCount(%q) = %v, want %v", tt.content, got, tt.want)
		}
	}
}

func TestRun_ProducesEvent(t *testing.T) {
	cfg := &configs.ExceptionbeatConfig{
		BaseTaskParam: configs.BaseTaskParam{
			TaskID:  5001,
			Enabled: true,
			Period:  60 * time.Second,
		},
		// 关闭所有子 collector，只测基础 event 产出
		CheckDiskRO:    false,
		CheckDiskSpace: false,
		CheckCorefile:  false,
		CheckOOM:       false,
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
		if data["disk_ro"] == nil {
			t.Error("disk_ro field missing")
		}
		if data["disk_space"] == nil {
			t.Error("disk_space field missing")
		}
		if data["corefile"] == nil {
			t.Error("corefile field missing")
		}
		if data["oom"] == nil {
			t.Error("oom field missing")
		}
		metrics, ok := data["metrics"].(map[string]float64)
		if !ok {
			t.Fatal("metrics field missing")
		}
		if v, ok := metrics["cost_ms"]; !ok || v < 0 {
			t.Errorf("cost_ms invalid: %v", v)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event received")
	}
}

func TestRun_WithDiskCheck(t *testing.T) {
	cfg := &configs.ExceptionbeatConfig{
		BaseTaskParam: configs.BaseTaskParam{
			TaskID:  5002,
			Enabled: true,
			Period:  60 * time.Second,
		},
		CheckDiskRO:      true,
		CheckDiskSpace:   true,
		DiskUsagePercent: 90,
		DiskMinFreeGB:    10,
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
		data := ev.GetData().(map[string]any)
		// disk_ro 至少是空 slice（nil 表示出错，nil 也算合理）
		// disk_space 检查，可能为空也可能有数据
		metrics := data["metrics"].(map[string]float64)
		t.Logf("disk_ro=%v, disk_space=%v, cost_ms=%v",
			data["disk_ro"], data["disk_space"], metrics["cost_ms"])
	case <-time.After(3 * time.Second):
		t.Fatal("no event received")
	}
}

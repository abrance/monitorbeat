// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import (
	"testing"
	"time"

	"github.com/abrance/monitorbeat/define"
)

func TestConfig_GetCheckInterval(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"zero falls back to default", 0, define.DefaultCheckInterval},
		{"explicit value kept", 7 * time.Second, 7 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{CheckInterval: tc.in}
			if got := c.GetCheckInterval(); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConfig_GetTaskConfigListByType(t *testing.T) {
	c := &Config{
		Basereports: []BasereportConfig{
			{BaseTaskParam: BaseTaskParam{TaskID: 1}},
			{BaseTaskParam: BaseTaskParam{TaskID: 2}},
		},
	}

	got := c.GetTaskConfigListByType(define.ModuleBasereport)
	if len(got) != 2 {
		t.Fatalf("basereport list len = %d, want 2", len(got))
	}

	// unknown type returns nil slice (no panic).
	if other := c.GetTaskConfigListByType("nonexistent"); other != nil {
		t.Fatalf("unknown type should return nil, got %v", other)
	}
}

func TestConfig_Clean_FillsDefaults(t *testing.T) {
	c := &Config{
		Basereports: []BasereportConfig{
			{BaseTaskParam: BaseTaskParam{Enabled: true, Period: 30 * time.Second}},
		},
	}
	if err := c.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	tc := c.Basereports[0]
	if tc.GetTaskID() == 0 {
		t.Fatal("task_id should default to non-zero")
	}
	if tc.GetIdent() == "" {
		t.Fatal("ident should default to <type>:<task_id>")
	}
	if got, want := tc.GetIdent(), "basereport:1"; got != want {
		t.Fatalf("ident = %q, want %q", got, want)
	}
	if tc.GetType() != define.ModuleBasereport {
		t.Fatalf("type = %q, want %q", tc.GetType(), define.ModuleBasereport)
	}
}

func TestConfig_GetEventBufferSize(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 1024},
		{2048, 2048},
	}
	for _, tc := range cases {
		c := &Config{EventBufferSize: tc.in}
		if got := c.GetEventBufferSize(); got != tc.want {
			t.Fatalf("got %d, want %d", got, tc.want)
		}
	}
}

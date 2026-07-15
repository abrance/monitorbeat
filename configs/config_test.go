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

func TestConfig_ProbeTaskGrouping(t *testing.T) {
	c := &Config{
		Pings: []PingConfig{{BaseTaskParam: BaseTaskParam{TaskID: 1}}},
		TCPs:  []TCPConfig{{BaseTaskParam: BaseTaskParam{TaskID: 2}}},
		UDPs:  []UDPConfig{{BaseTaskParam: BaseTaskParam{TaskID: 3}}},
		HTTPs: []HTTPConfig{{BaseTaskParam: BaseTaskParam{TaskID: 4}}},
	}

	checks := map[string]int{
		define.ModulePing: 1,
		define.ModuleTCP:  1,
		define.ModuleUDP:  1,
		define.ModuleHTTP: 1,
	}
	for typ, want := range checks {
		got := c.GetTaskConfigListByType(typ)
		if len(got) != want {
			t.Fatalf("%s list len = %d, want %d", typ, len(got), want)
		}
	}
	if all := c.AllTaskConfigs(); len(all) != 4 {
		t.Fatalf("all task configs len = %d, want 4", len(all))
	}
}

func TestProbeConfigs_CleanDefaults(t *testing.T) {
	c := &Config{
		Pings: []PingConfig{{BaseTaskParam: BaseTaskParam{Enabled: true}, Target: "127.0.0.1"}},
		TCPs:  []TCPConfig{{BaseTaskParam: BaseTaskParam{Enabled: true}, Address: "127.0.0.1:22"}},
		UDPs:  []UDPConfig{{BaseTaskParam: BaseTaskParam{Enabled: true}, Address: "127.0.0.1:9999"}},
		HTTPs: []HTTPConfig{{BaseTaskParam: BaseTaskParam{Enabled: true}, URL: "https://example.com"}},
	}

	if err := c.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}

	ping := c.Pings[0]
	if ping.GetIdent() != "ping:1" || ping.GetType() != define.ModulePing {
		t.Fatalf("unexpected ping defaults: ident=%q type=%q", ping.GetIdent(), ping.GetType())
	}
	if ping.Count != 2 || ping.PayloadSize != 56 || ping.MaxRTT != time.Second || ping.SendInterval != 500*time.Microsecond || ping.Backend != "icmp" || ping.Privileged {
		t.Fatalf("unexpected ping fields: %+v", ping)
	}

	if got := c.TCPs[0].GetIdent(); got != "tcp:1" {
		t.Fatalf("tcp ident = %q, want tcp:1", got)
	}
	if got := c.UDPs[0].GetIdent(); got != "udp:1" {
		t.Fatalf("udp ident = %q, want udp:1", got)
	}
	if got := c.HTTPs[0].GetIdent(); got != "http:1" {
		t.Fatalf("http ident = %q, want http:1", got)
	}
}

func TestKeywordConfig_CleanDefaults(t *testing.T) {
	c := &Config{
		Keywords: []KeywordConfig{
			{
				BaseTaskParam: BaseTaskParam{Enabled: true},
				File:          "/tmp/demo.log",
				Pattern:       `ERROR.*payment_id=(\d+)`,
			},
		},
	}
	if err := c.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	k := c.Keywords[0]
	if k.GetIdent() != "keyword:1" {
		t.Fatalf("ident = %q, want keyword:1", k.GetIdent())
	}
	if k.GetType() != define.ModuleKeyword {
		t.Fatalf("type = %q, want %q", k.GetType(), define.ModuleKeyword)
	}
	if k.Encoding == "" {
		t.Fatal("encoding should default")
	}
	if k.BufferSize <= 0 {
		t.Fatalf("buffer_size should default positive, got %d", k.BufferSize)
	}
	if k.FromBegin == nil || !*k.FromBegin {
		t.Fatalf("from_begin should default to true, got %v", k.FromBegin)
	}
}

func TestConfig_KeywordGrouping(t *testing.T) {
	c := &Config{
		Keywords: []KeywordConfig{
			{BaseTaskParam: BaseTaskParam{TaskID: 11}},
			{BaseTaskParam: BaseTaskParam{TaskID: 12}},
		},
	}
	if got := c.GetTaskConfigListByType(define.ModuleKeyword); len(got) != 2 {
		t.Fatalf("keyword list len = %d, want 2", len(got))
	}
	if got := c.AllTaskConfigs(); len(got) != 2 {
		t.Fatalf("all task configs len = %d, want 2", len(got))
	}
}

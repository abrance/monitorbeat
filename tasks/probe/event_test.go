// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package probe

import (
	"testing"
	"time"

	"github.com/abrance/monitorbeat/define"
)

func TestEventDataBuildsNormalizedShape(t *testing.T) {
	result := Result{
		Success:  true,
		Duration: 1500 * time.Microsecond,
		Metrics: map[string]float64{"connect_ms": 1.25},
	}

	ev := BuildEvent("tcp", "127.0.0.1:22", 42, result)

	if ev.GetType() != "tcp_event" {
		t.Fatalf("event type = %q, want tcp_event", ev.GetType())
	}
	data, ok := ev.GetData().(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", ev.GetData())
	}
	dims, ok := data["dimensions"].(map[string]string)
	if !ok {
		t.Fatalf("dimensions type = %T, want map[string]string", data["dimensions"])
	}
	if dims["probe_type"] != "tcp" || dims["target"] != "127.0.0.1:22" || dims["task_id"] != "42" {
		t.Fatalf("unexpected dimensions: %+v", dims)
	}
	metrics, ok := data["metrics"].(map[string]float64)
	if !ok {
		t.Fatalf("metrics type = %T, want map[string]float64", data["metrics"])
	}
	if metrics["success"] != 1 || metrics["duration_ms"] != 1.5 || metrics["connect_ms"] != 1.25 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
	if data["error"] != "" {
		t.Fatalf("error = %v, want empty", data["error"])
	}
}

func TestResultFailureMetrics(t *testing.T) {
	result := Result{
		Success:  false,
		Duration: time.Millisecond,
		Error:    "connect: timeout",
	}

	ev := BuildEvent("tcp", "192.0.2.1:65530", 7, result)
	data := ev.GetData().(map[string]any)
	metrics := data["metrics"].(map[string]float64)

	if metrics["success"] != 0 {
		t.Fatalf("success = %v, want 0", metrics["success"])
	}
	if data["error"] != "connect: timeout" {
		t.Fatalf("error = %v, want connect: timeout", data["error"])
	}
}

func TestDurationMillis(t *testing.T) {
	if got := DurationMillis(1500 * time.Microsecond); got != 1.5 {
		t.Fatalf("DurationMillis = %v, want 1.5", got)
	}
}

var _ define.Event = BuildEvent("tcp", "localhost:80", 1, Result{})

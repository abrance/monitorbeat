// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package parse

import (
	"testing"
)

func TestParsePrometheus(t *testing.T) {
	out := `# HELP demo_total demo counter
# TYPE demo_total counter
demo_total{env="prod"} 42.0
demo_uptime_seconds 99.5
`
	metrics, labels, err := Parse("prometheus", out)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := metrics["demo_total"]; !ok || v != 42.0 {
		t.Fatalf("demo_total = %v, want 42.0", v)
	}
	if v, ok := metrics["demo_uptime_seconds"]; !ok || v != 99.5 {
		t.Fatalf("uptime = %v, want 99.5", v)
	}
	if v, ok := labels["env"]; !ok || v != "prod" {
		t.Fatalf("env label = %q, want prod", v)
	}
}

func TestParseCustom(t *testing.T) {
	out := `duration_ms=1500
status=1
count=3
`
	metrics, _, err := Parse("custom", out)
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 3 {
		t.Fatalf("got %d metrics, want 3", len(metrics))
	}
	if metrics["count"] != 3 {
		t.Fatalf("count = %v, want 3", metrics["count"])
	}
}

func TestParse_EmptyOutput(t *testing.T) {
	metrics, _, err := Parse("custom", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 0 {
		t.Fatalf("expected empty metrics, got %d", len(metrics))
	}
}

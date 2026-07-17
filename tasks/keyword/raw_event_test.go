package keyword

import "testing"

func TestBuildRawLogEvent_Shape(t *testing.T) {
	ev := BuildRawLogEvent(
		"/tmp/demo.log",
		`ERROR payment_id=(\d+)`,
		7,
		map[string]string{"1": "12345"},
		"ERROR payment_id=12345 amount=99.9",
	)
	if ev.GetType() != RawLogEventType {
		t.Fatalf("type = %q, want %q", ev.GetType(), RawLogEventType)
	}
	data, ok := ev.GetData().(map[string]any)
	if !ok {
		t.Fatalf("data not map[string]any: %T", ev.GetData())
	}
	dims, ok := data["dimensions"].(map[string]string)
	if !ok {
		t.Fatalf("dimensions not map[string]string: %T", data["dimensions"])
	}
	if dims["file"] != "/tmp/demo.log" || dims["regex"] != `ERROR payment_id=(\d+)` {
		t.Fatalf("unexpected dimensions: %+v", dims)
	}
	if dims["hostname"] == "" {
		t.Error("hostname dimension missing")
	}
	metrics, ok := data["metrics"].(map[string]float64)
	if !ok {
		t.Fatalf("metrics not map[string]float64: %T", data["metrics"])
	}
	if metrics["matches_count"] != 1 {
		t.Fatalf("matches_count = %v, want 1", metrics["matches_count"])
	}
	fields, ok := data["fields"].(map[string]string)
	if !ok || fields["1"] != "12345" {
		t.Fatalf("unexpected fields: %+v", fields)
	}
	if data["raw"] != "ERROR payment_id=12345 amount=99.9" {
		t.Fatalf("raw line mismatch: %q", data["raw"])
	}
}

func TestBuildRawLogEvent_NilCaptures(t *testing.T) {
	ev := BuildRawLogEvent("f", `^INFO$`, 1, nil, "INFO")
	data := ev.GetData().(map[string]any)
	fields := data["fields"].(map[string]string)
	// fields should contain line_number even with nil captures
	if fields["line_number"] != "1" {
		t.Fatalf("line_number = %q, want 1", fields["line_number"])
	}
	if len(fields) != 1 {
		t.Fatalf("expected only line_number, got %+v", fields)
	}
}

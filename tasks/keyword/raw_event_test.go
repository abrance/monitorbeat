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
	if dims["file"] != "/tmp/demo.log" || dims["regex"] != `ERROR payment_id=(\d+)` || dims["line_number"] != "7" {
		t.Fatalf("unexpected dimensions: %+v", dims)
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
	fields := data["fields"]
	if fields == nil {
		return
	}
	if m, ok := fields.(map[string]string); ok && len(m) == 0 {
		return
	}
	t.Fatalf("nil captures should pass through, got %+v (%T)", fields, fields)
}

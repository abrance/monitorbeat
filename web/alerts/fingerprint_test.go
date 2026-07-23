// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License

package alerts

import "testing"

func TestFingerprintStableUnderMapReordering(t *testing.T) {
	a := map[string]string{
		"hostname":   "node-a",
		"probe_type": "http",
		"target":     "https://example.com",
		"task_id":    "1002",
	}
	b := map[string]string{
		"task_id":    "1002",
		"target":     "https://example.com",
		"probe_type": "http",
		"hostname":   "node-a",
	}
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatalf("fingerprint must be stable under map iteration order; got %s vs %s",
			Fingerprint(a), Fingerprint(b))
	}
}

func TestFingerprintDiffersWhenLabelChanges(t *testing.T) {
	base := map[string]string{"hostname": "node-a", "target": "https://a"}
	other := map[string]string{"hostname": "node-a", "target": "https://b"}
	if Fingerprint(base) == Fingerprint(other) {
		t.Fatalf("fingerprint must change when labels change")
	}
}

func TestFingerprintEmpty(t *testing.T) {
	if Fingerprint(nil) == "" {
		t.Fatal("fingerprint should not be empty for nil labels")
	}
	if Fingerprint(map[string]string{}) == "" {
		t.Fatal("fingerprint should not be empty for empty labels")
	}
}

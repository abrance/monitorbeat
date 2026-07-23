// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License

package alerts

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// Fingerprint returns a stable SHA-256 hex digest of a label set. The
// same labels in any map iteration order produce the same fingerprint.
//
// We sort labels by key and join them as `key=value\n` so the result is
// fully deterministic across Go's randomized map iteration. SHA-256 is
// overkill for uniqueness but keeps the fingerprint short, fixed-length
// and independent of label count.
func Fingerprint(labels map[string]string) string {
	if len(labels) == 0 {
		return sha256Hex("{}")
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf := make([]byte, 0, len(keys)*16)
	for _, k := range keys {
		buf = append(buf, k...)
		buf = append(buf, '=')
		buf = append(buf, labels[k]...)
		buf = append(buf, '\n')
	}
	return sha256Hex(string(buf))
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

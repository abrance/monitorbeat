// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License

package alerts

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// Time is a thin wrapper over time.Time that:
//   - Marshals to/from RFC3339 strings in JSON (so the wire format is the
//     same as today).
//   - Scans and Values as RFC3339 strings for SQLite storage (the same
//     convention the previous code used in web/alerts/store.go).
//
// We can't reuse time.Time directly because we'd need to keep custom JSON
// tags and database/sql interfaces side-by-side, and the previous store
// already marshals time as RFC3339 strings via helpers — staying with
// strings keeps the storage format stable across this rewrite.
type Time struct {
	time.Time
}

// NewTime returns a Time wrapping t.
func NewTime(t time.Time) Time {
	return Time{Time: t}
}

// MarshalJSON encodes the time as RFC3339.
func (t Time) MarshalJSON() ([]byte, error) {
	if t.Time.IsZero() {
		return []byte(`null`), nil
	}
	return []byte(`"` + t.Time.UTC().Format(time.RFC3339) + `"`), nil
}

// UnmarshalJSON parses an RFC3339 string. Empty strings decode to zero time.
func (t *Time) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" || s == `""` {
		t.Time = time.Time{}
		return nil
	}
	// JSON strings come wrapped in quotes; trim them.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("alerts: parse time %q: %w", s, err)
	}
	t.Time = parsed
	return nil
}

// Scan implements sql.Scanner. Accepts RFC3339 strings or time.Time.
func (t *Time) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		t.Time = time.Time{}
		return nil
	case string:
		if v == "" {
			t.Time = time.Time{}
			return nil
		}
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return err
		}
		t.Time = parsed
		return nil
	case time.Time:
		t.Time = v
		return nil
	default:
		return fmt.Errorf("alerts: cannot scan %T into Time", src)
	}
}

// Value implements driver.Valuer. Returns RFC3339 string or nil.
func (t Time) Value() (driver.Value, error) {
	if t.Time.IsZero() {
		return nil, nil
	}
	return t.Time.UTC().Format(time.RFC3339), nil
}

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package probe

import "time"

type Result struct {
	Metrics  map[string]float64
	Error    string
	Duration time.Duration
	Success  bool
}

func DurationMillis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

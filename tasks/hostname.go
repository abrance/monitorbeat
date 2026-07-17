// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package tasks

import "os"

// Hostname returns the monitorbeat-reported hostname.
// Prefers MONITORBEAT_HOSTNAME env var; falls back to os.Hostname().
func Hostname() string {
	if h := os.Getenv("MONITORBEAT_HOSTNAME"); h != "" {
		return h
	}
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

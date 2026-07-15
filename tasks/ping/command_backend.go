// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package ping

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/tasks/probe"
)

// runCommandBackend drives the Linux system `ping` binary, capturing its
// stdout and feeding it through parseCommandOutput. It is the Linux-only
// fallback used when raw ICMP sockets are unavailable (CI, shared
// containers, locked-down production hosts).
//
// The command is invoked with explicit bounds taken from the config so
// a misconfigured Count / PayloadSize / MaxRTT cannot wedge the runner:
// the surrounding context timeout still acts as a hard ceiling.
func runCommandBackend(ctx context.Context, start time.Time, cfg *configs.PingConfig) probe.Result {
	count := cfg.Count
	if count <= 0 {
		count = 2
	}
	payloadSize := cfg.PayloadSize
	if payloadSize < 8 {
		payloadSize = 56
	}
	// `ping -W` takes a per-packet deadline in seconds. Round up so
	// MaxRTT < 1s still gives at least one second.
	perPacketSeconds := int(cfg.MaxRTT.Seconds())
	if cfg.MaxRTT > 0 && cfg.MaxRTT < time.Second {
		perPacketSeconds = 1
	}
	if perPacketSeconds < 1 {
		perPacketSeconds = 1
	}

	args := []string{
		"-c", strconv.Itoa(count),
		"-s", strconv.Itoa(payloadSize),
		"-W", strconv.Itoa(perPacketSeconds),
		"-4",
		cfg.Target,
	}

	cmd := exec.CommandContext(ctx, "ping", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return probe.Result{
			Duration: duration,
			Error:    fmt.Sprintf("command ping aborted: %v", ctxErr),
			Metrics:  map[string]float64{"backend": 0}, // 0 = command
		}
	}
	if err != nil {
		// Surface stderr because exit-code-only errors (e.g. "unknown
		// host") are otherwise opaque.
		msg := err.Error()
		if stderr.Len() > 0 {
			msg = fmt.Sprintf("%s: %s", msg, stderr.String())
		}
		return probe.Result{
			Duration: duration,
			Error:    msg,
			Metrics:  map[string]float64{"backend": 0},
		}
	}

	metrics, parseErr := parseCommandOutput(stdout.String())
	if parseErr != nil {
		return probe.Result{
			Duration: duration,
			Error:    parseErr.Error(),
			Metrics:  map[string]float64{"backend": 0},
		}
	}
	if metrics == nil {
		metrics = map[string]float64{}
	}
	metrics["backend"] = 0 // 0 = command
	return probe.Result{Success: true, Duration: duration, Metrics: metrics}
}

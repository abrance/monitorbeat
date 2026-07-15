// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package ping

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// errBackendUnsupported is returned by parseCommandOutput when the supplied
// text does not match the Linux `iputils ping` layout. The ICMP backend in
// ping.go also surfaces this error to the runner when the requested
// backend is neither "icmp" nor "command".
var errBackendUnsupported = errors.New("ping: command output backend is linux-only")

// linuxPingHeader marks a Linux-shaped `ping` preamble. Both iputils and
// busybox (Alpine) ping begin with "PING <host> (<ip>)"; iputils follows
// with "<n>(<m>) bytes of data." while busybox uses ": <n> data bytes".
// Matching only the stable prefix keeps the parser backend-agnostic.
var linuxPingHeader = regexp.MustCompile(
	`(?m)^PING\s+\S+\s+\(\S+\)`,
)

// linuxStatsLine matches the trailing summary emitted by both iputils
// ("3 packets transmitted, 3 received, 0% packet loss") and busybox
// ("3 packets transmitted, 3 packets received, 0% packet loss"). The
// optional "packets" before "received" covers the busybox wording.
var linuxStatsLine = regexp.MustCompile(
	`(?m)^(\d+)\s+packets transmitted,\s+(\d+)\s+(?:packets\s+)?received,\s+([\d.]+)%\s+packet loss`,
)

// linuxRTTLine matches the round-trip summary from iputils
// ("rtt min/avg/max/mdev = a/b/c/d ms") and busybox
// ("round-trip min/avg/max = a/b/c ms"). Only the first three numbers
// (min/avg/max) are captured; mdev, when present, is ignored.
var linuxRTTLine = regexp.MustCompile(
	`(?m)^(?:rtt|round-trip)\s+min/avg/max(?:/mdev)?\s*=\s*([\d.]+)/([\d.]+)/([\d.]+)`,
)

// parseCommandOutput converts the stdout of a Linux `ping -c <N> -w <T>`
// invocation into the canonical ping metric map. The parser accepts both
// iputils and busybox (Alpine) output layouts; inputs missing the Linux
// preamble or stats block produce errBackendUnsupported so the caller can
// fall back to the ICMP backend instead of emitting misleading zeros.
func parseCommandOutput(output string) (map[string]float64, error) {
	if !linuxPingHeader.MatchString(output) {
		return nil, errBackendUnsupported
	}

	metrics := make(map[string]float64, 7)

	if m := linuxStatsLine.FindStringSubmatch(output); m != nil {
		sent, err1 := strconv.Atoi(m[1])
		received, err2 := strconv.Atoi(m[2])
		lossPct, err3 := strconv.ParseFloat(m[3], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("ping: malformed stats line: %q", m[0])
		}
		if sent <= 0 {
			return nil, fmt.Errorf("ping: stats line reports zero packets transmitted")
		}
		metrics["packets_sent"] = float64(sent)
		metrics["packets_received"] = float64(received)
		// `ping` reports loss as a percentage (0-100); normalise to the
		// 0.0-1.0 fraction the rest of the probe pipeline expects.
		metrics["packet_loss_percent"] = lossPct / 100.0
		metrics["available"] = float64(received) / float64(sent)
	} else {
		return nil, fmt.Errorf("ping: linux stats summary line missing: %s", errBackendUnsupported)
	}

	if m := linuxRTTLine.FindStringSubmatch(output); m != nil {
		minRTT, err1 := strconv.ParseFloat(m[1], 64)
		avgRTT, err2 := strconv.ParseFloat(m[2], 64)
		maxRTT, err3 := strconv.ParseFloat(m[3], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("ping: malformed rtt line: %q", m[0])
		}
		metrics["min_rtt_ms"] = minRTT
		metrics["avg_rtt_ms"] = avgRTT
		metrics["max_rtt_ms"] = maxRTT
	} else {
		// Linux always emits the rtt line when at least one reply arrived.
		// If it's missing but received > 0 the output is corrupt; otherwise
		// fall back to zeros so the loss ratio remains observable.
		if metrics["packets_received"] > 0 {
			return nil, fmt.Errorf("ping: linux rtt summary line missing despite received > 0")
		}
		metrics["min_rtt_ms"] = 0
		metrics["avg_rtt_ms"] = 0
		metrics["max_rtt_ms"] = 0
	}

	_ = strings.TrimSpace // keep strings import live for future helpers
	return metrics, nil
}

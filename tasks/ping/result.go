// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package ping

import "time"

// aggregateResults turns a slice of round-trip time samples into the
// canonical ping metric map consumed by both the ICMP and command
// backends. A negative duration marks a packet that did not come back and
// contributes to packet loss statistics while being excluded from the
// RTT summaries.
//
// The returned map always contains:
//
//   - packets_sent
//   - packets_received
//   - packet_loss_percent (0.0 - 1.0)
//   - available          (1 - packet_loss_percent)
//   - min_rtt_ms         (0 when packets_received == 0)
//   - avg_rtt_ms         (0 when packets_received == 0)
//   - max_rtt_ms         (0 when packets_received == 0)
//
// Returns nil when samples is empty so the caller can decide how to
// surface the absence of round-trip data to the probe pipeline.
func aggregateResults(samples []time.Duration) map[string]float64 {
	if len(samples) == 0 {
		return nil
	}

	metrics := make(map[string]float64, 7)
	sent := float64(len(samples))
	var (
		received int
		minRTT   time.Duration = -1
		maxRTT   time.Duration
		totalRTT time.Duration
	)

	for _, rtt := range samples {
		if rtt < 0 {
			continue
		}
		received++
		totalRTT += rtt
		if minRTT < 0 || rtt < minRTT {
			minRTT = rtt
		}
		if rtt > maxRTT {
			maxRTT = rtt
		}
	}

	metrics["packets_sent"] = sent
	metrics["packets_received"] = float64(received)
	metrics["packet_loss_percent"] = (sent - float64(received)) / sent
	metrics["available"] = float64(received) / sent

	if received == 0 {
		metrics["min_rtt_ms"] = 0
		metrics["avg_rtt_ms"] = 0
		metrics["max_rtt_ms"] = 0
		return metrics
	}

	metrics["min_rtt_ms"] = float64(minRTT) / float64(time.Millisecond)
	metrics["max_rtt_ms"] = float64(maxRTT) / float64(time.Millisecond)
	metrics["avg_rtt_ms"] = float64(totalRTT/time.Duration(received)) / float64(time.Millisecond)
	return metrics
}

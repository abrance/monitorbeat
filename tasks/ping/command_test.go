// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package ping

import "testing"

// iputils-style output (Debian/Ubuntu `ping`).
const iputilsOutput = `PING 186.241.120.132 (186.241.120.132) 56(84) bytes of data.
64 bytes from 186.241.120.132: icmp_seq=1 ttl=49 time=170.210 ms
64 bytes from 186.241.120.132: icmp_seq=2 ttl=49 time=168.506 ms
64 bytes from 186.241.120.132: icmp_seq=3 ttl=49 time=168.236 ms

--- 186.241.120.132 ping statistics ---
3 packets transmitted, 3 received, 0% packet loss, time 2041ms
rtt min/avg/max/mdev = 168.236/168.984/170.210/0.842 ms`

// busybox-style output (Alpine `ping`, used by the monitorbeat image).
const busyboxOutput = `PING 186.241.120.132 (186.241.120.132): 56 data bytes
64 bytes from 186.241.120.132: seq=0 ttl=49 time=168.846 ms
64 bytes from 186.241.120.132: seq=1 ttl=49 time=169.825 ms
64 bytes from 186.241.120.132: seq=2 ttl=49 time=169.435 ms

--- 186.241.120.132 ping statistics ---
3 packets transmitted, 3 packets received, 0% packet loss
round-trip min/avg/max = 168.236/168.984/170.210 ms`

func TestParseCommandOutput_IPUtils(t *testing.T) {
	m, err := parseCommandOutput(iputilsOutput)
	if err != nil {
		t.Fatalf("iputils parse failed: %v", err)
	}
	assertPingMetrics(t, m, 3, 3, 0)
}

func TestParseCommandOutput_Busybox(t *testing.T) {
	m, err := parseCommandOutput(busyboxOutput)
	if err != nil {
		t.Fatalf("busybox parse failed: %v", err)
	}
	assertPingMetrics(t, m, 3, 3, 0)
}

func TestParseCommandOutput_Loss(t *testing.T) {
	out := `PING 186.241.120.132 (186.241.120.132): 56 data bytes
64 bytes from 186.241.120.132: seq=0 ttl=49 time=168.846 ms

--- 186.241.120.132 ping statistics ---
3 packets transmitted, 1 packets received, 66.6667% packet loss
round-trip min/avg/max = 168.846/168.846/168.846 ms`
	m, err := parseCommandOutput(out)
	if err != nil {
		t.Fatalf("loss parse failed: %v", err)
	}
	assertPingMetrics(t, m, 3, 1, 66.6667)
}

func assertPingMetrics(t *testing.T, m map[string]float64, sent, recv int, lossPct float64) {
	t.Helper()
	if m["packets_sent"] != float64(sent) {
		t.Errorf("packets_sent = %v, want %v", m["packets_sent"], sent)
	}
	if m["packets_received"] != float64(recv) {
		t.Errorf("packets_received = %v, want %v", m["packets_received"], recv)
	}
	if diff := m["packet_loss_percent"] - lossPct/100.0; diff > 1e-6 || diff < -1e-6 {
		t.Errorf("packet_loss_percent = %v, want %v", m["packet_loss_percent"], lossPct/100.0)
	}
	if m["avg_rtt_ms"] <= 0 {
		t.Errorf("avg_rtt_ms = %v, want > 0", m["avg_rtt_ms"])
	}
}

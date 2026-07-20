// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package ping

import (
	"context"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

func TestBuildEchoMessage_preservesIDSeqAndPayloadSize(t *testing.T) {
	msg, err := buildEchoMessage(7, 3, 56)
	if err != nil {
		t.Fatalf("build echo: %v", err)
	}

	id, seq, size, err := parseEchoMessage(msg)
	if err != nil {
		t.Fatalf("parse echo: %v", err)
	}
	if id != 7 || seq != 3 || size != 56 {
		t.Fatalf("got id=%d seq=%d size=%d, want id=7 seq=3 size=56", id, seq, size)
	}
}

func TestAggregateResults_computesLossAndRTT(t *testing.T) {
	metrics := aggregateResults([]time.Duration{10 * time.Millisecond, -1, 30 * time.Millisecond})

	if metrics["packets_sent"] != 3 || metrics["packets_received"] != 2 {
		t.Fatalf("packet counts wrong: %+v", metrics)
	}
	if metrics["packet_loss_percent"] != 1.0/3.0 || metrics["available"] != 2.0/3.0 {
		t.Fatalf("loss/available wrong: %+v", metrics)
	}
	if metrics["min_rtt_ms"] != 10 || metrics["avg_rtt_ms"] != 20 || metrics["max_rtt_ms"] != 30 {
		t.Fatalf("rtt stats wrong: %+v", metrics)
	}
}

func TestParseCommandOutput_parsesLinuxPingStats(t *testing.T) {
	out := `PING 127.0.0.1 (127.0.0.1) 56(84) bytes of data.
64 bytes from 127.0.0.1: icmp_seq=1 ttl=64 time=0.025 ms
64 bytes from 127.0.0.1: icmp_seq=2 ttl=64 time=0.042 ms

--- 127.0.0.1 ping statistics ---
2 packets transmitted, 2 received, 0% packet loss, time 1002ms
rtt min/avg/max/mdev = 0.025/0.033/0.042/0.008 ms`

	metrics, err := parseCommandOutput(out)
	if err != nil {
		t.Fatalf("parse command output: %v", err)
	}
	if metrics["packets_sent"] != 2 || metrics["packets_received"] != 2 || metrics["packet_loss_percent"] != 0 {
		t.Fatalf("packet metrics wrong: %+v", metrics)
	}
	if metrics["min_rtt_ms"] != 0.025 || metrics["avg_rtt_ms"] != 0.033 || metrics["max_rtt_ms"] != 0.042 {
		t.Fatalf("rtt metrics wrong: %+v", metrics)
	}
}

func TestGather_Run_emitsEvent_whenCommandBackendRuns(t *testing.T) {
	cfg := &configs.PingConfig{BaseTaskParam: configs.BaseTaskParam{TaskID: 41, Timeout: 2 * time.Second}, Target: "127.0.0.1", Backend: "command", Count: 1, MaxRTT: time.Second, PayloadSize: 56}
	if err := cfg.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	ch := make(chan define.Event, 1)

	New(cfg).Run(context.Background(), ch)

	ev := <-ch
	if ev.GetType() != "ping_event" {
		t.Fatalf("event type = %q, want ping_event", ev.GetType())
	}
}

func TestGather_Run_emitsEvent_whenICMPBackendAvailable(t *testing.T) {
	cfg := &configs.PingConfig{BaseTaskParam: configs.BaseTaskParam{TaskID: 42, Timeout: 2 * time.Second}, Target: "127.0.0.1", Backend: "icmp", Count: 1, MaxRTT: time.Second, PayloadSize: 56}
	if err := cfg.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if !canListenICMP() {
		t.Skip("icmp socket unavailable in this environment")
	}
	ch := make(chan define.Event, 1)

	New(cfg).Run(context.Background(), ch)

	ev := <-ch
	if ev.GetType() != "ping_event" {
		t.Fatalf("event type = %q, want ping_event", ev.GetType())
	}
	data := ev.GetData().(map[string]any)
	metrics := data["metrics"].(map[string]float64)
	if metrics["success"] != 1 || metrics["packets_received"] < 1 {
		t.Fatalf("ICMP probe metrics = %+v, want successful reply", metrics)
	}
}

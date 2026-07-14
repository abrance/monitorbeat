// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package tcp

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

func TestGather_Run_emitsSuccess_whenTCPReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	cfg := &configs.TCPConfig{BaseTaskParam: configs.BaseTaskParam{TaskID: 12, Timeout: time.Second}, Address: ln.Addr().String()}
	if err := cfg.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	ch := make(chan define.Event, 1)

	New(cfg).Run(context.Background(), ch)

	ev := <-ch
	if ev.GetType() != "tcp_event" {
		t.Fatalf("event type = %q, want tcp_event", ev.GetType())
	}
	data := ev.GetData().(map[string]any)
	metrics := data["metrics"].(map[string]float64)
	if metrics["success"] != 1 || metrics["connect_ms"] < 0 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

func TestGather_Run_emitsFailure_whenTCPUnreachable(t *testing.T) {
	cfg := &configs.TCPConfig{BaseTaskParam: configs.BaseTaskParam{TaskID: 13, Timeout: 50 * time.Millisecond}, Address: "192.0.2.1:65530"}
	if err := cfg.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	ch := make(chan define.Event, 1)

	New(cfg).Run(context.Background(), ch)

	ev := <-ch
	data := ev.GetData().(map[string]any)
	metrics := data["metrics"].(map[string]float64)
	if metrics["success"] != 0 {
		t.Fatalf("success = %v, want 0", metrics["success"])
	}
	if data["error"] == "" {
		t.Fatal("expected non-empty error")
	}
}

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package udp

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

func startUDPEchoServer(t *testing.T) net.PacketConn {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	go func() {
		buf := make([]byte, 1024)
		n, addr, err := conn.ReadFrom(buf)
		if err == nil {
			conn.WriteTo(buf[:n], addr)
		}
	}()
	return conn
}

func TestGather_Run_emitsSuccess_whenUDPEchoReplies(t *testing.T) {
	server := startUDPEchoServer(t)
	defer server.Close()
	cfg := &configs.UDPConfig{BaseTaskParam: configs.BaseTaskParam{TaskID: 21, Timeout: time.Second}, Address: server.LocalAddr().String(), Payload: "hello", ExpectReply: true}
	if err := cfg.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	ch := make(chan define.Event, 1)

	New(cfg).Run(context.Background(), ch)

	ev := <-ch
	if ev.GetType() != "udp_event" {
		t.Fatalf("event type = %q, want udp_event", ev.GetType())
	}
	data := ev.GetData().(map[string]any)
	metrics := data["metrics"].(map[string]float64)
	if metrics["success"] != 1 || metrics["bytes_written"] != 5 || metrics["bytes_read"] != 5 || metrics["round_trip_ms"] < 0 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

func TestGather_Run_emitsFailure_whenUDPReplyTimesOut(t *testing.T) {
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer server.Close()
	cfg := &configs.UDPConfig{BaseTaskParam: configs.BaseTaskParam{TaskID: 22, Timeout: 30 * time.Millisecond}, Address: server.LocalAddr().String(), Payload: "hello", ExpectReply: true}
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

func TestGather_Run_emitsSuccess_whenUDPWriteOnly(t *testing.T) {
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer server.Close()
	cfg := &configs.UDPConfig{BaseTaskParam: configs.BaseTaskParam{TaskID: 23, Timeout: time.Second}, Address: server.LocalAddr().String(), Payload: "hello"}
	if err := cfg.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	ch := make(chan define.Event, 1)

	New(cfg).Run(context.Background(), ch)

	ev := <-ch
	data := ev.GetData().(map[string]any)
	metrics := data["metrics"].(map[string]float64)
	if metrics["success"] != 1 || metrics["bytes_written"] != 5 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

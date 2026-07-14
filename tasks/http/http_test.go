// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

func TestGather_Run_emitsSuccess_whenHTTPStatusMatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()
	cfg := &configs.HTTPConfig{BaseTaskParam: configs.BaseTaskParam{TaskID: 31, Timeout: time.Second}, URL: server.URL, ExpectedCode: http.StatusOK}
	if err := cfg.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	ch := make(chan define.Event, 1)

	New(cfg).Run(context.Background(), ch)

	ev := <-ch
	if ev.GetType() != "http_event" {
		t.Fatalf("event type = %q, want http_event", ev.GetType())
	}
	data := ev.GetData().(map[string]any)
	metrics := data["metrics"].(map[string]float64)
	if metrics["success"] != 1 || metrics["status_code"] != 200 || metrics["total_ms"] < 0 || metrics["ttfb_ms"] < 0 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

func TestGather_Run_emitsFailure_whenHTTPStatusMismatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()
	cfg := &configs.HTTPConfig{BaseTaskParam: configs.BaseTaskParam{TaskID: 32, Timeout: time.Second}, URL: server.URL, ExpectedCode: http.StatusOK}
	if err := cfg.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	ch := make(chan define.Event, 1)

	New(cfg).Run(context.Background(), ch)

	ev := <-ch
	data := ev.GetData().(map[string]any)
	metrics := data["metrics"].(map[string]float64)
	if metrics["success"] != 0 || metrics["status_code"] != http.StatusTeapot {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
	if data["error"] == "" {
		t.Fatal("expected non-empty error")
	}
}

func TestGather_Run_recordsTLSMetric_whenHTTPS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cfg := &configs.HTTPConfig{BaseTaskParam: configs.BaseTaskParam{TaskID: 33, Timeout: time.Second}, URL: server.URL, ExpectedCode: http.StatusOK}
	if err := cfg.Clean(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	ch := make(chan define.Event, 1)

	NewWithClient(cfg, server.Client()).Run(context.Background(), ch)

	ev := <-ch
	data := ev.GetData().(map[string]any)
	metrics := data["metrics"].(map[string]float64)
	if metrics["success"] != 1 || metrics["tls_ms"] <= 0 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

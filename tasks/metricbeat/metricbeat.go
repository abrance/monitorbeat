// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package metricbeat implements Prometheus metrics pull (lightweight placeholder).
//
// P2 MVP:
//   - HTTP GET to configured URL
//   - Parse response as prometheus text format
//   - Emit one metricbeat_event per run with all parsed metrics
//
// Reuses internal/script/parse for prometheus text parsing.
package metricbeat

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/script/parse"
	"github.com/abrance/monitorbeat/tasks"
)

const EventType = "metricbeat_event"

var client = &http.Client{Timeout: 10 * time.Second}

func init() {
	tasks.RegisterBuilder(define.ModuleMetricbeat, func(tc define.TaskConfig) (define.Task, error) {
		cfg, ok := tc.(*configs.MetricbeatConfig)
		if !ok {
			return nil, fmt.Errorf("metricbeat: config type mismatch: %T", tc)
		}
		if cfg.URL == "" {
			return nil, fmt.Errorf("metricbeat: url is required")
		}
		return New(cfg), nil
	})
}

type Gather struct {
	tasks.BaseTask
	cfg *configs.MetricbeatConfig
}

func New(cfg *configs.MetricbeatConfig) define.Task {
	g := &Gather{cfg: cfg}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	start := time.Now()

	client.Timeout = g.cfg.Timeout
	metrics, labels, fetchErr := g.fetch(ctx)

	data := map[string]any{
		"metrics": metrics,
		"labels":  labels,
		"cost_ms": float64(time.Since(start).Milliseconds()),
	}

	if fetchErr != nil {
		data["error"] = fetchErr.Error()
	}

	select {
	case e <- define.NewEvent(EventType, data):
	case <-ctx.Done():
	}
}

func (g *Gather) fetch(ctx context.Context) (map[string]float64, map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.cfg.URL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, nil, fmt.Errorf("read body: %w", err)
	}

	return parse.Parse(g.cfg.Format, string(body))
}

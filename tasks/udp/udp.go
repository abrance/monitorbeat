// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package udp

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/tasks"
	"github.com/abrance/monitorbeat/tasks/probe"
)

func init() {
	tasks.RegisterBuilder(define.ModuleUDP, func(tc define.TaskConfig) (define.Task, error) {
		cfg, ok := tc.(*configs.UDPConfig)
		if !ok {
			return nil, fmt.Errorf("udp: config type mismatch: %T", tc)
		}
		return New(cfg), nil
	})
}

type Gather struct {
	tasks.BaseTask
	cfg *configs.UDPConfig
}

func New(cfg *configs.UDPConfig) define.Task {
	g := &Gather{cfg: cfg}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	start := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, g.cfg.GetTimeout())
	defer cancel()

	result := g.probe(probeCtx, start)
	select {
	case e <- probe.BuildEvent("udp", g.cfg.Address, g.GetTaskID(), result):
	case <-ctx.Done():
	}
}

func (g *Gather) probe(ctx context.Context, start time.Time) probe.Result {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "udp", g.cfg.Address)
	if err != nil {
		duration := time.Since(start)
		return probe.Result{Duration: duration, Error: err.Error(), Metrics: map[string]float64{"round_trip_ms": probe.DurationMillis(duration)}}
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if ok {
		if err := conn.SetDeadline(deadline); err != nil {
			duration := time.Since(start)
			return probe.Result{Duration: duration, Error: err.Error(), Metrics: map[string]float64{"round_trip_ms": probe.DurationMillis(duration)}}
		}
	}

	payload := []byte(g.cfg.Payload)
	if len(payload) == 0 {
		payload = []byte("monitorbeat")
	}
	n, err := conn.Write(payload)
	if err != nil {
		duration := time.Since(start)
		return probe.Result{Duration: duration, Error: err.Error(), Metrics: map[string]float64{"bytes_written": float64(n), "round_trip_ms": probe.DurationMillis(duration)}}
	}

	metrics := map[string]float64{"bytes_written": float64(n)}
	if g.cfg.ExpectReply {
		buf := make([]byte, 1024)
		n, err = conn.Read(buf)
		duration := time.Since(start)
		metrics["bytes_read"] = float64(n)
		metrics["round_trip_ms"] = probe.DurationMillis(duration)
		if err != nil {
			return probe.Result{Duration: duration, Error: err.Error(), Metrics: metrics}
		}
		return probe.Result{Success: true, Duration: duration, Metrics: metrics}
	}

	duration := time.Since(start)
	metrics["round_trip_ms"] = probe.DurationMillis(duration)
	return probe.Result{Success: true, Duration: duration, Metrics: metrics}
}

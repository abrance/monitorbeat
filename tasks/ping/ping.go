// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package ping

import (
	"context"
	"fmt"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/tasks"
	"github.com/abrance/monitorbeat/tasks/probe"
)

// backend labels accepted by PingConfig.Backend. Anything else is treated
// as an explicit configuration error rather than silently defaulting.
const (
	BackendICMP    = "icmp"
	BackendCommand = "command"
)

func init() {
	tasks.RegisterBuilder(define.ModulePing, func(tc define.TaskConfig) (define.Task, error) {
		cfg, ok := tc.(*configs.PingConfig)
		if !ok {
			return nil, fmt.Errorf("ping: config type mismatch: %T", tc)
		}
		return New(cfg), nil
	})
}

// Gather is the scheduler-visible ping task. It dispatches to a backend
// implementation (ICMP or system `ping`) and emits a single normalized
// probe event per Run invocation.
type Gather struct {
	tasks.BaseTask
	cfg *configs.PingConfig
}

// New wraps a PingConfig into a runnable Task. The returned Gather
// satisfies define.Task; readiness status is set immediately so the
// scheduler can pick it up without extra wiring.
func New(cfg *configs.PingConfig) define.Task {
	g := &Gather{cfg: cfg}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

// Run performs a single round-trip probe and emits exactly one event on
// the supplied channel. The backend is selected from cfg.Backend; an
// unrecognised backend produces a failure event instead of an unbounded
// wait so the scheduler does not stall.
func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	start := time.Now()

	probeCtx, cancel := context.WithTimeout(ctx, g.cfg.GetTimeout())
	defer cancel()

	result := g.probe(probeCtx, start)

	select {
	case e <- probe.BuildEvent("ping", g.cfg.Target, g.GetTaskID(), result):
	case <-ctx.Done():
	}
}

// probe routes to the configured backend and returns the resulting
// probe.Result. Centralising the dispatch keeps Run small and lets unit
// tests exercise backends directly.
func (g *Gather) probe(ctx context.Context, start time.Time) probe.Result {
	switch g.cfg.Backend {
	case BackendCommand:
		return runCommandBackend(ctx, start, g.cfg)
	case BackendICMP:
		return runICMPBackend(ctx, start, g.cfg)
	default:
		duration := time.Since(start)
		return probe.Result{
			Duration: duration,
			Error:    fmt.Sprintf("%v: %q", errBackendUnsupported, g.cfg.Backend),
			Metrics:  map[string]float64{},
		}
	}
}

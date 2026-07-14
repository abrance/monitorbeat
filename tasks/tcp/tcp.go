// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package tcp

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
	tasks.RegisterBuilder(define.ModuleTCP, func(tc define.TaskConfig) (define.Task, error) {
		cfg, ok := tc.(*configs.TCPConfig)
		if !ok {
			return nil, fmt.Errorf("tcp: config type mismatch: %T", tc)
		}
		return New(cfg), nil
	})
}

type Gather struct {
	tasks.BaseTask
	cfg *configs.TCPConfig
}

func New(cfg *configs.TCPConfig) define.Task {
	g := &Gather{cfg: cfg}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	start := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, g.cfg.GetTimeout())
	defer cancel()

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(probeCtx, "tcp", g.cfg.Address)
	duration := time.Since(start)
	result := probe.Result{
		Duration: duration,
		Metrics:  map[string]float64{"connect_ms": probe.DurationMillis(duration)},
		Success:  err == nil,
	}
	if err != nil {
		result.Error = err.Error()
	} else {
		if closeErr := conn.Close(); closeErr != nil {
			result.Success = false
			result.Error = closeErr.Error()
		}
	}

	select {
	case e <- probe.BuildEvent("tcp", g.cfg.Address, g.GetTaskID(), result):
	case <-ctx.Done():
	}
}

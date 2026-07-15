// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package gatherupbeat 实现采集器健康心跳上报任务。
//
// P2 MVP：
//   - 上报 uptime、task_id，证明采集器存活
//   - 一次 Run 发一条 gather_up_beat_event
package gatherupbeat

import (
	"context"
	"fmt"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/tasks"
)

const EventType = "gather_up_beat_event"

var startTime = time.Now()

func init() {
	tasks.RegisterBuilder(define.ModuleGatherUpBeat, func(tc define.TaskConfig) (define.Task, error) {
		cfg, ok := tc.(*configs.GatherUpBeatConfig)
		if !ok {
			return nil, fmt.Errorf("gather_up_beat: config type mismatch: %T", tc)
		}
		return New(cfg), nil
	})
}

// Gather is gather_up_beat task runtime.
type Gather struct {
	tasks.BaseTask
	cfg *configs.GatherUpBeatConfig
}

// New constructs gather_up_beat task.
func New(cfg *configs.GatherUpBeatConfig) define.Task {
	g := &Gather{cfg: cfg}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

// Run emits gather_up_beat_event with uptime and task_id.
func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	data := map[string]any{
		"uptime_sec": time.Since(startTime).Seconds(),
		"task_id":    g.cfg.TaskID,
	}

	select {
	case e <- define.NewEvent(EventType, data):
	case <-ctx.Done():
	}
}

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package selfstats 实现采集器自监控任务。
//
// P2 MVP：
//   - 采集 Go runtime 指标：goroutines、heap alloc、GC、threads
//   - 一次 Run 发一条 selfstats_event
package selfstats

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/tasks"
)

const EventType = "selfstats_event"

var startTime = time.Now()

func init() {
	tasks.RegisterBuilder(define.ModuleSelfStats, func(tc define.TaskConfig) (define.Task, error) {
		cfg, ok := tc.(*configs.SelfStatsConfig)
		if !ok {
			return nil, fmt.Errorf("selfstats: config type mismatch: %T", tc)
		}
		return New(cfg), nil
	})
}

// Gather is selfstats task runtime.
type Gather struct {
	tasks.BaseTask
	cfg *configs.SelfStatsConfig
}

// New constructs selfstats task.
func New(cfg *configs.SelfStatsConfig) define.Task {
	g := &Gather{cfg: cfg}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

// Run collects Go runtime metrics and emits one selfstats_event.
func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	data := map[string]any{
		"uptime_sec":        time.Since(startTime).Seconds(),
		"num_goroutine":     runtime.NumGoroutine(),
		"num_cpu":           runtime.NumCPU(),
		"num_threads":       0, // runtime doesn't expose directly; use debug.SetMaxThreads indirectly
		"heap_alloc_mb":     float64(ms.HeapAlloc) / (1024 * 1024),
		"heap_sys_mb":       float64(ms.HeapSys) / (1024 * 1024),
		"heap_objects":      ms.HeapObjects,
		"num_gc":            ms.NumGC,
		"gc_pause_total_ns": ms.PauseTotalNs,
		"alloc_mb":          float64(ms.Alloc) / (1024 * 1024),
		"total_alloc_mb":    float64(ms.TotalAlloc) / (1024 * 1024),
		"sys_mb":            float64(ms.Sys) / (1024 * 1024),
		"go_version":        runtime.Version(),
	}

	select {
	case e <- define.NewEvent(EventType, data):
	case <-ctx.Done():
	}
}

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package processbeat 实现进程性能采集任务（processbeat）。
//
// P2 MVP：
//   - 通过 gopsutil/process 采集所有进程信息
//   - 按 process_names 精确名或 process_regex 正则匹配过滤
//   - 采集指标：CPU%、内存%、RSS、VMS、FD 数、线程数
//   - 按 CPU 排序取 top_n 个进程，发一条 processbeat_event
//
// 参考 bkmonitorbeat/tasks/processbeat/，砍掉 CMDB 配置、端口检测、netlink、PID 映射。
package processbeat

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/shirou/gopsutil/v3/process"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
	"github.com/abrance/monitorbeat/tasks"
)

const EventType = "processbeat_event"

func init() {
	tasks.RegisterBuilder(define.ModuleProcessbeat, func(tc define.TaskConfig) (define.Task, error) {
		cfg, ok := tc.(*configs.ProcessbeatConfig)
		if !ok {
			return nil, fmt.Errorf("processbeat: config type mismatch: %T", tc)
		}
		return New(cfg), nil
	})
}

// Gather 是 processbeat task 的运行时实例。
type Gather struct {
	tasks.BaseTask
	cfg *configs.ProcessbeatConfig
}

// New 构造 processbeat task。
func New(cfg *configs.ProcessbeatConfig) define.Task {
	g := &Gather{cfg: cfg}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

// procInfo 是单个进程采集结果。
type procInfo struct {
	PID        int32   `json:"pid"`
	Name       string  `json:"name"`
	Cmdline    string  `json:"cmdline"`
	CPUPercent float64 `json:"cpu_percent"`
	MemPercent float32 `json:"mem_percent"`
	RSSBytes   uint64  `json:"rss_bytes"`
	VMSBytes   uint64  `json:"vms_bytes"`
	NumFDs     int32   `json:"num_fds"`
	NumThreads int32   `json:"num_threads"`
}

// Run 采集进程性能数据，发一条 processbeat_event。
func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	start := time.Now()

	procs, err := g.collect()
	if err != nil {
		logging.Error("processbeat: collect failed", "err", err)
		return
	}

	data := map[string]any{
		"dimensions": map[string]string{
			"hostname": tasks.Hostname(),
		},
		"metrics": map[string]float64{
			"total":   float64(len(procs)),
			"cost_ms": float64(time.Since(start).Milliseconds()),
		},
		"processes": procs,
	}

	select {
	case e <- define.NewEvent(EventType, data):
	case <-ctx.Done():
	}
}

func (g *Gather) collect() ([]procInfo, error) {
	allProcs, err := process.Processes()
	if err != nil {
		return nil, fmt.Errorf("process.Processes: %w", err)
	}

	var nameSet map[string]bool
	if len(g.cfg.ProcessNames) > 0 {
		nameSet = make(map[string]bool, len(g.cfg.ProcessNames))
		for _, n := range g.cfg.ProcessNames {
			nameSet[n] = true
		}
	}

	var re *regexp.Regexp
	if g.cfg.ProcessRegex != "" {
		re, err = regexp.Compile(g.cfg.ProcessRegex)
		if err != nil {
			return nil, fmt.Errorf("process_regex compile: %w", err)
		}
	}

	var results []procInfo
	for _, p := range allProcs {
		name, err := p.Name()
		if err != nil {
			continue
		}

		// 过滤：如果配置了 process_names，必须匹配
		if nameSet != nil && !nameSet[name] {
			continue
		}

		// 过滤：如果配置了 process_regex，cmdline 必须匹配
		if re != nil {
			cmdline, err := p.Cmdline()
			if err != nil || !re.MatchString(cmdline) {
				continue
			}
		}

		// 如果没配置任何过滤条件，采集所有进程
		info := g.collectProc(p, name)
		results = append(results, info)
	}

	// 按 CPU 排序，取 top_n
	sort.Slice(results, func(i, j int) bool {
		return results[i].CPUPercent > results[j].CPUPercent
	})
	if g.cfg.TopN > 0 && len(results) > g.cfg.TopN {
		results = results[:g.cfg.TopN]
	}

	return results, nil
}

func (g *Gather) collectProc(p *process.Process, name string) procInfo {
	info := procInfo{
		PID:  p.Pid,
		Name: name,
	}

	// cmdline
	if cmdline, err := p.Cmdline(); err == nil {
		info.Cmdline = cmdline
	}

	// CPU percent (may need a warm-up interval to be non-zero)
	if cpu, err := p.CPUPercent(); err == nil {
		info.CPUPercent = cpu
	}

	// memory percent
	if mem, err := p.MemoryPercent(); err == nil {
		info.MemPercent = mem
	}

	// memory info (RSS, VMS)
	if memInfo, err := p.MemoryInfo(); err == nil {
		info.RSSBytes = memInfo.RSS
		info.VMSBytes = memInfo.VMS
	}

	// file descriptors
	if fds, err := p.NumFDs(); err == nil {
		info.NumFDs = fds
	}

	// threads
	if threads, err := p.NumThreads(); err == nil {
		info.NumThreads = threads
	}

	return info
}

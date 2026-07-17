// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package exceptionbeat 实现异常检测采集任务（exceptionbeat）。
//
// P2 MVP：
//   - 四个子 collector：diskro / diskspace / corefile / outofmem
//   - 统一 period 定时触发，一次 Run() 执行所有启用的子 collector
//   - 发一条 exceptionbeat_event，data 内含各子 collector 的结果列表
package exceptionbeat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/disk"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
	"github.com/abrance/monitorbeat/tasks"
)

// Event type constant.
const EventType = "exceptionbeat_event"

func init() {
	tasks.RegisterBuilder(define.ModuleExceptionbeat, func(tc define.TaskConfig) (define.Task, error) {
		cfg, ok := tc.(*configs.ExceptionbeatConfig)
		if !ok {
			return nil, fmt.Errorf("exceptionbeat: config type mismatch: %T", tc)
		}
		return New(cfg), nil
	})
}

// Gather 是 exceptionbeat task 的运行时实例。
type Gather struct {
	tasks.BaseTask
	cfg *configs.ExceptionbeatConfig

	// corefile 子 collector 的跨周期状态
	lastCoreFiles map[string]bool // 上次发现的 core file 路径集合
	lastCoreCheck time.Time       // 上次检查时间

	// oom 子 collector 的跨周期状态
	lastOOMKillCount int64 // /proc/vmstat oom_kill 上次值
}

// New 构造 exceptionbeat task；cfg 必须已经过 Clean() 处理。
func New(cfg *configs.ExceptionbeatConfig) define.Task {
	g := &Gather{
		cfg:           cfg,
		lastCoreFiles: make(map[string]bool),
	}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

// Run 执行所有启用的子 collector，发一条 exceptionbeat_event。
func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	start := time.Now()

	var (
		diskRO    = make([]map[string]any, 0)
		diskSpace = make([]map[string]any, 0)
		corefiles = make([]map[string]any, 0)
		ooms      = make([]map[string]any, 0)
	)

	if g.cfg.CheckDiskRO {
		diskRO = g.collectDiskRO()
	}
	if g.cfg.CheckDiskSpace {
		diskSpace = g.collectDiskSpace()
	}
	if g.cfg.CheckCorefile {
		corefiles = g.collectCorefile()
	}
	if g.cfg.CheckOOM {
		ooms = g.collectOOM()
	}

	data := map[string]any{
		"dimensions": map[string]string{
			"hostname": tasks.Hostname(),
		},
		"metrics": map[string]float64{
			"cost_ms":          float64(time.Since(start).Milliseconds()),
			"disk_ro_count":    float64(len(diskRO)),
			"disk_space_count": float64(len(diskSpace)),
			"corefile_count":   float64(len(corefiles)),
			"oom_count":        float64(len(ooms)),
		},
		"disk_ro":    diskRO,
		"disk_space": diskSpace,
		"corefile":   corefiles,
		"oom":        ooms,
	}

	select {
	case e <- define.NewEvent(EventType, data):
	case <-ctx.Done():
	}
}

// ---------- diskro ----------

func (g *Gather) collectDiskRO() []map[string]any {
	parts, err := disk.Partitions(false)
	if err != nil {
		logging.Error("exceptionbeat: disk partitions failed", "err", err)
		return nil
	}

	results := make([]map[string]any, 0)
	for _, p := range parts {
		readOnly := isReadOnly(p.Opts)
		if !readOnly {
			continue
		}
		// 白名单：命中则一定上报
		if matchAny(p.Mountpoint, g.cfg.DiskROWhiteList) {
			results = append(results, roItem(p, "white_list"))
			continue
		}
		// 黑名单：命中则跳过
		if matchAny(p.Mountpoint, g.cfg.DiskROBlackList) {
			continue
		}
		results = append(results, roItem(p, "detected"))
	}
	return results
}

func isReadOnly(opts []string) bool {
	for _, s := range opts {
		if s == "ro" {
			return true
		}
	}
	return false
}

func roItem(p disk.PartitionStat, reason string) map[string]any {
	return map[string]any{
		"mount_point": p.Mountpoint,
		"device":      p.Device,
		"fstype":      p.Fstype,
		"opts":        strings.Join(p.Opts, ","),
		"reason":      reason,
	}
}

func matchAny(s string, patterns []string) bool {
	for _, pat := range patterns {
		matched, err := filepath.Match(pat, s)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// ---------- diskspace ----------

func (g *Gather) collectDiskSpace() []map[string]any {
	parts, err := disk.Partitions(false)
	if err != nil {
		logging.Error("exceptionbeat: disk partitions failed", "err", err)
		return nil
	}

	results := make([]map[string]any, 0)
	for _, p := range parts {
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue
		}
		usedPercent := usage.UsedPercent
		freeGB := float64(usage.Free) / (1024 * 1024 * 1024)

		alert := false
		var reason string
		if usedPercent > float64(g.cfg.DiskUsagePercent) {
			alert = true
			reason = fmt.Sprintf("usage %.1f%% > %d%%", usedPercent, g.cfg.DiskUsagePercent)
		}
		if int(freeGB) < g.cfg.DiskMinFreeGB {
			if alert {
				reason += "; "
			}
			alert = true
			reason += fmt.Sprintf("free %.1f GB < %d GB", freeGB, g.cfg.DiskMinFreeGB)
		}

		if alert {
			results = append(results, map[string]any{
				"mount_point":         p.Mountpoint,
				"device":              p.Device,
				"fstype":              p.Fstype,
				"total_gb":            float64(usage.Total) / (1024 * 1024 * 1024),
				"used_gb":             float64(usage.Used) / (1024 * 1024 * 1024),
				"free_gb":             freeGB,
				"used_percent":        usedPercent,
				"inodes_used_percent": usage.InodesUsedPercent,
				"reason":              reason,
			})
		}
	}
	return results
}

// ---------- corefile ----------

const corePatternFile = "/proc/sys/kernel/core_pattern"

func (g *Gather) collectCorefile() []map[string]any {
	now := time.Now()

	coreDir := g.resolveCoreDir()
	if coreDir == "" {
		return nil
	}

	entries, err := os.ReadDir(coreDir)
	if err != nil {
		logging.Error("exceptionbeat: read core dir failed", "dir", coreDir, "err", err)
		return nil
	}

	results := make([]map[string]any, 0)
	currentFiles := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(coreDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		currentFiles[fullPath] = true

		// 只上报在上次检查之后修改的新文件
		if g.lastCoreCheck.IsZero() {
			// 首次运行，记录状态但不告警（避免把历史 core file 全报出来）
			continue
		}
		if info.ModTime().Before(g.lastCoreCheck) {
			continue
		}

		results = append(results, map[string]any{
			"file_path": fullPath,
			"file_name": entry.Name(),
			"file_size": info.Size(),
			"mod_time":  info.ModTime().Unix(),
		})
	}

	g.lastCoreFiles = currentFiles
	g.lastCoreCheck = now
	return results
}

func (g *Gather) resolveCoreDir() string {
	if g.cfg.CorefilePattern != "" {
		dir := filepath.Dir(g.cfg.CorefilePattern)
		if dir != "" && dir != "." {
			return dir
		}
	}

	data, err := os.ReadFile(corePatternFile)
	if err != nil {
		return ""
	}
	pattern := strings.TrimSpace(string(data))
	if pattern == "" || pattern[0] == '|' {
		return ""
	}
	// pattern like "/var/core/core.%e.%p"
	dir := filepath.Dir(pattern)
	if dir == "" || dir == "." {
		return ""
	}
	return dir
}

// ---------- outofmem ----------

const vmstatFile = "/proc/vmstat"

func (g *Gather) collectOOM() []map[string]any {
	data, err := os.ReadFile(vmstatFile)
	if err != nil {
		logging.Error("exceptionbeat: read vmstat failed", "err", err)
		return nil
	}

	currentCount := g.parseOOMKillCount(string(data))
	if g.lastOOMKillCount == 0 {
		// 首次运行，记录基线
		g.lastOOMKillCount = currentCount
		return nil
	}

	if currentCount <= g.lastOOMKillCount {
		return nil
	}

	newOOMs := currentCount - g.lastOOMKillCount
	g.lastOOMKillCount = currentCount

	return []map[string]any{
		{
			"new_oom_count": newOOMs,
			"total_count":   currentCount,
		},
	}
}

func (g *Gather) parseOOMKillCount(content string) int64 {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "oom_kill ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				v, err := strconv.ParseInt(parts[1], 10, 64)
				if err == nil {
					return v
				}
			}
		}
	}
	return 0
}

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package basereport 提供主机基础指标采集任务。
//
// 对照 bkmonitorbeat/tasks/basereport/basereport.go：
//   - 砍 fastRunOnce/CollectItem/IsDiffMinLastPublish 等下沉逻辑
//   - 砍 tasks.CmdbEventSender，事件直接走 define.Event chan
//   - CPU/Mem/Disk/Load/Net 五类指标一次采集、一条事件
package basereport

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
	"github.com/abrance/monitorbeat/tasks"
)

// defaultCPUSampleWindow 是 cpu.Percent 默认采样窗口。
const defaultCPUSampleWindow = 1 * time.Second

// defaultDiskPath 默认采集的磁盘挂载点。
var defaultDiskPath = "/"

func init() {
	tasks.RegisterBuilder(define.ModuleBasereport, func(tc define.TaskConfig) (define.Task, error) {
		cfg, ok := tc.(*configs.BasereportConfig)
		if !ok {
			return nil, fmt.Errorf("basereport: config type mismatch: %T", tc)
		}
		return New(cfg), nil
	})
}

// Gather 是 basereport task 实现。
type Gather struct {
	tasks.BaseTask
	cfg *configs.BasereportConfig
}

// New 构造 basereport task；cfg 必须已经过 Clean() 处理。
func New(cfg *configs.BasereportConfig) define.Task {
	g := &Gather{cfg: cfg}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

// Run 执行一次 basereport 采集，向 e 发送一条 SimpleEvent。
//
// 数据形态：{"dimensions": {...}, "metrics": {...}}
// 各子项失败只记日志、跳过，不阻塞其他指标采集。
func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	dims := g.dimensions()
	metrics := make(map[string]float64)

	g.collectCPU(ctx, metrics)
	g.collectMem(metrics)
	g.collectDisk(metrics)
	g.collectLoad(metrics)
	g.collectNet(metrics)

	data := map[string]any{
		"dimensions": dims,
		"metrics":    metrics,
	}

	select {
	case e <- define.NewEvent(define.ModuleBasereport, data):
	case <-ctx.Done():
	}
}

// dimensions 返回事件维度集合：hostname + 启动时确定的 host info。
func (g *Gather) dimensions() map[string]string {
	hostname := os.Getenv("MONITORBEAT_HOSTNAME")
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
	}
	dims := map[string]string{
		"hostname": hostname,
	}
	if hi, err := host.Info(); err == nil {
		dims["os"] = hi.OS
		dims["platform"] = hi.Platform
		dims["kernel_version"] = hi.KernelVersion
		dims["arch"] = hi.KernelArch
	}
	return dims
}

// collectCPU 调用 cpu.Percent 采样 InfoPeriod 时长。
//
// 这是 Run 中唯一阻塞的子采集（默认 1s），其他子项都是即时返回。
func (g *Gather) collectCPU(ctx context.Context, metrics map[string]float64) {
	window := g.cfg.Cpu.InfoPeriod
	if window <= 0 {
		window = defaultCPUSampleWindow
	}
	pcts, err := cpu.Percent(window, false)
	if err != nil {
		logging.Error("basereport cpu.percent failed", "err", err, "task_id", g.GetTaskID())
		return
	}
	// ctx 在采样窗口期间可能已被取消，跳过本次结果避免发送过期事件。
	select {
	case <-ctx.Done():
		return
	default:
	}
	if len(pcts) > 0 {
		metrics["cpu_usage"] = pcts[0]
	}
}

// collectMem 采集虚拟内存统计。
func (g *Gather) collectMem(metrics map[string]float64) {
	if !g.cfg.Mem.Enabled {
		return
	}
	v, err := mem.VirtualMemory()
	if err != nil {
		logging.Error("basereport mem failed", "err", err)
		return
	}
	metrics["mem_total_bytes"] = float64(v.Total)
	metrics["mem_available_bytes"] = float64(v.Available)
	metrics["mem_used_bytes"] = float64(v.Used)
	metrics["mem_used_percent"] = v.UsedPercent
}

// collectDisk 采集配置中各挂载点的容量使用率。
//
// 字段命名：disk_<sanitized_path>_used_percent。
// 多路径时按相同规则展开。
func (g *Gather) collectDisk(metrics map[string]float64) {
	if !g.cfg.Disk.Enabled {
		return
	}
	paths := g.cfg.Disk.Paths
	if len(paths) == 0 {
		paths = []string{defaultDiskPath}
	}
	for _, p := range paths {
		u, err := disk.Usage(p)
		if err != nil {
			logging.Error("basereport disk usage failed", "path", p, "err", err)
			continue
		}
		key := sanitizeMetricKey(p)
		metrics["disk_"+key+"_total_bytes"] = float64(u.Total)
		metrics["disk_"+key+"_used_bytes"] = float64(u.Used)
		metrics["disk_"+key+"_used_percent"] = u.UsedPercent
	}
}

// collectLoad 采集系统平均负载（仅 Linux/macOS）。
func (g *Gather) collectLoad(metrics map[string]float64) {
	if !g.cfg.Load.Enabled {
		return
	}
	avg, err := load.Avg()
	if err != nil {
		// Windows 等平台 load.Avg 返回 not supported，不刷错误日志。
		return
	}
	metrics["load1"] = avg.Load1
	metrics["load5"] = avg.Load5
	metrics["load15"] = avg.Load15
}

// collectNet 采集所有网卡的累计 IO 字节数。
func (g *Gather) collectNet(metrics map[string]float64) {
	if !g.cfg.Net.Enabled {
		return
	}
	counters, err := net.IOCounters(false)
	if err != nil {
		logging.Error("basereport net failed", "err", err)
		return
	}
	if len(counters) == 0 {
		return
	}
	c := counters[0] // percpu=false 返回 all 接口合计
	metrics["net_bytes_sent"] = float64(c.BytesSent)
	metrics["net_bytes_recv"] = float64(c.BytesRecv)
	metrics["net_packets_sent"] = float64(c.PacketsSent)
	metrics["net_packets_recv"] = float64(c.PacketsRecv)
}

// sanitizeMetricKey 把 "/var/log" 转为 "var_log"，"." 不允许在 metric 名中。
func sanitizeMetricKey(p string) string {
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, c)
		case c == '/' || c == '.' || c == '-' || c == '_':
			out = append(out, '_')
		}
	}
	// 去掉首尾下划线。
	for len(out) > 0 && out[0] == '_' {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == '_' {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return "root"
	}
	return string(out)
}

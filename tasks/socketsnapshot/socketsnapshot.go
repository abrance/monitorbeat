// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package socketsnapshot 实现 Socket 连接快照采集任务。
//
// P2 MVP：
//   - 使用 gopsutil/net.Connections 采集所有 TCP/UDP 连接
//   - 每条连接包含 PID、协议、状态、本地/远端地址端口
//   - 一次 Run 发一条 socketsnapshot_event
//
// 参考 bkmonitorbeat/tasks/socketsnapshot/，砍掉 netlink detector、procsnapshot 缓存依赖。
package socketsnapshot

import (
	"context"
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v3/net"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
	"github.com/abrance/monitorbeat/tasks"
)

const EventType = "socketsnapshot_event"

func init() {
	tasks.RegisterBuilder(define.ModuleSocketSnapshot, func(tc define.TaskConfig) (define.Task, error) {
		cfg, ok := tc.(*configs.SocketSnapshotConfig)
		if !ok {
			return nil, fmt.Errorf("socketsnapshot: config type mismatch: %T", tc)
		}
		return New(cfg), nil
	})
}

// Gather 是 socketsnapshot task 的运行时实例。
type Gather struct {
	tasks.BaseTask
	cfg *configs.SocketSnapshotConfig
}

// New 构造 socketsnapshot task。
func New(cfg *configs.SocketSnapshotConfig) define.Task {
	g := &Gather{cfg: cfg}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

// connInfo 是单条连接的快照信息。
type connInfo struct {
	PID        int32  `json:"pid"`
	Protocol   string `json:"protocol"`
	Status     string `json:"status"`
	LocalIP    string `json:"local_ip"`
	Port       uint32 `json:"port"`
	RemoteIP   string `json:"remote_ip"`
	RemotePort uint32 `json:"remote_port"`
}

// Run 采集所有 TCP/UDP 连接，发一条 socketsnapshot_event。
func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	start := time.Now()

	conns, err := g.collect()
	if err != nil {
		logging.Error("socketsnapshot: collect failed", "err", err)
		return
	}

	data := map[string]any{
		"connections": conns,
		"total":       len(conns),
		"cost_ms":     float64(time.Since(start).Milliseconds()),
	}

	select {
	case e <- define.NewEvent(EventType, data):
	case <-ctx.Done():
	}
}

func (g *Gather) collect() ([]connInfo, error) {
	// 先采集 TCP，再采集 UDP，合并
	var results []connInfo

	for _, kind := range []string{"tcp", "udp"} {
		connections, err := net.Connections(kind)
		if err != nil {
			logging.Error("socketsnapshot: net.Connections failed", "kind", kind, "err", err)
			continue
		}
		for _, c := range connections {
			results = append(results, connInfo{
				PID:        c.Pid,
				Protocol:   kind,
				Status:     c.Status,
				LocalIP:    c.Laddr.IP,
				Port:       c.Laddr.Port,
				RemoteIP:   c.Raddr.IP,
				RemotePort: c.Raddr.Port,
			})
		}
	}

	return results, nil
}

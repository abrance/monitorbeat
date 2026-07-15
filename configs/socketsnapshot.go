// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import (
	"time"

	"github.com/abrance/monitorbeat/define"
)

const defaultSocketSnapshotPeriod = 60 * time.Second

// SocketSnapshotConfig 控制 Socket 快照采集任务。
//
// P2 MVP：
//   - 使用 gopsutil/net.Connections 采集所有 TCP/UDP 连接
//   - 一次 Run 发一条 socketsnapshot_event，包含所有连接列表
type SocketSnapshotConfig struct {
	BaseTaskParam `yaml:",inline"`
}

func (c *SocketSnapshotConfig) GetType() string { return define.ModuleSocketSnapshot }

func (c *SocketSnapshotConfig) Clean() error {
	c.BaseTaskParam.fillDefaults(define.ModuleSocketSnapshot)
	if c.Period <= 0 {
		c.Period = defaultSocketSnapshotPeriod
	}
	return nil
}

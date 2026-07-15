// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import (
	"time"

	"github.com/abrance/monitorbeat/define"
)

const (
	defaultProcessbeatPeriod = 30 * time.Second
	defaultProcessbeatTopN   = 20
)

// ProcessbeatConfig 控制进程性能采集任务。
//
// P2 MVP 限定：
//   - process_names：精确匹配进程名（如 "nginx", "mysqld"）
//   - process_regex：正则匹配 cmdline（可选）
//   - top_n：按 CPU 排序取 top N 个进程，默认 20
//   - 采集指标：CPU%、内存%、RSS、VMS、FD 数、线程数
//   - 不做端口检测、PID 映射、CMDB 配置
type ProcessbeatConfig struct {
	BaseTaskParam `yaml:",inline"`

	ProcessNames []string `yaml:"process_names"` // 精确匹配的进程名列表
	ProcessRegex string   `yaml:"process_regex"` // 正则匹配 cmdline（可选）
	TopN         int      `yaml:"top_n"`         // 上报 top N 个进程，默认 20
}

func (c *ProcessbeatConfig) GetType() string { return define.ModuleProcessbeat }

func (c *ProcessbeatConfig) Clean() error {
	c.BaseTaskParam.fillDefaults(define.ModuleProcessbeat)
	if c.Period <= 0 {
		c.Period = defaultProcessbeatPeriod
	}
	if c.TopN <= 0 {
		c.TopN = defaultProcessbeatTopN
	}
	return nil
}

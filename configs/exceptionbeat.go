// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import (
	"time"

	"github.com/abrance/monitorbeat/define"
)

const (
	defaultExceptionbeatPeriod = 60 * time.Second
	defaultDiskUsagePercent    = 90
	defaultDiskMinFreeGB       = 10
	defaultCorefileReportGap   = 60 * time.Second
	defaultOutOfMemReportGap   = 60 * time.Second
)

// ExceptionbeatConfig 控制异常检测采集任务（exceptionbeat）。
//
// P2 MVP 限定：
//   - 四个子 collector：diskro / diskspace / corefile / outofmem
//   - 统一 period 定时触发，一次 Run() 执行所有启用的子 collector
//   - 发一条 exceptionbeat_event，data 内含各子 collector 的结果列表
type ExceptionbeatConfig struct {
	BaseTaskParam `yaml:",inline"`

	// 子 collector 开关
	CheckDiskRO    bool `yaml:"check_disk_ro"`
	CheckDiskSpace bool `yaml:"check_disk_space"`
	CheckCorefile  bool `yaml:"check_corefile"`
	CheckOOM       bool `yaml:"check_oom"`

	// diskspace 阈值
	DiskUsagePercent int `yaml:"disk_usage_percent"` // 使用率超过此值告警，默认 90
	DiskMinFreeGB    int `yaml:"disk_min_free_gb"`   // 剩余空间低于此值告警，默认 10

	// diskro 过滤
	DiskROWhiteList []string `yaml:"disk_ro_white_list"`
	DiskROBlackList []string `yaml:"disk_ro_black_list"`

	// corefile
	CorefileReportGap  time.Duration `yaml:"corefile_report_gap"`
	CorefilePattern    string        `yaml:"corefile_pattern"`
	CorefileMatchRegex string        `yaml:"corefile_match_regex"`

	// outofmem
	OutOfMemReportGap time.Duration `yaml:"oom_report_gap"`
}

func (c *ExceptionbeatConfig) GetType() string { return define.ModuleExceptionbeat }

func (c *ExceptionbeatConfig) Clean() error {
	c.BaseTaskParam.fillDefaults(define.ModuleExceptionbeat)
	if c.Period <= 0 {
		c.Period = defaultExceptionbeatPeriod
	}
	if c.DiskUsagePercent <= 0 {
		c.DiskUsagePercent = defaultDiskUsagePercent
	}
	if c.DiskMinFreeGB <= 0 {
		c.DiskMinFreeGB = defaultDiskMinFreeGB
	}
	if c.CorefileReportGap <= 0 {
		c.CorefileReportGap = defaultCorefileReportGap
	}
	if c.OutOfMemReportGap <= 0 {
		c.OutOfMemReportGap = defaultOutOfMemReportGap
	}
	return nil
}

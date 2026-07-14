// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import (
	"time"

	"github.com/abrance/monitorbeat/define"
)

// CpuConfig 是 basereport 任务中 CPU 子项的配置。
type CpuConfig struct {
	// InfoPeriod 是 gopsutil cpu.Percent 的采样窗口；0 表示用默认 1s。
	InfoPeriod time.Duration `yaml:"info_period"`
}

// MemConfig 控制内存指标采集。
type MemConfig struct {
	Enabled bool `yaml:"enabled"`
}

// DiskConfig 控制磁盘容量指标采集。
type DiskConfig struct {
	Enabled bool     `yaml:"enabled"`
	Paths   []string `yaml:"paths"`
}

// LoadConfig 控制平均负载采集（Linux/macOS）。
type LoadConfig struct {
	Enabled bool `yaml:"enabled"`
}

// NetConfig 控制 IO 计数采集。
type NetConfig struct {
	Enabled bool `yaml:"enabled"`
}

// BasereportConfig 是 basereport 任务配置。
//
// 对照 bkmonitorbeat/configs/basereport.go：
//   - P0 收尾：CPU + Mem + Disk + Load + Net 全开
//   - 黑白名单、核数过滤、collect_type 字段留 P1
type BasereportConfig struct {
	BaseTaskParam `yaml:",inline"`
	Cpu           CpuConfig  `yaml:"cpu"`
	Mem           MemConfig  `yaml:"mem"`
	Disk          DiskConfig `yaml:"disk"`
	Load          LoadConfig `yaml:"load"`
	Net           NetConfig  `yaml:"net"`
}

// GetType 覆盖 BaseTaskParam.GetType，返回 basereport 模块名。
func (b *BasereportConfig) GetType() string { return define.ModuleBasereport }

// Clean 实现 define.TaskConfig.Clean：填充 Ident/TaskID 默认值。
func (b *BasereportConfig) Clean() error {
	b.BaseTaskParam.fillDefaults(define.ModuleBasereport)
	return nil
}

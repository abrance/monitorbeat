// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import (
	"time"

	"github.com/abrance/monitorbeat/define"
)

const defaultSelfStatsPeriod = 60 * time.Second

// SelfStatsConfig 控制采集器自监控任务。
//
// P2 MVP：
//   - 采集 Go runtime 指标：goroutines, heap alloc, GC 次数
//   - 一次 Run 发一条 selfstats_event
type SelfStatsConfig struct {
	BaseTaskParam `yaml:",inline"`
}

func (c *SelfStatsConfig) GetType() string { return define.ModuleSelfStats }

func (c *SelfStatsConfig) Clean() error {
	c.BaseTaskParam.fillDefaults(define.ModuleSelfStats)
	if c.Period <= 0 {
		c.Period = defaultSelfStatsPeriod
	}
	return nil
}

// GatherUpBeatConfig 控制采集器心跳上报任务。
//
// P2 MVP：
//   - 上报 version、uptime、task_count
//   - 一次 Run 发一条 gather_up_beat_event
type GatherUpBeatConfig struct {
	BaseTaskParam `yaml:",inline"`
}

func (c *GatherUpBeatConfig) GetType() string { return define.ModuleGatherUpBeat }

func (c *GatherUpBeatConfig) Clean() error {
	c.BaseTaskParam.fillDefaults(define.ModuleGatherUpBeat)
	if c.Period <= 0 {
		c.Period = defaultSelfStatsPeriod
	}
	return nil
}

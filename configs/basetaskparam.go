// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import (
	"fmt"
	"time"

	"github.com/abrance/monitorbeat/define"
)

// BaseTaskParam 是所有任务配置共享的字段集合，嵌入后即满足 define.TaskConfig 的
// 通用部分（TaskID/Ident/Period/Timeout/Enabled/Labels）。
//
// 对照 bkmonitorbeat/configs/basetaskparam.go：
//   - 砍 BizID/DataID/InitIdent/hash（BK 多租户/数据账号概念）
//   - Ident 不再 hash，直接用 "<type>:<task_id>" 字符串拼接
//   - GetType() 留给具体任务配置覆盖，BaseTaskParam 不预填 type
type BaseTaskParam struct {
	TaskID  int32               `yaml:"task_id"`
	Ident   string              `yaml:"ident,omitempty"`
	Period  time.Duration       `yaml:"period"`
	Timeout time.Duration       `yaml:"timeout"`
	Enabled bool                `yaml:"enabled"`
	Labels  []map[string]string `yaml:"labels,omitempty"`
}

// GetTaskID 返回任务 ID。
func (b *BaseTaskParam) GetTaskID() int32 { return b.TaskID }

// GetIdent 返回任务指纹（调度器去重与 reload key）。
func (b *BaseTaskParam) GetIdent() string { return b.Ident }

// SetIdent 设置任务指纹。
func (b *BaseTaskParam) SetIdent(ident string) { b.Ident = ident }

// GetTimeout 返回单次执行超时；未配置时取 define.DefaultTimeout。
func (b *BaseTaskParam) GetTimeout() time.Duration {
	if b.Timeout > 0 {
		return b.Timeout
	}
	return define.DefaultTimeout
}

// GetPeriod 返回执行间隔；未配置时取 define.DefaultPeriod。
func (b *BaseTaskParam) GetPeriod() time.Duration {
	if b.Period > 0 {
		return b.Period
	}
	return define.DefaultPeriod
}

// GetLabels 返回任务标签。
func (b *BaseTaskParam) GetLabels() []map[string]string { return b.Labels }

// GetEnabled 返回是否启用。
func (b *BaseTaskParam) GetEnabled() bool { return b.Enabled }

// GetType 默认空字符串，留给具体任务配置覆盖以返回自身模块名。
func (b *BaseTaskParam) GetType() string { return "" }

// fillDefaults 在 Clean 流程中填充缺省值：保证 TaskID 非零、Ident 非空。
//
// typ 参数由具体任务配置传入自身模块名，用于拼装 Ident。
func (b *BaseTaskParam) fillDefaults(typ string) {
	if b.TaskID == 0 {
		b.TaskID = 1
	}
	if b.Ident == "" {
		b.Ident = fmt.Sprintf("%s:%d", typ, b.TaskID)
	}
}

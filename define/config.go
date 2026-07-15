// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package define

import "time"

const (
	// DefaultTimeout 默认任务超时。
	DefaultTimeout = 3 * time.Second

	// DefaultPeriod 默认任务执行间隔。
	DefaultPeriod = 10 * time.Second

	// DefaultCheckInterval 默认调度器轮询任务队列的间隔。
	DefaultCheckInterval = 1 * time.Second

	// DefaultTaskConcurrencyLimitPerInstance 默认任务单实例并发限制。
	DefaultTaskConcurrencyLimitPerInstance = 100000

	// DefaultTaskConcurrencyLimitPerTask 默认单个任务并发限制。
	DefaultTaskConcurrencyLimitPerTask = 1000
)

// TaskConfig 是单个采集任务配置的统一契约。
//
// 去掉 bkmonitorbeat 中的 GetBizID/GetDataID/InitIdent 等
// 蓝鲸多租户、数据上报标志概念；Ident 保留为调度器去重 key。
type TaskConfig interface {
	GetTaskID() int32
	GetIdent() string
	SetIdent(ident string)
	GetTimeout() time.Duration
	GetPeriod() time.Duration
	GetType() string
	GetLabels() []map[string]string
	GetEnabled() bool
	Clean() error
}

// Config 是全局配置契约，承载所有任务配置与调度器全局参数。
type Config interface {
	GetTaskConfigListByType(string) []TaskConfig
	GetCheckInterval() time.Duration
	Clean() error
}

// CompositeConfig 表示需要清理的复合配置。
type CompositeConfig interface {
	CleanConfig() error
}

// CompositeParam 表示需要清理的复合参数。
type CompositeParam interface {
	CleanParams() error
}

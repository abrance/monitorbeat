// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License (the "License");
// you may not use this file except in compliance with the License.

package define

import "time"

// Event 表示调度器输出的一个采集事件。
//
// 替代 bkmonitorbeat 中依赖 libbeat common.MapStr 的实现，
// 砍掉 IgnoreCMDBLevel 等蓝鲸多层级下沉概念。
type Event interface {
	GetType() string
	GetData() any
	GetTimestamp() time.Time
}

// SimpleEvent 是 Event 的最小实现，用于无复杂负载的简单采集任务。
type SimpleEvent struct {
	Type      string
	Data      any
	Timestamp time.Time
}

func (e SimpleEvent) GetType() string         { return e.Type }
func (e SimpleEvent) GetData() any            { return e.Data }
func (e SimpleEvent) GetTimestamp() time.Time { return e.Timestamp }

// NewEvent 构造带当前时间戳的简单事件。
func NewEvent(typ string, data any) Event {
	return SimpleEvent{Type: typ, Data: data, Timestamp: time.Now()}
}

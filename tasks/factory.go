// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package tasks 提供所有采集任务的统一注册表。
//
// P0 阶段为解除 cmd/monitorbeat/main.go 中 switch 耦合，引入 Builder 注册模式：
// 每个 task 包在 init() 中调用 RegisterBuilder 注册自己的构造函数，
// main.go 通过 factory.Build(tc) 统一构造 task 实例。
package tasks

import (
	"fmt"
	"sync"

	"github.com/abrance/monitorbeat/define"
)

// Builder 把 TaskConfig 转换为 Task 实例。
//
// 实现方应返回 define.Task 与可能的构造错误；若配置不可用，
// 返回 error 让上层跳过该任务。
type Builder func(tc define.TaskConfig) (define.Task, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Builder)
)

// RegisterBuilder 把 type → Builder 注册到全局表。
//
// 应在 task 包 init() 中调用。重复注册同一 type 视为编程错误，panic。
func RegisterBuilder(typ string, b Builder) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, ok := registry[typ]; ok {
		panic(fmt.Sprintf("tasks: duplicate builder for type %q", typ))
	}
	registry[typ] = b
}

// Build 按 TaskConfig.GetType() 查找 Builder 并构造 task。
//
// 未注册的 type 返回 ErrUnknownTaskType，调用方应跳过该任务并记日志。
func Build(tc define.TaskConfig) (define.Task, error) {
	registryMu.RLock()
	b, ok := registry[tc.GetType()]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTaskType, tc.GetType())
	}
	return b(tc)
}

// RegisteredTypes 返回当前已注册的所有 task type，便于自检。
func RegisteredTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

// ErrUnknownTaskType 表示 factory 收到未注册的 task type。
var ErrUnknownTaskType = fmt.Errorf("tasks: unknown task type")

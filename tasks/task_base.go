// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package tasks 提供所有采集任务的公共基类与注册表。
//
// BaseTask 对照 bkmonitorbeat/tasks/task.go 中的 BaseTask：
//   - 砍 Semaphore/HookManager/EventTask/Beater 引用
//   - 仅保留 status + waitgroup + cfg/gcfg setter/getter
package tasks

import (
	"sync"

	"github.com/abrance/monitorbeat/define"
)

// BaseTask 是所有 task 实现共享的最小基类，提供 Task 接口的方法默认实现。
//
// 具体任务（如 basereport.Gather）以匿名嵌入方式复用：
//
//	type Gather struct {
//	    tasks.BaseTask
//	    cfg *configs.BasereportConfig
//	}
type BaseTask struct {
	status define.Status
	mu     sync.Mutex
	wg     sync.WaitGroup
	cfg    define.TaskConfig
	gcfg   define.Config
}

// GetTaskID 委派给当前 TaskConfig。
func (b *BaseTask) GetTaskID() int32 {
	if b.cfg == nil {
		return 0
	}
	return b.cfg.GetTaskID()
}

// GetStatus 返回当前任务状态。
func (b *BaseTask) GetStatus() define.Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

// SetStatus 设置任务状态（task 内部使用，例如 Run 完成后置 StatusFinished）。
func (b *BaseTask) SetStatus(s define.Status) {
	b.mu.Lock()
	b.status = s
	b.mu.Unlock()
}

// SetConfig 设置当前任务配置（Reload 时由调度器透传）。
func (b *BaseTask) SetConfig(c define.TaskConfig) {
	b.mu.Lock()
	b.cfg = c
	b.mu.Unlock()
}

// GetConfig 返回当前任务配置。
func (b *BaseTask) GetConfig() define.TaskConfig {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg
}

// SetGlobalConfig 设置全局配置引用。
func (b *BaseTask) SetGlobalConfig(c define.Config) {
	b.mu.Lock()
	b.gcfg = c
	b.mu.Unlock()
}

// GetGlobalConfig 返回全局配置引用。
func (b *BaseTask) GetGlobalConfig() define.Config {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gcfg
}

// Reload 默认空实现；具体任务如有需要可覆盖。
func (b *BaseTask) Reload() {}

// Wait 阻塞至所有由 Track 启动的 goroutine 结束。
func (b *BaseTask) Wait() { b.wg.Wait() }

// Stop 默认空实现；具体任务如有 ctx 取消需求可覆盖。
func (b *BaseTask) Stop() {}

// Track 包裹一个函数到 waitgroup 中，便于 task 实现启动后台 goroutine
// 并让 Wait() 正确等待。
func (b *BaseTask) Track(fn func()) {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		fn()
	}()
}

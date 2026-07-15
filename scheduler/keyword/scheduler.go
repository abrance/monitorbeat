// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

// Package keyword 提供 keyword 日志采集任务的调度器。
//
// 与 daemon 时间堆不同：
//   - 每个 task 一个长生命周期 goroutine，调用 task.Run 后阻塞等待退出
//   - 不做周期性触发；task 自己负责循环读文件并写事件
//   - Stop / ctx cancel 都会立即让所有 task 的 Run 返回
//
// IsDaemon 返回 false，与 checker 一致（不抢占 daemon 调度位）。
package keyword

import (
	"context"
	"sync"

	"github.com/emirpasic/gods/maps/treemap"

	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
)

// Scheduler 是 keyword 任务的专用调度器。
type Scheduler struct {
	*define.BaseScheduler

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	tasks  *treemap.Map
	mu     sync.RWMutex
}

// New 构造 keyword 调度器。
func New(eventChan chan<- define.Event, _ define.Config) define.Scheduler {
	return &Scheduler{
		BaseScheduler: &define.BaseScheduler{
			EventChan: eventChan,
		},
		tasks: treemap.NewWithStringComparator(),
	}
}

// Add 注册一个 task。
func (s *Scheduler) Add(task define.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks.Put(task.GetConfig().GetIdent(), task)
}

// IsDaemon keyword 调度器不是 daemon 调度器（区别于时间堆）。
func (s *Scheduler) IsDaemon() bool { return false }

// Count 已注册 task 数。
func (s *Scheduler) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks.Size()
}

// Start 启动所有 task 的 Run goroutine。
func (s *Scheduler) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.Status = define.StatusRunning

	s.mu.RLock()
	tasks := make([]define.Task, 0, s.tasks.Size())
	iter := s.tasks.Iterator()
	for iter.Next() {
		tasks = append(tasks, iter.Value().(define.Task))
	}
	s.mu.RUnlock()

	for _, task := range tasks {
		s.wg.Add(1)
		go s.runTask(s.ctx, task)
	}
	return nil
}

func (s *Scheduler) runTask(ctx context.Context, task define.Task) {
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			logging.Error("keyword scheduler task panic", "ident", task.GetConfig().GetIdent(), "panic", r)
		}
	}()
	task.Run(ctx, s.EventChan)
}

// Stop 取消 ctx，让所有 task.Run 返回。
func (s *Scheduler) Stop() {
	logging.Info("keyword scheduler stop")
	s.Status = define.StatusFinished
	if s.cancel != nil {
		s.cancel()
	}
}

// Wait 阻塞至所有 task goroutine 退出。
func (s *Scheduler) Wait() {
	s.wg.Wait()
	s.Status = define.StatusFinished
}

// Reload 热替换 task 列表。
func (s *Scheduler) Reload(_ context.Context, conf define.Config, tasks []define.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Config = conf
	s.tasks.Clear()
	for _, t := range tasks {
		s.tasks.Put(t.GetConfig().GetIdent(), t)
	}
	logging.Info("keyword scheduler reloaded", "tasks", len(tasks))
	return nil
}

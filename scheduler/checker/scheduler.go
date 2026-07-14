// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package checker 提供一次性"执行所有已注册 task 后退出"的调度器。
//
// 对照 bkmonitorbeat/scheduler/checker/scheduler.go：
//   - 砍 utils.HookManager（PreRun/PostRun）
//   - 砍 logp（用 internal/logging）
//   - 砍 panic(define.ErrTaskNoutFound)，无 task 时直接退出
//   - 砍"为测试 sleep 2s"
//
// 用途：sync/setup 类 task（如 hostid 同步、CMDB 拉取）。
// 周期性 task 走 daemon，监听型走 listen（P2）。
package checker

import (
	"context"
	"sync"

	"github.com/emirpasic/gods/maps/treemap"

	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
)

// Scheduler 是 checker 调度器实现。
type Scheduler struct {
	*define.BaseScheduler

	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	tasks   *treemap.Map // key: ident, value: define.Task
	mu      sync.RWMutex
}

// New 构造 checker 调度器。
func New(eventChan chan<- define.Event, config define.Config) define.Scheduler {
	s := &Scheduler{
		tasks: treemap.NewWithStringComparator(),
	}
	s.BaseScheduler = &define.BaseScheduler{
		EventChan: eventChan,
		Config:    config,
	}
	return s
}

// Add 注册 task 到待执行列表。
func (s *Scheduler) Add(task define.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks.Put(task.GetConfig().GetIdent(), task)
}

// IsDaemon 返回 false：checker 不是常驻调度器。
func (s *Scheduler) IsDaemon() bool { return false }

// Count 返回已注册 task 数。
func (s *Scheduler) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks.Size()
}

// Start 启动一次性执行：所有 task 同步顺序执行后退出。
func (s *Scheduler) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.Status = define.StatusRunning

	s.wg.Add(1)
	go s.run(s.ctx)
	return nil
}

// run 单 goroutine 串行执行所有 task，然后退出。
//
// 串行而非并发：checker 任务通常是配置同步类，并发反而增加调试难度；
// 若需并发可在 P1 阶段加 worker 池。
func (s *Scheduler) run(ctx context.Context) {
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			logging.Error("checker scheduler panic", "panic", r)
		}
	}()

	logging.Info("checker scheduler running", "tasks", s.Count())

	s.mu.RLock()
	tasks := make([]define.Task, 0, s.tasks.Size())
	iter := s.tasks.Iterator()
	for iter.Next() {
		tasks = append(tasks, iter.Value().(define.Task))
	}
	s.mu.RUnlock()

	for _, task := range tasks {
		select {
		case <-ctx.Done():
			logging.Info("checker scheduler cancelled")
			s.Status = define.StatusFinished
			return
		default:
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					logging.Error("checker task panic", "task_id", task.GetTaskID(), "panic", r)
				}
			}()
			task.Run(ctx, s.EventChan)
		}()
	}

	s.Status = define.StatusFinished
	logging.Info("checker scheduler finished")
}

// Stop 取消 ctx，让正在执行的 task 检测到取消信号后退出。
func (s *Scheduler) Stop() {
	logging.Info("checker scheduler stop")
	s.Status = define.StatusFinished
	if s.cancel != nil {
		s.cancel()
	}
}

// Wait 阻塞至 run goroutine 退出。
func (s *Scheduler) Wait() {
	s.wg.Wait()
	s.Status = define.StatusFinished
}

// Reload 热替换 task 列表：checker 一次性执行，Reload 等价于"换 task 后再 Start"。
//
// 调用方应先 Stop + Wait 旧实例，再用新 config New + Add + Start。
// 这里仅更新内存 task 列表与 config，不主动重启。
func (s *Scheduler) Reload(_ context.Context, conf define.Config, tasks []define.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Config = conf
	s.tasks.Clear()
	for _, t := range tasks {
		s.tasks.Put(t.GetConfig().GetIdent(), t)
	}
	logging.Info("checker scheduler reloaded", "tasks", len(tasks))
	return nil
}

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package define

import "context"

// Scheduler 是所有调度器（daemon/cron/checker/listen）的统一接口。
//
// 对齐 bkmonitorbeat/define/scheduler.go，但用本地 Status 类型避免和 Task 状态混淆。
type Scheduler interface {
	Add(Task)
	Start(context.Context) error
	IsDaemon() bool
	Count() int
	Stop()
	Wait()
	GetStatus() Status
	Reload(context.Context, Config, []Task) error
}

// BaseScheduler 提供 Scheduler 接口的默认实现，调度器类型可嵌入以复用通用逻辑。
type BaseScheduler struct {
	Status    Status
	EventChan chan<- Event
	Config    Config
}

func (s *BaseScheduler) Stop()             { s.Status = StatusFinished }
func (s *BaseScheduler) Wait()             { s.Status = StatusFinished }
func (s *BaseScheduler) GetStatus() Status { return s.Status }
func (s *BaseScheduler) IsDaemon() bool    { return true }
func (s *BaseScheduler) Count() int        { return 0 }

func (s *BaseScheduler) Start(_ context.Context) error {
	s.Status = StatusRunning
	return nil
}

func (s *BaseScheduler) Reload(_ context.Context, config Config, _ []Task) error {
	s.Config = config
	return nil
}

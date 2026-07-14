// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package daemon

import (
	"context"
	"time"

	"github.com/emirpasic/gods/utils"

	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
)

// Job 是调度器调度的最小单元，包装 Task 并维护下次执行时间。
//
// 对应 bkmonitorbeat/scheduler/daemon/job.go 中的 Job interface 与 IntervalJob 实现；
// 砍掉 StroedIntervalJob 的 libgse/storage 持久化版本，P0 内存足够，
// P1 阶段如需"重启恢复上次执行时间"再补 file storage。
type Job interface {
	GetCheckTime() time.Time
	Init()
	Next()
	Run(e chan<- define.Event)
	GetTask() define.Task
	Reload()
	Stop()
	SetScheduler(*Daemon)
}

// NewJob 根据任务构造 Job 实例。
func NewJob(task define.Task, scheduler *Daemon) Job {
	job := &IntervalJob{task: task}
	job.SetScheduler(scheduler)
	return job
}

// IntervalJob 是固定间隔调度的 Job 实现。
type IntervalJob struct {
	ctx       context.Context
	cancel    context.CancelFunc
	scheduler *Daemon
	task      define.Task
	checkTime time.Time
}

// Init 设置初始 checkTime 为当前时刻。
func (j *IntervalJob) Init() {
	j.checkTime = time.Now()
}

// Next 根据任务 period 向前推进 checkTime。
func (j *IntervalJob) Next() {
	conf := j.task.GetConfig()
	j.checkTime = j.checkTime.Add(conf.GetPeriod())
}

// Reload 透传给嵌入的 task。
func (j *IntervalJob) Reload() {
	logging.Info("intervaljob.reload", "task_id", j.task.GetTaskID())
	j.task.Reload()
}

// Stop 取消 ctx 并停止 task。
func (j *IntervalJob) Stop() {
	logging.Info("intervaljob.stop", "task_id", j.task.GetTaskID())
	if j.cancel != nil {
		j.cancel()
	}
	if j.task != nil {
		j.task.Stop()
	}
}

// SetScheduler 绑定所属 daemon 并从其 ctx 派生子 ctx。
func (j *IntervalJob) SetScheduler(s *Daemon) {
	j.scheduler = s
	if s != nil && s.ctx != nil {
		j.ctx, j.cancel = context.WithCancel(s.ctx)
	}
}

// Run 执行封装的 task。
func (j *IntervalJob) Run(e chan<- define.Event) {
	j.task.Run(j.ctx, e)
}

// GetCheckTime 返回下次应执行时刻。
func (j *IntervalJob) GetCheckTime() time.Time { return j.checkTime }

// GetTask 返回封装的 task。
func (j *IntervalJob) GetTask() define.Task { return j.task }

// JobTimeComparator 用于按 checkTime 升序排序 Job。
func JobTimeComparator(a, b interface{}) int {
	ja, oka := a.(Job)
	jb, okb := b.(Job)
	if !oka || !okb {
		return 0
	}
	return utils.TimeComparator(ja.GetCheckTime(), jb.GetCheckTime())
}


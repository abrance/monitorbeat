// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emirpasic/gods/maps/treemap"

	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
)

// daemonState 是 Daemon 的可热替换内部状态，Reload 时整体置换以保证不阻塞调度。
type daemonState struct {
	define.BaseScheduler

	taskLock  sync.RWMutex
	tasks     *treemap.Map // key: ident, value: define.Task
	jobs      JobQueue
	jobAtomic atomic.Bool // 1 表示正在 pop+run，避免 Reload 与之交错
	ticker    *time.Ticker

	ctx        context.Context
	cancelFunc context.CancelFunc
}

func newDaemonState() *daemonState {
	return &daemonState{
		tasks: treemap.NewWithStringComparator(),
		jobs:  NewLockQueue(),
	}
}

// Stop 标记终止态。
func (s *daemonState) Stop() { s.Status = define.StatusFinished }

// Daemon 是周期性轮询任务队列的调度器实现。
//
// 对齐 bkmonitorbeat/scheduler/daemon/scheduler.go，砍掉：
//   - define.Beater 依赖（直接用 chan<- define.Event + define.Config）
//   - configs.Config 强转（用 define.Config 接口的 GetCheckInterval）
//   - utils.RecoverFor panic 处理（改用显式 deferred recover）
//   - metrics 上报（P1 阶段补）
type Daemon struct {
	*daemonState
	waitgroup sync.WaitGroup
}

// New 构造 Daemon。EventChan 与 config 由调用方提供。
func New(eventChan chan<- define.Event, config define.Config) define.Scheduler {
	state := newDaemonState()
	state.EventChan = eventChan
	state.Config = config
	return &Daemon{daemonState: state}
}

// Add 增加一个 task；若调度器已运行，同时入队对应 job。
func (s *Daemon) Add(task define.Task) {
	conf := task.GetConfig()
	s.taskLock.Lock()
	s.tasks.Put(conf.GetIdent(), task)
	s.taskLock.Unlock()
	if s.GetStatus() == define.StatusRunning {
		s.addJob(s.makeJobFromTask(task))
	}
}

func (s *Daemon) addJob(job Job) { s.jobs.Push(job) }

// Wait 阻塞至调度 goroutine 结束，并等待所有遗留 task 完成。
func (s *Daemon) Wait() {
	s.waitgroup.Wait()
	for _, job := range s.jobs.PopAll() {
		job.GetTask().Wait()
	}
	s.Status = define.StatusFinished
}

func (s *Daemon) makeJobFromTask(task define.Task) Job {
	job := NewJob(task, s)
	job.Init()
	return job
}

func (s *Daemon) reloadJobFromTask(job Job, task define.Task) Job {
	jobTask := job.GetTask()
	jobTask.SetGlobalConfig(task.GetGlobalConfig())
	jobTask.SetConfig(task.GetConfig())
	job.Reload()
	return job
}

// Start 启动调度循环。
func (s *Daemon) Start(ctx context.Context) error {
	state := s.daemonState
	state.ctx, state.cancelFunc = context.WithCancel(ctx)
	state.Status = define.StatusRunning

	state.taskLock.RLock()
	tasks := state.tasks
	jobs := make([]Job, 0, tasks.Size())
	iter := tasks.Iterator()
	for iter.Next() {
		jobs = append(jobs, s.makeJobFromTask(iter.Value().(define.Task)))
	}
	state.taskLock.RUnlock()

	s.jobs.Push(jobs...)

	checkInterval := define.DefaultCheckInterval
	if config := s.Config; config != nil {
		if interval := config.GetCheckInterval(); interval > 0 {
			checkInterval = interval
		}
	}
	state.ticker = time.NewTicker(checkInterval)

	s.waitgroup.Add(1)
	go s.run(ctx)
	return nil
}

// Count 返回当前已注册任务数。
func (s *Daemon) Count() int {
	s.taskLock.RLock()
	defer s.taskLock.RUnlock()
	return s.tasks.Size()
}

func (s *Daemon) run(ctx context.Context) {
	defer s.waitgroup.Done()
	defer func() {
		if r := recover(); r != nil {
			logging.Error("daemon scheduler crash", "panic", r)
		}
	}()

	s.Status = define.StatusRunning
	logging.Info("daemon scheduler is running")

	for s.Status == define.StatusRunning {
		ticker := s.ticker
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case now, ok := <-ticker.C:
			if !ok {
				return
			}
			state := s.daemonState
			jobsQ := state.jobs
			if !state.jobAtomic.CompareAndSwap(false, true) {
				continue
			}
			jobs := jobsQ.PopUntil(now)
			if len(jobs) == 0 {
				state.jobAtomic.Store(false)
				continue
			}
			logging.Info("daemon scheduler ready to run jobs", "count", len(jobs))
			for _, job := range jobs {
				go func(job Job) {
					defer func() {
						if r := recover(); r != nil {
							logging.Error("run task panic", "task_id", job.GetTask().GetTaskID(), "panic", r)
						}
					}()
					job.Run(state.EventChan)
				}(job)
			}
			for _, job := range jobs {
				job.Next()
			}
			jobsQ.Push(jobs...)
			state.jobAtomic.Store(false)
		}
	}

	logging.Info("daemon scheduler stop", "status", s.Status)
}

// Reload 热替换任务列表：保留同 ident 的旧 job（透传新 config），停掉已删除的旧 job。
func (s *Daemon) Reload(ctx context.Context, conf define.Config, tasks []define.Task) error {
	logging.Info("daemon.reload", "tasks", len(tasks))

	oldState := s.daemonState
	state := newDaemonState()
	state.ticker = oldState.ticker
	state.Status = oldState.Status
	state.Config = conf
	state.EventChan = oldState.EventChan
	state.ctx = oldState.ctx
	state.cancelFunc = oldState.cancelFunc

	taskMaps := treemap.NewWithStringComparator()
	for _, task := range tasks {
		ident := task.GetConfig().GetIdent()
		taskMaps.Put(ident, task)
		state.tasks.Put(ident, task)
	}

	// 等正在跑的 job queue 干完再换 job 列表
	for !oldState.jobAtomic.CompareAndSwap(false, true) {
	}
	jobs := oldState.jobs.PopAll()
	for i := 0; i < len(jobs); i++ {
		job := jobs[i]
		ident := job.GetTask().GetConfig().GetIdent()
		if task, ok := taskMaps.Get(ident); ok {
			state.jobs.Push(s.reloadJobFromTask(job, task.(define.Task)))
			taskMaps.Remove(ident)
		} else {
			job.Stop()
		}
	}
	oldState.jobAtomic.Store(false)

	iter := taskMaps.Iterator()
	for iter.Next() {
		state.jobs.Push(s.makeJobFromTask(iter.Value().(define.Task)))
	}

	logging.Info("daemon.reload.pushed_new_tasks", "count", taskMaps.Size())
	s.daemonState = state
	return nil
}

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package checker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/define"
)

// stubTask 实现最小 define.Task：Run 计数 + 发一条 event。
type stubTask struct {
	id       int32
	runs     int32
	blocking chan struct{}
	cfg      define.TaskConfig
}

func newStubTask(id int32) *stubTask {
	return &stubTask{id: id, cfg: &stubCfg{ident: "stub:" + fmt.Sprintf("%d", id)}}
}

func (s *stubTask) GetTaskID() int32                { return s.id }
func (s *stubTask) GetStatus() define.Status        { return define.StatusReady }
func (s *stubTask) SetConfig(define.TaskConfig)     {}
func (s *stubTask) GetConfig() define.TaskConfig    { return s.cfg }
func (s *stubTask) SetGlobalConfig(define.Config)   {}
func (s *stubTask) GetGlobalConfig() define.Config  { return nil }
func (s *stubTask) Reload()                          {}
func (s *stubTask) Wait()                            {}
func (s *stubTask) Stop() {
	if s.blocking != nil {
		close(s.blocking)
	}
}
func (s *stubTask) Run(ctx context.Context, e chan<- define.Event) {
	s.runs++
	if s.blocking != nil {
		// 阻塞直到 blocking chan 被关闭（Stop）或 ctx 取消。
		select {
		case <-s.blocking:
		case <-ctx.Done():
			return
		}
	}
	e <- define.NewEvent("stub", s.id)
}

type stubCfg struct {
	ident string
}

func (c *stubCfg) GetTaskID() int32            { return 1 }
func (c *stubCfg) GetIdent() string            { return c.ident }
func (c *stubCfg) SetIdent(s string)           { c.ident = s }
func (c *stubCfg) GetType() string             { return "stub" }
func (c *stubCfg) GetTimeout() time.Duration   { return time.Second }
func (c *stubCfg) GetPeriod() time.Duration    { return time.Second }
func (c *stubCfg) GetLabels() []map[string]string { return nil }
func (c *stubCfg) GetEnabled() bool            { return true }
func (c *stubCfg) Clean() error                { return nil }

// 验证：Start 后所有 task 各执行一次，事件按顺序入 chan，scheduler 自动退出。
func TestScheduler_RunAllOnce(t *testing.T) {
	ch := make(chan define.Event, 8)
	sched := New(ch, nil).(*Scheduler)

	tasks := []*stubTask{newStubTask(1), newStubTask(2), newStubTask(3)}
	for _, tk := range tasks {
		sched.Add(tk)
	}
	if c := sched.Count(); c != 3 {
		t.Fatalf("count = %d, want 3", c)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// 等三个事件到齐。
	got := make([]int32, 0, 3)
	deadline := time.After(2 * time.Second)
	for len(got) < 3 {
		select {
		case ev := <-ch:
			id, _ := ev.GetData().(int32)
			got = append(got, id)
		case <-deadline:
			t.Fatalf("only got %d events in 2s", len(got))
		}
	}

	// scheduler 应已自动 finished。
	sched.Wait()
	if s := sched.GetStatus(); s != define.StatusFinished {
		t.Fatalf("status = %v, want StatusFinished", s)
	}
	for i, tk := range tasks {
		if tk.runs != 1 {
			t.Fatalf("task[%d] runs = %d, want 1", i, tk.runs)
		}
	}
}

// 验证：无 task 时 scheduler 不 panic，正常完成。
func TestScheduler_EmptySafe(t *testing.T) {
	sched := New(make(chan define.Event, 1), nil).(*Scheduler)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	sched.Wait()
	if s := sched.GetStatus(); s != define.StatusFinished {
		t.Fatalf("status = %v, want StatusFinished", s)
	}
}

// 验证：IsDaemon == false，与 daemon scheduler 区分。
func TestScheduler_IsDaemon(t *testing.T) {
	sched := New(make(chan define.Event, 1), nil)
	if sched.IsDaemon() {
		t.Fatal("checker scheduler should not be daemon")
	}
}

// 验证：ctx 取消时 scheduler 停止运行。
func TestScheduler_CancelStops(t *testing.T) {
	ch := make(chan define.Event, 1)
	sched := New(ch, nil).(*Scheduler)
	blockingTask := newStubTask(1)
	blockingTask.blocking = make(chan struct{})
	sched.Add(blockingTask)

	ctx, cancel := context.WithCancel(context.Background())
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// 立即取消：task 应感知到 ctx.Done 并停止。
	cancel()
	done := make(chan struct{})
	go func() {
		sched.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not exit after cancel")
	}
}

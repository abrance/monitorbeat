package keyword

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/tasks"
)

type fakeCfg struct {
	configs.BaseTaskParam
	typ string
}

func (f *fakeCfg) GetType() string { return f.typ }

func (f *fakeCfg) Clean() error { return nil }

func (f *fakeCfg) GetTaskConfigListByType(string) []define.TaskConfig { return nil }

func (f *fakeCfg) GetCheckInterval() time.Duration { return define.DefaultCheckInterval }

type fakeTask struct {
	tasks.BaseTask
	cfg     *fakeCfg
	started atomic.Int32
	onRun   func(ctx context.Context, e chan<- define.Event)
	done    chan struct{}
}

func newFakeTask(typ string, onRun func(ctx context.Context, e chan<- define.Event)) *fakeTask {
	t := &fakeTask{
		cfg:   &fakeCfg{BaseTaskParam: configs.BaseTaskParam{TaskID: 1}, typ: typ},
		onRun: onRun,
		done:  make(chan struct{}),
	}
	t.SetConfig(t.cfg)
	t.SetStatus(define.StatusReady)
	return t
}

func (f *fakeTask) Run(ctx context.Context, e chan<- define.Event) {
	f.started.Add(1)
	defer close(f.done)
	f.onRun(ctx, e)
}

func TestScheduler_StartsAndStopsTask(t *testing.T) {
	ch := make(chan define.Event, 4)
	s := New(ch, &fakeCfg{}).(*Scheduler)
	s.Add(newFakeTask("keyword", func(ctx context.Context, e chan<- define.Event) {
		<-ctx.Done()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	s.Wait()
	if s.Count() != 1 {
		t.Fatalf("count = %d", s.Count())
	}
}

func TestScheduler_IsDaemon(t *testing.T) {
	s := New(make(chan define.Event), &fakeCfg{}).(*Scheduler)
	if s.IsDaemon() {
		t.Fatal("keyword scheduler should not be daemon")
	}
}

func TestScheduler_StopExitsRun(t *testing.T) {
	ch := make(chan define.Event, 1)
	s := New(ch, &fakeCfg{}).(*Scheduler)
	tk := newFakeTask("keyword", func(ctx context.Context, e chan<- define.Event) {
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
			t.Error("timeout waiting for ctx cancel")
		}
	})
	s.Add(tk)
	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if tk.started.Load() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Stop()
	select {
	case <-tk.done:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not exit after Stop")
	}
	s.Wait()
}

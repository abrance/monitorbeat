// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

// fakeTask 是测试用的最小 Task 实现：每次 Run 向 chan 发一条 event，
// 并把 runCount / lastConfig 更新到自身。
type fakeTask struct {
	id       int32
	ident    string
	period   time.Duration
	cfg      define.TaskConfig
	gcfg     define.Config
	status   define.Status
	runCount int32
	reloaded int32
}

func newFakeTask(id int32, period time.Duration) *fakeTask {
	t := &fakeTask{id: id, ident: "fake:" + itoa(id), period: period}
	t.cfg = &fakeCfg{BaseTaskParam: configs.BaseTaskParam{TaskID: id, Ident: t.ident, Period: period}}
	t.status = define.StatusReady
	return t
}

func itoa(i int32) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	pos := len(b)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func (t *fakeTask) GetTaskID() int32                { return t.id }
func (t *fakeTask) GetStatus() define.Status        { return t.status }
func (t *fakeTask) SetConfig(c define.TaskConfig)   { t.cfg = c }
func (t *fakeTask) GetConfig() define.TaskConfig    { return t.cfg }
func (t *fakeTask) SetGlobalConfig(c define.Config) { t.gcfg = c }
func (t *fakeTask) GetGlobalConfig() define.Config  { return t.gcfg }
func (t *fakeTask) Reload()                         { atomic.AddInt32(&t.reloaded, 1) }
func (t *fakeTask) Wait()                           {}
func (t *fakeTask) Stop()                           {}
func (t *fakeTask) Run(_ context.Context, e chan<- define.Event) {
	atomic.AddInt32(&t.runCount, 1)
	e <- define.NewEvent("fake", map[string]any{"id": t.id})
}

type fakeCfg struct {
	configs.BaseTaskParam
}

func (f *fakeCfg) GetType() string { return "fake" }
func (f *fakeCfg) Clean() error    { return nil }

// fakeGlobalCfg 满足 define.Config 的最小实现。
type fakeGlobalCfg struct {
	checkInterval time.Duration
}

func (g *fakeGlobalCfg) GetTaskConfigListByType(string) []define.TaskConfig { return nil }
func (g *fakeGlobalCfg) GetCheckInterval() time.Duration {
	if g.checkInterval > 0 {
		return g.checkInterval
	}
	return 50 * time.Millisecond
}
func (g *fakeGlobalCfg) Clean() error { return nil }

// 验证：Start 后 task 在 checkInterval 内被调度执行一次。
func TestDaemon_StartSchedulesTask(t *testing.T) {
	ch := make(chan define.Event, 8)
	sched := New(ch, &fakeGlobalCfg{checkInterval: 50 * time.Millisecond})
	task := newFakeTask(1, 100*time.Millisecond)
	sched.Add(task)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sched.Stop()

	select {
	case ev := <-ch:
		if ev.GetType() != "fake" {
			t.Fatalf("unexpected event type: %s", ev.GetType())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event within 2s — daemon did not schedule task")
	}
}

// 验证：daemon 按 task.Period 周期性触发，不漏次。
func TestDaemon_PeriodicNoLoss(t *testing.T) {
	ch := make(chan define.Event, 64)
	sched := New(ch, &fakeGlobalCfg{checkInterval: 20 * time.Millisecond})
	task := newFakeTask(1, 50*time.Millisecond)
	sched.Add(task)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// 收 5 个事件后停止，检查 task.runCount >= 5。
	got := 0
	deadline := time.After(3 * time.Second)
	for got < 5 {
		select {
		case <-ch:
			got++
		case <-deadline:
			t.Fatalf("only got %d events in 3s, expected 5", got)
		}
	}
	cancel()
	sched.Wait()
	if got := atomic.LoadInt32(&task.runCount); got < 5 {
		t.Fatalf("runCount = %d, want >= 5", got)
	}
}

// 验证：Reload 切换 task 列表后，新增 task 立即可被调度，旧 task 不再被调用。
func TestDaemon_ReloadSwapsTasks(t *testing.T) {
	ch := make(chan define.Event, 64)
	sched := New(ch, &fakeGlobalCfg{checkInterval: 20 * time.Millisecond})
	oldTask := newFakeTask(1, 50*time.Millisecond)
	sched.Add(oldTask)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// 等老 task 跑过至少一次。
	<-ch

	newTask := newFakeTask(2, 50*time.Millisecond)
	if err := sched.Reload(ctx, &fakeGlobalCfg{checkInterval: 20 * time.Millisecond}, []define.Task{newTask}); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// 等新 task 跑过至少一次。
	select {
	case ev := <-ch:
		id, _ := ev.GetData().(map[string]any)["id"].(int32)
		if id != 2 {
			t.Fatalf("got event from old task after reload: %+v", ev.GetData())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event after reload — new task not scheduled")
	}

	oldCount := atomic.LoadInt32(&oldTask.runCount)
	// 给老 task 至少 2 个周期的时间，确认它不再增长。
	time.Sleep(150 * time.Millisecond)
	if cur := atomic.LoadInt32(&oldTask.runCount); cur > oldCount {
		t.Fatalf("old task ran after reload: before=%d after=%d", oldCount, cur)
	}
}

// 验证：Count 反映已注册 task 数。
func TestDaemon_Count(t *testing.T) {
	sched := New(make(chan define.Event, 1), &fakeGlobalCfg{})
	if c := sched.Count(); c != 0 {
		t.Fatalf("empty daemon count = %d, want 0", c)
	}
	sched.Add(newFakeTask(1, time.Second))
	sched.Add(newFakeTask(2, time.Second))
	if c := sched.Count(); c != 2 {
		t.Fatalf("count = %d, want 2", c)
	}
}

// 验证：PopUntil + 时间排序保证 checkTime 升序，多个到期 job 全部返回。
func TestDaemon_FiresByCheckTimeOrder(t *testing.T) {
	q := NewLockQueue()
	now := time.Now()
	// 故意按"较近的过去 → 较远的过去"顺序 push，验证排序后变成"较远 → 较近"。
	t1 := &IntervalJob{checkTime: now.Add(-50 * time.Millisecond)}
	t2 := &IntervalJob{checkTime: now.Add(-100 * time.Millisecond)}
	q.Push(t1, t2)

	got := q.PopUntil(now)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// checkTime 升序：t2 (more past) 应在前。
	if got[0] != t2 {
		t.Fatal("queue not sorted by checkTime ascending")
	}
	if got[1] != t1 {
		t.Fatal("second element wrong")
	}
}

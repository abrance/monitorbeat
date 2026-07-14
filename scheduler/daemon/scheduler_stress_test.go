// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package daemon

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/define"
)

// 验证 P0.9 性能基线：100 task 并发调度，事件 0 丢失，堆内存 < 200MB。
//
// 这是 P0 验证出口标准中的"性能基线"项：
//   - 100 task 并发
//   - 事件丢失率 = 0
//   - 内存 < 200MB
func TestDaemon_Stress_100Tasks(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped in -short mode")
	}

	const (
		taskCount = 100
		period    = 50 * time.Millisecond
		runsEach  = 3 // 每 task 至少跑 3 次
	)

	ch := make(chan define.Event, taskCount*runsEach*2)
	sched := New(ch, &fakeGlobalCfg{checkInterval: 10 * time.Millisecond})

	for i := int32(1); i <= taskCount; i++ {
		sched.Add(newFakeTask(i, period))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// 收齐 taskCount * runsEach 个事件，超时即视为丢失。
	want := int32(taskCount * runsEach)
	got := int32(0)
	deadline := time.After(10 * time.Second)
	for got < want {
		select {
		case <-ch:
			got++
		case <-deadline:
			cancel()
			sched.Wait()
			t.Fatalf("event loss: got %d / want %d", got, want)
		}
	}
	cancel()
	sched.Wait()

	if got != want {
		t.Fatalf("event count drift: got %d want %d", got, want)
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	allocMB := float64(m.Alloc) / 1024 / 1024
	if allocMB > 200 {
		t.Fatalf("heap alloc = %.1f MB, want < 200 MB", allocMB)
	}
	t.Logf("P0 perf baseline OK: 100 tasks, %d events, 0 lost, heap %.1f MB", got, allocMB)
}

// 防止误引入 unused import。
var _ = define.ModuleBasereport

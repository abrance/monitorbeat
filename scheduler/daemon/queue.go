// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package daemon

import (
	"sync"
	"time"

	"github.com/emirpasic/gods/lists/doublylinkedlist"
)

// JobQueue 是带锁的 Job 双向链表队列。
type JobQueue = *LockQueue

// LockQueue 提供线程安全的 Job 入队/弹出操作。
//
// 对齐 bkmonitorbeat/scheduler/daemon/queue.go，修复原版的
// Size() 递归死循环 bug（原版 `return s.Size()` 应为读 size）。
type LockQueue struct {
	lock sync.RWMutex
	jobs *doublylinkedlist.List
}

// Push 入队并按 checkTime 升序重排。
func (q *LockQueue) Push(js ...Job) {
	q.lock.Lock()
	defer q.lock.Unlock()
	for _, j := range js {
		q.jobs.Add(j)
	}
	q.jobs.Sort(JobTimeComparator)
}

// First 返回队首元素但不弹出。
func (q *LockQueue) First() Job {
	q.lock.RLock()
	defer q.lock.RUnlock()
	el, ok := q.jobs.Get(0)
	if !ok {
		return nil
	}
	return el.(Job)
}

// Clear 清空队列。
func (q *LockQueue) Clear() {
	q.lock.Lock()
	defer q.lock.Unlock()
	q.jobs.Clear()
}

// Pop 弹出队首元素。
func (q *LockQueue) Pop() Job {
	q.lock.Lock()
	defer q.lock.Unlock()
	el, ok := q.jobs.Get(0)
	if !ok {
		return nil
	}
	q.jobs.Remove(0)
	return el.(Job)
}

// PopAll 弹出全部任务并保留原排序。
func (q *LockQueue) PopAll() []Job {
	q.lock.Lock()
	defer q.lock.Unlock()
	jobs := make([]Job, 0, q.jobs.Size())
	iter := q.jobs.Iterator()
	for iter.Next() {
		jobs = append(jobs, iter.Value().(Job))
	}
	q.jobs.Clear()
	return jobs
}

// PopUntil 弹出所有 checkTime <= now 的任务。
func (q *LockQueue) PopUntil(now time.Time) []Job {
	q.lock.Lock()
	defer q.lock.Unlock()
	jobs := make([]Job, 0)
	iter := q.jobs.Iterator()
	for iter.Next() {
		j := iter.Value().(Job)
		if j.GetCheckTime().After(now) {
			break
		}
		jobs = append(jobs, j)
	}
	for i := 0; i < len(jobs); i++ {
		q.jobs.Remove(0)
	}
	return jobs
}

// Size 返回队列长度。修复 bkmonitorbeat 原版递归死循环 bug。
func (q *LockQueue) Size() int {
	q.lock.RLock()
	defer q.lock.RUnlock()
	return q.jobs.Size()
}

// NewLockQueue 构造空队列。
func NewLockQueue() JobQueue {
	return &LockQueue{jobs: doublylinkedlist.New()}
}
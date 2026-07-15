// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package engine 是 monitorbeat 的事件分发引擎。
//
// 对照 bkmonitorbeat/beater/beater.go 的 chain 部分：
//   - 砍 libgse/output/gse 注册
//   - 砍 Beater 多租户/心跳框架
//   - 仅保留"消费 EventChan → 广播到 N 个 Output"的核心循环
package engine

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
	"github.com/abrance/monitorbeat/internal/output"
)

// Engine 持有事件 channel 与输出端列表，并内置心跳 ticker。
//
// 心跳由 Run() 内部 ticker 驱动，每 heartbeatInterval 向所有 output 广播
// 一条 heartbeat_event。heartbeatInterval=0 时禁用心跳。
type Engine struct {
	ch      chan define.Event
	outputs []output.Output
	wg      sync.WaitGroup

	version           string
	heartbeatInterval time.Duration
	startTime         time.Time
}

// New 构造 Engine。buf 为事件 channel 缓冲大小。
// version 注入到心跳事件；heartbeatInterval=0 禁用心跳。
func New(buf int, version string, heartbeatInterval time.Duration) *Engine {
	if buf <= 0 {
		buf = 1024
	}
	return &Engine{
		ch:                make(chan define.Event, buf),
		version:           version,
		heartbeatInterval: heartbeatInterval,
		startTime:         time.Now(),
	}
}

// Chan 返回事件写入端，由调度器持有并写入事件。
func (e *Engine) Chan() chan<- define.Event { return e.ch }

// AddOutput 注册一个输出端，引擎启动前调用。
func (e *Engine) AddOutput(o output.Output) {
	e.outputs = append(e.outputs, o)
}

// Run 启动事件分发循环，阻塞至 ctx 取消或 channel 关闭。
//
// 事件广播策略：同步顺序遍历所有 Output.Publish，单条事件出错只记日志
// 不影响后续输出端。
//
// 心跳：若 heartbeatInterval > 0，引擎每 heartbeatInterval 向所有 output
// 广播一条 heartbeat_event（包含 version / uptime_sec / go_version）。
func (e *Engine) Run(ctx context.Context) error {
	e.wg.Add(1)
	defer e.wg.Done()

	logging.Info("engine running", "outputs", len(e.outputs))

	var heartbeatCh <-chan time.Time
	var heartbeatTicker *time.Ticker
	if e.heartbeatInterval > 0 {
		heartbeatTicker = time.NewTicker(e.heartbeatInterval)
		defer heartbeatTicker.Stop()
		heartbeatCh = heartbeatTicker.C
	}

	for {
		select {
		case <-ctx.Done():
			logging.Info("engine stop", "reason", ctx.Err())
			return ctx.Err()
		case <-heartbeatCh:
			e.publishHeartbeat(ctx)
		case ev, ok := <-e.ch:
			if !ok {
				logging.Info("engine event chan closed")
				return nil
			}
			for _, o := range e.outputs {
				if err := o.Publish(ctx, ev); err != nil {
					logging.Error("engine output publish failed", "output", o.Name(), "err", err)
				}
			}
		}
	}
}

// publishHeartbeat 构建并广播一条 heartbeat_event。
func (e *Engine) publishHeartbeat(ctx context.Context) {
	data := map[string]any{
		"version":    e.version,
		"uptime_sec": time.Since(e.startTime).Seconds(),
		"go_version": runtime.Version(),
	}
	ev := define.NewEvent("heartbeat_event", data)
	for _, o := range e.outputs {
		if err := o.Publish(ctx, ev); err != nil {
			logging.Error("engine heartbeat publish failed", "output", o.Name(), "err", err)
		}
	}
}

// Wait 阻塞至 Run goroutine 退出。
func (e *Engine) Wait() { e.wg.Wait() }

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
	"sync"

	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
	"github.com/abrance/monitorbeat/internal/output"
)

// Engine 持有事件 channel 与输出端列表，是 P0 阶段的事件总线简化形态。
//
// P1 阶段会替换为 internal/eventbus 的多订阅者实现，但 chan define.Event
// 的对外形态保持不变，方便调度器与 task 透明接入。
type Engine struct {
	ch      chan define.Event
	outputs []output.Output
	wg      sync.WaitGroup
}

// New 构造 Engine。buf 为事件 channel 缓冲大小。
func New(buf int) *Engine {
	if buf <= 0 {
		buf = 1024
	}
	return &Engine{ch: make(chan define.Event, buf)}
}

// Chan 返回事件写入端，由调度器持有并写入事件。
func (e *Engine) Chan() chan<- define.Event { return e.ch }

// AddOutput 注册一个输出端，引擎启动前调用。
func (e *Engine) AddOutput(o output.Output) {
	e.outputs = append(e.outputs, o)
}

// Run 启动事件分发循环，阻塞至 ctx 取消或所有输出端异常。
//
// 事件广播策略：同步顺序遍历所有 Output.Publish，单条事件出错只记日志
// 不影响后续输出端。P1 阶段可改为并发 fan-out + 限速。
func (e *Engine) Run(ctx context.Context) error {
	e.wg.Add(1)
	defer e.wg.Done()

	logging.Info("engine running", "outputs", len(e.outputs))

	for {
		select {
		case <-ctx.Done():
			logging.Info("engine stop", "reason", ctx.Err())
			return ctx.Err()
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

// Wait 阻塞至 Run goroutine 退出。
func (e *Engine) Wait() { e.wg.Wait() }

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package reloader 提供 SIGUSR1 配置热重载能力。
//
// 对照 bkmonitorbeat 计划 P0.8：
//   - 接收 SIGUSR1 信号
//   - 重新从原路径读取配置文件
//   - 调用 Scheduler.Reload(ctx, newCfg, newTasks) 热替换
//
// 设计为可注入 reload 函数，避免与具体 Scheduler 类型耦合。
package reloader

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/abrance/monitorbeat/internal/logging"
)

// ReloadFunc 由调用方注入：根据新 config 构造 tasks 并调用 Scheduler.Reload。
//
// 实现应负责：读 yaml → Clean → 构造 tasks → scheduler.Reload。
// 返回的 error 仅记日志，不影响 reloader 主循环。
type ReloadFunc func(ctx context.Context) error

// Reloader 监听 SIGUSR1，收到信号时调用注入的 ReloadFunc。
type Reloader struct {
	reload ReloadFunc
	sig    os.Signal
}

// New 构造默认监听 SIGUSR1 的 reloader。reload 不能为 nil。
func New(reload ReloadFunc) *Reloader {
	return &Reloader{reload: reload, sig: syscall.SIGUSR1}
}

// NewWithSig 允许自定义信号，便于测试。
func NewWithSig(reload ReloadFunc, sig os.Signal) *Reloader {
	return &Reloader{reload: reload, sig: sig}
}

// Run 阻塞监听信号直到 ctx 取消。
//
// 每次收到信号触发一次 reload；reload 内部错误只记日志。
func (r *Reloader) Run(ctx context.Context) {
	if r.reload == nil {
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, r.sig)
	defer signal.Stop(ch)

	var wg sync.WaitGroup
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-ch:
			wg.Add(1)
			go func() {
				defer wg.Done()
				logging.Info("reloader signal received, reloading")
				if err := r.reload(ctx); err != nil {
					logging.Error("reloader reload failed", "err", err)
					return
				}
				logging.Info("reloader reload ok")
			}()
		}
	}
}

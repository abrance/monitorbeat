// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package output 提供 monitorbeat 的事件输出抽象与实现。
//
// 对照 bkmonitorbeat 的 libgse/output/gse 抽象，去掉 GSE SDK 依赖。
// 接口与 libbeatpublisher 解耦：输出端订阅 EventBus 或 chan define.Event
// 即可消费。
package output

import (
	"context"

	"github.com/abrance/monitorbeat/define"
)

// Output 是所有输出端的统一接口。
//
// 生命周期：Init 一次性初始化 → Publish 并发安全调用 → Close 收尾释放资源。
type Output interface {
	// Name 返回输出端唯一标识，用于日志与多输出路由。
	Name() string
	// Init 用配置 map 初始化输出端；可在 main.go 中调用一次。
	Init(cfg map[string]any) error
	// Publish 同步发布一个事件；阻塞调用方应自行处理超时。
	Publish(ctx context.Context, ev define.Event) error
	// Close 关闭输出端，释放底层资源（文件句柄/HTTP 连接等）。
	Close() error
}

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package logging 提供基于 log/slog 的统一日志门面。
//
// 替代 bkmonitorbeat 中依赖 pkg/utils/logger（libgse logger 包装）。
// 默认输出 JSON 到 stdout，P1 阶段可扩展 file/rotate。
package logging

import (
	"log/slog"
	"os"
	"sync"
)

var (
	defaultLogger *slog.Logger
	defaultOnce   sync.Once
)

// Default 返回全局默认 logger（JSON 输出至 stdout，Info 级别）。
func Default() *slog.Logger {
	defaultOnce.Do(func() {
		handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
		defaultLogger = slog.New(handler)
		slog.SetDefault(defaultLogger)
	})
	return defaultLogger
}

// SetLevel 切换全局日志级别。
func SetLevel(level slog.Level) {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// Debug/Info/Warn/Error 提供与 slog 一致语义的便捷封装，避免调用方引入 slog。
func Debug(msg string, args ...any) { Default().Debug(msg, args...) }
func Info(msg string, args ...any)  { Default().Info(msg, args...) }
func Warn(msg string, args ...any)  { Default().Warn(msg, args...) }
func Error(msg string, args ...any) { Default().Error(msg, args...) }

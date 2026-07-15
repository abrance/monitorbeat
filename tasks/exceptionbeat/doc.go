// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package exceptionbeat 实现 monitorbeat 的异常检测采集 task。
//
// P2 MVP：
//   - diskro：检测只读文件系统
//   - diskspace：检测磁盘空间不足
//   - corefile：检测新产生的 core dump 文件
//   - outofmem：检测 OOM killer 事件
//
// 参考 bkmonitorbeat/tasks/exceptionbeat/，砍掉 BK 协议依赖、fsnotify、telegraf。
// 四个子 collector 统一用 daemon 调度器周期性触发，每次 Run() 发一条 exceptionbeat_event。
package exceptionbeat

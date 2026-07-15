// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package socketsnapshot 实现 monitorbeat 的 Socket 连接快照采集 task。
//
// P2 MVP：
//   - 使用 gopsutil/net.Connections 采集所有 TCP/UDP 连接
//   - 一次 Run 发一条 socketsnapshot_event
//
// 参考 bkmonitorbeat/tasks/socketsnapshot/，砍掉 netlink detector、procsnapshot 缓存。
package socketsnapshot

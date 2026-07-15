// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package processbeat 实现 monitorbeat 的进程性能采集 task。
//
// P2 MVP：
//   - 使用 gopsutil/process 采集所有进程信息
//   - 支持 process_names 精确名过滤和 process_regex 正则过滤
//   - 采集 CPU%、内存%、RSS、VMS、FD 数、线程数
//   - 按 CPU 排序取 top_n 个进程，发一条 processbeat_event
//
// 参考 bkmonitorbeat/tasks/processbeat/，砍掉 CMDB 配置、端口检测、netlink、PID 映射。
package processbeat

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package script 实现 monitorbeat 的脚本采集 task。
//
// P1.3 MVP：
//   - 定期执行 shell 命令，解析 stdout（prometheus / custom 格式）
//   - 每次 Run 发一条 script_event，包含所有解析出的 metrics + labels
//   - 使用 daemon 时间堆调度（周期性触发），非长驻调度器
package script

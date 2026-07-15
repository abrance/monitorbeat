// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

// Package file 实现单文件 append-only tail harvester。
//
// P1.2 MVP 限定：
//   - 单文件路径
//   - 同步阻塞 ReadLine API（ctx 可中断）
//   - 编码仅 utf-8 / gb18030
//   - 不做 offset 持久化、不做轮转检测（每次启动按 FromBegin 决定从 0 或 EOF 开始）
//
// 不依赖 libgse。
package file

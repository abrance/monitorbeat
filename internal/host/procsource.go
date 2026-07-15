// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package procsource 抽象进程采集源的标识数据来源。
//
// P0 阶段仅提供 interface 与三个 stub 实现，由 P2 阶段的 procconf/procsync/proccustom
// 填实，替代 bkmonitorbeat 中硬编码 /var/lib/gse/host/hostid 的逻辑。
package procsource

import (
	"context"
	"errors"
)

// Info 主机标识信息。P0 起点只关注 HostID；其他字段 P2 填实。
type Info struct {
	HostID  string
	CloudID string
	InnerIP string
}

// Source 抽象"主机身份与进程配置来源"。
//
// 设计动因：bkmonitorbeat 把 /var/lib/gse/host/hostid 路径硬编码到
// procconf/procsync/proccustom 三个 task 里，导致无法在非 BK 环境复用。
// 本接口让身份信息来源可插拔：FileSource 读本地文件，HTTPSource 拉远端 API，
// StaticSource 从内存注入，方便测试与容器化部署。
type Source interface {
	// Start 初始化源并准备读取。
	Start(ctx context.Context) error
	// Stop 关闭源、释放文件/网络资源。
	Stop()
	// Reload 触发同步刷新（不阻塞），返回是否成功。
	Reload() error
	// GetHostID 返回最近一次刷新得到的主机标识。空字符串视为未知。
	GetHostID() string
	// GetInfo 返回完整 Identity 快照。
	GetInfo() (Info, error)
	// Notify 返回源发生变化时通知的 channel；不需要时返回 nil。
	Notify() <-chan struct{}
}

// ErrNotImplemented 表示当前实现尚未支持该操作（P0 stub 的默认返回）。
var ErrNotImplemented = errors.New("procsource: not implemented in P0 stub")

// --- FileSource: 读本地身份文件，对应原 hostid 解析 ---

// FileSource 从本地文件读取主机身份；P0 阶段 stub，P2 填实解析逻辑。
type FileSource struct {
	path string
	info Info
}

// NewFileSource 构造 FileSource。path 为身份文件路径，例如 /var/lib/gse/host/hostid。
func NewFileSource(path string) *FileSource {
	return &FileSource{path: path}
}

func (s *FileSource) Start(_ context.Context) error { return ErrNotImplemented }
func (s *FileSource) Stop()                         {}
func (s *FileSource) Reload() error                 { return ErrNotImplemented }
func (s *FileSource) GetHostID() string             { return s.info.HostID }
func (s *FileSource) GetInfo() (Info, error)        { return s.info, ErrNotImplemented }
func (s *FileSource) Notify() <-chan struct{}       { return nil }

// --- HTTPSource: 拉远端 API 获取身份 ---

// HTTPSource 从远端 API 拉取身份信息；P0 stub。
type HTTPSource struct {
	endpoint string
	info     Info
}

// NewHTTPSource 构造 HTTPSource。endpoint 为身份 API URL。
func NewHTTPSource(endpoint string) *HTTPSource {
	return &HTTPSource{endpoint: endpoint}
}

func (s *HTTPSource) Start(_ context.Context) error { return ErrNotImplemented }
func (s *HTTPSource) Stop()                         {}
func (s *HTTPSource) Reload() error                 { return ErrNotImplemented }
func (s *HTTPSource) GetHostID() string             { return s.info.HostID }
func (s *HTTPSource) GetInfo() (Info, error)        { return s.info, ErrNotImplemented }
func (s *HTTPSource) Notify() <-chan struct{}       { return nil }

// --- StaticSource: 内存注入，便于测试 ---

// StaticSource 从内存直接返回身份信息；P0 全功能 stub，方便测试与无 BK 环境单测路径。
type StaticSource struct {
	info  Info
	notif chan struct{}
}

// NewStaticSource 构造 StaticSource。P0 场景下默认填空字符串，留 P2 注入真实数据。
func NewStaticSource(info Info) *StaticSource {
	return &StaticSource{info: info, notif: make(chan struct{}, 1)}
}

func (s *StaticSource) Start(_ context.Context) error { return nil }
func (s *StaticSource) Stop()                         {}
func (s *StaticSource) Reload() error                 { return nil }
func (s *StaticSource) GetHostID() string             { return s.info.HostID }
func (s *StaticSource) GetInfo() (Info, error)        { return s.info, nil }
func (s *StaticSource) Notify() <-chan struct{}       { return s.notif }

// SetInfo 显式更新静态源的身份，触发 Notify。
func (s *StaticSource) SetInfo(info Info) {
	s.info = info
	select {
	case s.notif <- struct{}{}:
	default:
	}
}

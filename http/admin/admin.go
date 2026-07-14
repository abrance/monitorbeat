// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package admin 提供 monitorbeat 的 pprof / 健康检查 HTTP 端点。
//
// 对照 bkmonitorbeat 计划 P0.8 / P0.9：
//   - 默认监听 0.0.0.0:56060
//   - /debug/pprof/  暴露标准 pprof 端点
//   - /healthz      返回版本与运行模式
//
// 不引入额外 router 库，直接用 net/http + 标准库 pprof。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/pprof"
	"sync"
	"time"

	"github.com/abrance/monitorbeat/internal/logging"
)

// Server 是 admin HTTP 服务器。
type Server struct {
	addr    string
	version string
	server  *http.Server
	ln      net.Listener
	wg      sync.WaitGroup
}

// New 构造 admin server。addr 形如 "0.0.0.0:56060"；version 写入 /healthz 响应。
func New(addr, version string) *Server {
	s := &Server{
		addr:    addr,
		version: version,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	// 直接挂标准 pprof handlers。
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Start 在后台 goroutine 启动 HTTP 监听；端口已被占用则返回错误。
func (s *Server) Start(_ context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.ln = ln
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		logging.Info("admin server listening", "addr", s.addr)
		if err := s.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logging.Error("admin server exit", "err", err)
		}
	}()
	return nil
}

// Stop 优雅关闭 admin server。
func (s *Server) Stop() {
	if s.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		logging.Error("admin server shutdown", "err", err)
	}
	s.wg.Wait()
}

// handleHealth 返回版本号与启动时间，便于 K8s liveness/readiness probe。
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version":   s.version,
		"status":    "ok",
		"timestamp": time.Now(),
	})
}

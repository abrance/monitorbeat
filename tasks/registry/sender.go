package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/tasks"
)

// Sender 定期向 monitorweb registry 上报 agent 心跳。
type Sender struct {
	cfg    configs.RegistryConfig
	client *http.Client
	info   AgentInfo
	stopCh chan struct{}
}

// New 构造心跳发送器。
// version 由主程序注入（-ldflags）。
// taskTypesFn 返回当前已注册的 task type 列表（用于 tasks.RegisteredTypes）。
func New(cfg configs.RegistryConfig, version string, taskTypesFn func() []string) *Sender {
	info := AgentInfo{
		Hostname:  tasks.Hostname(),
		Version:   version,
		Tasks:     taskTypesFn(),
		IP:        resolveOutboundIP(),
		K8sNode:   os.Getenv("K8S_NODE"),
		StartTime: time.Now().Unix(),
	}
	return &Sender{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		info:   info,
		stopCh: make(chan struct{}),
	}
}

// Run 启动心跳上报循环。启动时立即上报一次，之后按配置间隔上报。
// ctx 取消或调用 Stop 时退出。
func (s *Sender) Run(ctx context.Context) {
	if s.cfg.URL == "" {
		return
	}
	// 启动时立即上报一次
	s.send(ctx)
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.send(ctx)
		}
	}
}

// Stop 停止心跳上报循环。
func (s *Sender) Stop() {
	close(s.stopCh)
}

func (s *Sender) send(ctx context.Context) {
	body, err := json.Marshal(s.info)
	if err != nil {
		slog.Warn("registry: marshal heartbeat failed", "err", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.URL, bytes.NewReader(body))
	if err != nil {
		slog.Warn("registry: create request failed", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		slog.Warn("registry: heartbeat failed", "err", err)
		return
	}
	resp.Body.Close()
}

// resolveOutboundIP 获取本机出口 IP（用于连接外部时的源地址）。
func resolveOutboundIP() string {
	conn, err := net.DialTimeout("udp", "8.8.8.8:80", 3*time.Second)
	if err != nil {
		return ""
	}
	defer conn.Close()
	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	return localAddr.IP.String()
}

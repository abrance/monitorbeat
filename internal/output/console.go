// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package output

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
)

// Console 把事件以 JSON 行格式写到 stdout。
//
// 用于本地调试与 demo：每条 event 输出一行 JSON：
//
//	{"timestamp":"...","type":"basereport","data":{"cpu_usage":12.3}}
type Console struct {
	mu  sync.Mutex
	out *os.File
}

// NewConsole 构造默认写到 os.Stdout 的 Console 输出端。
func NewConsole() *Console {
	return &Console{out: os.Stdout}
}

// Name 返回输出端名称。
func (c *Console) Name() string { return "console" }

// Init 校验/初始化；当前无配置项，预留 P1 阶段 pretty/level 等开关。
func (c *Console) Init(_ map[string]any) error {
	if c.out == nil {
		c.out = os.Stdout
	}
	return nil
}

// Publish 把 event 序列化为 JSON 行写到 stdout。
//
// 单条写入加锁以避免多 goroutine 交错；JSON marshal 已 escape 控制字符。
func (c *Console) Publish(_ context.Context, ev define.Event) error {
	payload := map[string]any{
		"timestamp": ev.GetTimestamp(),
		"type":      ev.GetType(),
		"data":      ev.GetData(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("console marshal: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.out.Write(append(b, '\n')); err != nil {
		logging.Error("console.write failed", "err", err)
		return err
	}
	return nil
}

// Close 关闭输出端。stdout 由系统接管，不主动关闭。
func (c *Console) Close() error { return nil }

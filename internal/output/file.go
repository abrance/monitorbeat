// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package output

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
)

// defaultFileMode 是输出文件的权限。
const defaultFileMode = 0o644

// defaultMaxSizeMB 是单个文件最大体积，超过后滚动。
const defaultMaxSizeMB = 100

// File 把事件以 JSON 行格式写到本地文件，达到 max_size_mb 后滚动。
//
// 对照 bkmonitorbeat 计划 P0.5：
//   - 路径与最大体积可配置
//   - 滚动策略：当前文件改名为 <path>.1，新建 <path> 继续写
//   - 单一代替 lumberjack 依赖；P1 如需更复杂轮转再引入
type File struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	f        *os.File
	written  int64 // 当前文件已写字节
}

// NewFile 构造 File 输出端；路径必须非空。
func NewFile(path string) *File {
	return &File{path: path, maxBytes: int64(defaultMaxSizeMB) * 1024 * 1024}
}

// Name 返回输出端标识。
func (f *File) Name() string { return "file" }

// Init 解析配置 map 并打开/创建目标文件。
//
// 支持 cfg 字段：
//   - path      string  文件路径（必填）
//   - max_size_mb int    单文件最大 MB，超过后滚动（默认 100）
func (f *File) Init(cfg map[string]any) error {
	if cfg != nil {
		if v, ok := cfg["path"]; ok {
			if p, ok := v.(string); ok && p != "" {
				f.path = p
			}
		}
		if v, ok := cfg["max_size_mb"]; ok {
			switch n := v.(type) {
			case int:
				if n > 0 {
					f.maxBytes = int64(n) * 1024 * 1024
				}
			case int64:
				if n > 0 {
					f.maxBytes = n * 1024 * 1024
				}
			case float64:
				if n > 0 {
					f.maxBytes = int64(n) * 1024 * 1024
				}
			}
		}
	}
	if f.path == "" {
		return errors.New("file output: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return fmt.Errorf("file output mkdir: %w", err)
	}
	return f.open()
}

func (f *File) open() error {
	file, err := os.OpenFile(f.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, defaultFileMode)
	if err != nil {
		return fmt.Errorf("file output open %s: %w", f.path, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("file output stat: %w", err)
	}
	f.f = file
	f.written = info.Size()
	return nil
}

// rotate 把当前文件改名为 <path>.1 并新建同名文件。
func (f *File) rotate() error {
	if f.f != nil {
		if err := f.f.Close(); err != nil {
			logging.Error("file output close before rotate", "err", err)
		}
	}
	backup := f.path + ".1"
	// 重命名失败不致命，下次 Publish 会再尝试；这里只记日志。
	if err := os.Rename(f.path, backup); err != nil && !os.IsNotExist(err) {
		logging.Error("file output rotate rename", "err", err)
	}
	return f.open()
}

// Publish 写一行 JSON 到文件；写入后若超过 maxBytes 触发滚动。
func (f *File) Publish(_ context.Context, ev define.Event) error {
	payload := map[string]any{
		"timestamp": ev.GetTimestamp(),
		"type":      ev.GetType(),
		"data":      ev.GetData(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("file output marshal: %w", err)
	}
	b = append(b, '\n')

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.f == nil {
		if err := f.open(); err != nil {
			return err
		}
	}

	n, err := f.f.Write(b)
	if err != nil {
		return fmt.Errorf("file output write: %w", err)
	}
	f.written += int64(n)

	if f.written >= f.maxBytes {
		if err := f.rotate(); err != nil {
			logging.Error("file output rotate failed", "err", err)
		}
	}
	return nil
}

// Close 关闭当前打开的文件句柄。
func (f *File) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.f == nil {
		return nil
	}
	err := f.f.Close()
	f.f = nil
	return err
}

// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

// Package keyword 实现 monitorbeat 的日志关键字采集 task。
//
// P1.2 MVP（raw_log only）：
//   - 单文件 tail + regex capture
//   - 每命中一行发一条 raw_log 事件（matches_count=1）
//
// P2 enhancement:
//   - offset_registry 持久化读取偏移，重启断点续读
//   - fsnotify 替代轮询，降低采集延迟
//
// 行为对齐 bkmonitorbeat/tasks/keyword/raw_log 分支，差异：
//   - 去 libgse，事件走 internal/output（已通过 define.Event 通道）
//   - 用 scheduler/keyword 长驻调度器（不是 daemon 时间堆）
package keyword

import (
	"context"
	"fmt"
	"regexp"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/input/file"
	"github.com/abrance/monitorbeat/tasks"
)

func init() {
	tasks.RegisterBuilder(define.ModuleKeyword, func(tc define.TaskConfig) (define.Task, error) {
		return builder(tc)
	})
}

func builder(tc define.TaskConfig) (define.Task, error) {
	cfg, ok := tc.(*configs.KeywordConfig)
	if !ok {
		return nil, fmt.Errorf("keyword: config type mismatch: %T", tc)
	}
	if cfg.File == "" {
		return nil, fmt.Errorf("keyword: file is required")
	}
	if cfg.Pattern == "" {
		return nil, fmt.Errorf("keyword: pattern is required")
	}
	if _, err := regexp.Compile(cfg.Pattern); err != nil {
		return nil, fmt.Errorf("keyword: bad pattern: %w", err)
	}
	return New(cfg), nil
}

// Gather 是 keyword task 的运行时实例。
type Gather struct {
	tasks.BaseTask
	cfg      *configs.KeywordConfig
	registry *file.OffsetRegistry
}

// New 构造可运行的 keyword task。
func New(cfg *configs.KeywordConfig) define.Task {
	g := &Gather{cfg: cfg}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

// Run 阻塞读文件 → 命中正则 → 发 raw_log 事件；ctx 取消即退出。
func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	re := regexp.MustCompile(g.cfg.Pattern)

	var registry *file.OffsetRegistry
	if g.cfg.OffsetRegistry != "" {
		var err error
		registry, err = file.NewOffsetRegistry(g.cfg.OffsetRegistry)
		if err != nil {
			g.emitError(ctx, e, fmt.Errorf("registry: %w", err))
			return
		}
		g.registry = registry
	}

	hc := file.HarvesterConfig{
		File:       g.cfg.File,
		Encoding:   g.cfg.Encoding,
		FromBegin:  g.cfg.FromBegin == nil || *g.cfg.FromBegin,
		BufferSize: g.cfg.BufferSize,
		Registry:   registry,
	}
	h, err := file.New(hc)
	if err != nil {
		g.emitError(ctx, e, err)
		return
	}
	defer func() {
		h.Close()
		if registry != nil {
			registry.Save()
		}
	}()

	lineNo := 0
	for {
		line, err := h.ReadLine(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			return
		}
		lineNo++
		captures, ok := ExtractLine(line, re)
		if !ok {
			continue
		}
		ev := BuildRawLogEvent(g.cfg.File, g.cfg.Pattern, lineNo, captures, line)
		select {
		case e <- ev:
		case <-ctx.Done():
			return
		}
	}
}

func (g *Gather) emitError(ctx context.Context, e chan<- define.Event, cause error) {
	ev := define.NewEvent(RawLogEventType, map[string]any{
		"dimensions": map[string]string{
			"file":     g.cfg.File,
			"regex":    g.cfg.Pattern,
			"hostname": tasks.Hostname(),
		},
		"metrics": map[string]float64{"matches_count": 0},
		"fields":  map[string]string{},
		"raw":     "",
		"error":   cause.Error(),
	})
	select {
	case e <- ev:
	case <-ctx.Done():
	}
}

// ExtractLine 对行执行正则匹配，返回命名/匿名 capture 组。
func ExtractLine(line string, re *regexp.Regexp) (map[string]string, bool) {
	m := re.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}
	names := re.SubexpNames()
	allUnnamed := true
	for _, name := range names {
		if name != "" {
			allUnnamed = false
			break
		}
	}
	out := make(map[string]string, len(m))
	for i, name := range names {
		if i == 0 {
			continue
		}
		if !allUnnamed && name != "" {
			out[name] = m[i]
			continue
		}
		if allUnnamed || name == "" {
			out[fmt.Sprintf("%d", i)] = m[i]
		}
	}
	return out, true
}

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package script

import (
	"context"
	"fmt"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	execrunner "github.com/abrance/monitorbeat/internal/script/exec"
	"github.com/abrance/monitorbeat/internal/script/parse"
	"github.com/abrance/monitorbeat/tasks"
)

const ScriptEventType = "script_event"

func init() {
	tasks.RegisterBuilder(define.ModuleScript, func(tc define.TaskConfig) (define.Task, error) {
		return builder(tc)
	})
}

func builder(tc define.TaskConfig) (define.Task, error) {
	cfg, ok := tc.(*configs.ScriptConfig)
	if !ok {
		return nil, fmt.Errorf("script: config type mismatch: %T", tc)
	}
	if cfg.Command == "" {
		return nil, fmt.Errorf("script: command is required")
	}
	return New(cfg), nil
}

// Gather 是 script task 的运行时实例。
type Gather struct {
	tasks.BaseTask
	cfg *configs.ScriptConfig
}

// New 构造可运行的 script task。
func New(cfg *configs.ScriptConfig) define.Task {
	g := &Gather{cfg: cfg}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

// Run 执行脚本 → 解析输出 → 发 script_event。
func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	start := time.Now()

	// step 1: execute
	timeout := g.cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cmdCtx, cmdCancel := context.WithTimeout(ctx, timeout)
	defer cmdCancel()

	stdout, execErr := execrunner.Run(cmdCtx, g.cfg.Command, g.cfg.UserEnvs)

	costMs := float64(time.Since(start).Milliseconds())

	// step 2: parse
	metrics, labels, parseErr := parse.Parse(g.cfg.Format, stdout)

	// step 3: build event
	metrics["cost_ms"] = costMs

	dims := map[string]string{
		"command": g.cfg.Command,
		"task_id": fmt.Sprintf("%d", g.cfg.TaskID),
	}
	for k, v := range labels {
		dims[k] = v
	}

	data := map[string]any{
		"dimensions": dims,
		"metrics":    metrics,
	}

	if execErr != nil {
		data["error"] = execErr.Error()
	}
	if parseErr != nil {
		data["parse_error"] = parseErr.Error()
	}

	select {
	case e <- define.NewEvent(ScriptEventType, data):
	case <-ctx.Done():
	}
}

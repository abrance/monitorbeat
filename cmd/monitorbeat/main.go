// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package main 是 monitorbeat 的命令行入口。
//
// 对照 bkmonitorbeat/cmd/bkmonitorbeat/main.go + beater/beater.go：
//   - 砍 libbeat cmd 骨架与 Beater 生命周期框架
//   - 流程：解析 YAML → Clean → 建 Engine → 注册输出端 → 建 Daemon →
//     factory.Build 注册 task → daemon.Start → admin server → reloader →
//     等 ctx
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/http/admin"
	"github.com/abrance/monitorbeat/internal/engine"
	"github.com/abrance/monitorbeat/internal/logging"
	"github.com/abrance/monitorbeat/internal/output"
	"github.com/abrance/monitorbeat/internal/reloader"
	"github.com/abrance/monitorbeat/scheduler/daemon"
	"github.com/abrance/monitorbeat/scheduler/keyword"
	"github.com/abrance/monitorbeat/tasks"

	// 副作用导入：触发 init() 把 builder 注册到 tasks.factory。
	_ "github.com/abrance/monitorbeat/tasks/basereport"
	_ "github.com/abrance/monitorbeat/tasks/dmesg"
	_ "github.com/abrance/monitorbeat/tasks/exceptionbeat"
	_ "github.com/abrance/monitorbeat/tasks/gatherupbeat"
	_ "github.com/abrance/monitorbeat/tasks/http"
	_ "github.com/abrance/monitorbeat/tasks/keyword"
	_ "github.com/abrance/monitorbeat/tasks/metricbeat"
	_ "github.com/abrance/monitorbeat/tasks/ping"
	_ "github.com/abrance/monitorbeat/tasks/processbeat"
	_ "github.com/abrance/monitorbeat/tasks/script"
	_ "github.com/abrance/monitorbeat/tasks/selfstats"
	_ "github.com/abrance/monitorbeat/tasks/socketsnapshot"
	_ "github.com/abrance/monitorbeat/tasks/tcp"
	_ "github.com/abrance/monitorbeat/tasks/udp"
)

// version 在构建时由 -ldflags 注入；默认 dev。
var version = "dev"

func main() {
	var (
		configPath string
		checkOnly  bool
	)
	flag.StringVar(&configPath, "config", "configs/minimal.yaml", "config file path")
	flag.BoolVar(&checkOnly, "check", false, "validate config and exit")
	flag.Parse()

	cfg, err := loadConfig(configPath)
	if err != nil {
		slog.Error("load config failed", "err", err, "path", configPath)
		os.Exit(1)
	}
	cfg.ConfigPath = configPath
	if err := cfg.Clean(); err != nil {
		slog.Error("clean config failed", "err", err)
		os.Exit(1)
	}

	if checkOnly {
		fmt.Println("config OK")
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	eng := engine.New(cfg.GetEventBufferSize(), version, 60*time.Second)
	if err := wireOutputs(eng, cfg.Outputs); err != nil {
		slog.Error("wire outputs failed", "err", err)
		os.Exit(1)
	}
	go func() {
		if err := eng.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("engine exited with error", "err", err)
		}
	}()

	var adminSrv *admin.Server
	if cfg.AdminAddr != "" {
		adminSrv = admin.New(cfg.AdminAddr, version)
		if err := adminSrv.Start(ctx); err != nil {
			slog.Error("admin server start failed", "err", err)
		}
	}

	sched := daemon.New(eng.Chan(), cfg)
	keywordSched := keyword.New(eng.Chan(), cfg)
	tasksList, skipped := buildAllTasks(cfg)
	for _, t := range tasksList {
		typ := t.GetConfig().GetType()
		switch typ {
		case define.ModuleKeyword:
			keywordSched.Add(t)
			logging.Info("task registered", "type", typ, "ident", t.GetConfig().GetIdent(), "scheduler", "keyword")
		default:
			sched.Add(t)
			logging.Info("task registered", "type", typ, "ident", t.GetConfig().GetIdent(), "scheduler", "daemon")
		}
	}
	for _, typ := range skipped {
		slog.Warn("task skipped", "type", typ)
	}

	if err := sched.Start(ctx); err != nil {
		slog.Error("daemon scheduler start failed", "err", err)
		os.Exit(1)
	}
	if err := keywordSched.Start(ctx); err != nil {
		slog.Error("keyword scheduler start failed", "err", err)
		os.Exit(1)
	}

	rld := reloader.New(func(_ context.Context) error {
		newCfg, err := loadConfig(cfg.ConfigPath)
		if err != nil {
			return fmt.Errorf("reload load: %w", err)
		}
		newCfg.ConfigPath = cfg.ConfigPath
		if err := newCfg.Clean(); err != nil {
			return fmt.Errorf("reload clean: %w", err)
		}
		tasksReloaded, _ := buildAllTasks(newCfg)
		return sched.Reload(ctx, newCfg, tasksReloaded)
	})
	go rld.Run(ctx)

	logging.Info("monitorbeat started", "version", version, "tasks", sched.Count(), "admin", cfg.AdminAddr)

	<-ctx.Done()
	logging.Info("monitorbeat shutting down")
	keywordSched.Stop()
	sched.Stop()
	keywordSched.Wait()
	sched.Wait()
	if adminSrv != nil {
		adminSrv.Stop()
	}
	eng.Wait()
}

func loadConfig(path string) (*configs.Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	cfg := &configs.Config{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}
	return cfg, nil
}

// buildAllTasks 通过 tasks.factory 构造所有 task。
//
// 返回构造成功与被跳过的 type 列表，便于 main 记日志。
func buildAllTasks(cfg *configs.Config) ([]define.Task, []string) {
	var (
		out     []define.Task
		skipped []string
	)
	for _, tc := range cfg.AllTaskConfigs() {
		t, err := tasks.Build(tc)
		if err != nil {
			skipped = append(skipped, tc.GetType())
			continue
		}
		out = append(out, t)
	}
	return out, skipped
}

// wireOutputs 按 cfg.Outputs 顺序构造输出端并注册到 engine。
//
// 未知 type 记 warning 但不中断，便于渐进式引入新输出端。
func wireOutputs(eng *engine.Engine, outs []configs.OutputConfig) error {
	for _, oc := range outs {
		o, err := buildOutput(oc)
		if err != nil {
			slog.Warn("skip unknown output", "type", oc.Type, "err", err)
			continue
		}
		if err := o.Init(oc.Params); err != nil {
			return fmt.Errorf("init output %s: %w", oc.Type, err)
		}
		eng.AddOutput(o)
		logging.Info("output registered", "type", oc.Type)
	}
	return nil
}

func buildOutput(oc configs.OutputConfig) (output.Output, error) {
	switch oc.Type {
	case "console":
		return output.NewConsole(), nil
	case "file":
		return output.NewFile(""), nil
	case "http":
		cfg, err := decodeHTTPOutputConfig(oc.Params)
		if err != nil {
			return nil, err
		}
		return output.NewHTTPOutput(cfg), nil
	default:
		return nil, fmt.Errorf("unknown output type: %s", oc.Type)
	}
}

// decodeHTTPOutputConfig converts the inline params map into a typed
// HTTPOutputConfig, parsing any time.Duration fields (e.g. `timeout: 3s`)
// from string form, since json round-trip can't decode "3s" into time.Duration.
func decodeHTTPOutputConfig(params map[string]any) (configs.HTTPOutputConfig, error) {
	if v, ok := params["timeout"]; ok {
		if s, ok := v.(string); ok {
			d, err := time.ParseDuration(s)
			if err != nil {
				return configs.HTTPOutputConfig{}, fmt.Errorf("http output: invalid timeout %q: %w", s, err)
			}
			params["timeout"] = d
		}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return configs.HTTPOutputConfig{}, fmt.Errorf("http output: marshal cfg: %w", err)
	}
	var c configs.HTTPOutputConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return configs.HTTPOutputConfig{}, fmt.Errorf("http output: decode cfg: %w", err)
	}
	if err := c.Clean(); err != nil {
		return configs.HTTPOutputConfig{}, fmt.Errorf("http output: %w", err)
	}
	return c, nil
}

// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package configs 提供全局配置承载与任务配置分组能力。
//
// 对照 bkmonitorbeat/configs/config.go：
//   - 砍 Mode/GetGatherUpDataID 等蓝鲸概念
//   - 任务配置以"按 type 分组的 struct 切片"形式存储，方便 YAML 直配
//   - GetTaskConfigListByType 用于 scheduler 按 type 检索
package configs

import (
	"time"

	"github.com/abrance/monitorbeat/define"
)

// Config 是 monitorbeat 全局配置的根结构，实现 define.Config。
type Config struct {
	// CheckInterval 调度器轮询任务队列的间隔；0 表示用 define.DefaultCheckInterval。
	CheckInterval time.Duration `yaml:"check_interval"`

	// EventBufferSize 事件 channel 缓冲大小；0 表示默认 1024。
	EventBufferSize int `yaml:"event_buffer_size"`

	// AdminAddr pprof / healthz HTTP 监听地址；空表示不启用 admin server。
	AdminAddr string `yaml:"admin_addr"`

	// ConfigPath 配置文件路径，由 main.go 注入；reloader 用它做 SIGUSR1 重载。
	ConfigPath string `yaml:"-"`

	// Outputs 输出端配置列表（按声明顺序初始化）。
	Outputs []OutputConfig `yaml:"outputs"`

	// Basereports 是 basereport 任务配置列表。
	// P1 阶段在此并行添加 PingConfigs/TCPConfigs/HTTPConfigs/KeywordConfigs/ScriptConfigs。
	Basereports []BasereportConfig `yaml:"basereports"`
	Pings       []PingConfig       `yaml:"pings"`
	TCPs        []TCPConfig        `yaml:"tcps"`
	UDPs        []UDPConfig        `yaml:"udps"`
	HTTPs       []HTTPConfig       `yaml:"https"`
	Keywords    []KeywordConfig    `yaml:"keywords"`
}

// OutputConfig 是单个输出端的配置，type 决定具体实现。
type OutputConfig struct {
	Type   string         `yaml:"type"`
	Params map[string]any `yaml:",inline"`
}

// GetCheckInterval 返回调度器轮询间隔，未配置时取默认值。
func (c *Config) GetCheckInterval() time.Duration {
	if c.CheckInterval > 0 {
		return c.CheckInterval
	}
	return define.DefaultCheckInterval
}

// GetEventBufferSize 返回事件 channel 缓冲大小，未配置时取默认 1024。
func (c *Config) GetEventBufferSize() int {
	if c.EventBufferSize > 0 {
		return c.EventBufferSize
	}
	return 1024
}

// GetTaskConfigListByType 按 task type 过滤返回任务配置列表。
//
// 实现方式：switch case 分发到各 type 的内嵌切片。新增 task 类型时
// 在此处添加一个 case 即可。
func (c *Config) GetTaskConfigListByType(typ string) []define.TaskConfig {
	var out []define.TaskConfig
	switch typ {
	case define.ModuleBasereport:
		for i := range c.Basereports {
			out = append(out, &c.Basereports[i])
		}
	case define.ModulePing:
		for i := range c.Pings {
			out = append(out, &c.Pings[i])
		}
	case define.ModuleTCP:
		for i := range c.TCPs {
			out = append(out, &c.TCPs[i])
		}
	case define.ModuleUDP:
		for i := range c.UDPs {
			out = append(out, &c.UDPs[i])
		}
	case define.ModuleHTTP:
		for i := range c.HTTPs {
			out = append(out, &c.HTTPs[i])
		}
	case define.ModuleKeyword:
		for i := range c.Keywords {
			out = append(out, &c.Keywords[i])
		}
	}
	return out
}

// AllTaskConfigs 返回所有已注册任务配置（按 type 拼接，顺序与 switch 一致）。
//
// 给 main.go / engine 启动时遍历构造 task 用，避免调用方枚举 type。
func (c *Config) AllTaskConfigs() []define.TaskConfig {
	var out []define.TaskConfig
	for i := range c.Basereports {
		out = append(out, &c.Basereports[i])
	}
	for i := range c.Pings {
		out = append(out, &c.Pings[i])
	}
	for i := range c.TCPs {
		out = append(out, &c.TCPs[i])
	}
	for i := range c.UDPs {
		out = append(out, &c.UDPs[i])
	}
	for i := range c.HTTPs {
		out = append(out, &c.HTTPs[i])
	}
	for i := range c.Keywords {
		out = append(out, &c.Keywords[i])
	}
	return out
}

// Clean 实现 define.Config.Clean，调用所有任务配置的 Clean。
//
// 在配置加载后、调度器启动前调用一次，确保所有 Ident/TaskID 已填充默认值。
func (c *Config) Clean() error {
	for i := range c.Basereports {
		if err := c.Basereports[i].Clean(); err != nil {
			return err
		}
	}
	for i := range c.Pings {
		if err := c.Pings[i].Clean(); err != nil {
			return err
		}
	}
	for i := range c.TCPs {
		if err := c.TCPs[i].Clean(); err != nil {
			return err
		}
	}
	for i := range c.UDPs {
		if err := c.UDPs[i].Clean(); err != nil {
			return err
		}
	}
	for i := range c.HTTPs {
		if err := c.HTTPs[i].Clean(); err != nil {
			return err
		}
	}
	for i := range c.Keywords {
		if err := c.Keywords[i].Clean(); err != nil {
			return err
		}
	}
	return nil
}

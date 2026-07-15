// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import (
	"time"

	"github.com/abrance/monitorbeat/define"
)

const (
	defaultScriptFormat  = "prometheus"
	defaultScriptTimeout = 30 * time.Second
)

// ScriptConfig 控制脚本采集任务。
//
// P1.3 MVP 限定：
//   - 单行 command，通过 sh -c 执行
//   - Format: "prometheus"（使用 expfmt）或 "custom"（key=value 逐行）
//   - 不做 timestamp/offset 解析（用当前时间）
//   - 不做 KeepOneDimension
type ScriptConfig struct {
	BaseTaskParam `yaml:",inline"`

	Command  string            `yaml:"command"`
	Format   string            `yaml:"format"` // "prometheus" | "custom"
	UserEnvs map[string]string `yaml:"user_envs"`
}

func (s *ScriptConfig) GetType() string { return define.ModuleScript }

func (s *ScriptConfig) Clean() error {
	s.BaseTaskParam.fillDefaults(define.ModuleScript)
	if s.Format == "" {
		s.Format = defaultScriptFormat
	}
	if s.Timeout <= 0 {
		s.Timeout = defaultScriptTimeout
	}
	return nil
}

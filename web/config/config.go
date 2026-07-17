// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License

// Package config holds monitorweb runtime configuration.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// WebConfig 是 monitorweb 的运行时配置。
type WebConfig struct {
	Listen          string          `yaml:"listen"`
	DBPath          string          `yaml:"db_path"` // 统一 SQLite 路径
	VictoriaMetrics VictoriaMetrics `yaml:"victoriametrics"`
	Alert           AlertConfig     `yaml:"alerts"`
	SMTP            SMTPConfig      `yaml:"smtp"`
	Registry        RegistryConfig  `yaml:"registry"`
	UIDir           string          `yaml:"ui_dir"`
}

// AlertConfig 配置告警引擎。
type AlertConfig struct {
	EvalInterval time.Duration `yaml:"eval_interval"`
}

// RegistryConfig 配置 Agent Registry 发现。
type RegistryConfig struct {
	TTL           time.Duration `yaml:"ttl"`            // agent 超时判离线，默认 90s
	CleanInterval time.Duration `yaml:"clean_interval"` // 清理间隔，默认 60s
}

// SMTPConfig 配置邮件发送。
type SMTPConfig struct {
	Host     string   `yaml:"host"`
	Port     int      `yaml:"port"`
	Username string   `yaml:"username"`
	Password string   `yaml:"password"`
	From     string   `yaml:"from"`
	To       []string `yaml:"to"`
	Insecure bool     `yaml:"insecure"`
}

// VictoriaMetrics 配置 VM 查询后端地址。
type VictoriaMetrics struct {
	URL     string        `yaml:"url"`
	Timeout time.Duration `yaml:"timeout"`
}

// Load 读取 yaml 配置，填充默认值并校验。
func Load(path string) (*WebConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg := &WebConfig{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := cfg.Clean(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Clean 填充默认值并校验必填项。
func (c *WebConfig) Clean() error {
	if c.Listen == "" {
		c.Listen = "0.0.0.0:8080"
	}
	if c.DBPath == "" {
		c.DBPath = "./data/monitorweb.db"
	}
	if c.VictoriaMetrics.URL == "" {
		c.VictoriaMetrics.URL = "http://127.0.0.1:8428"
	}
	if c.VictoriaMetrics.Timeout <= 0 {
		c.VictoriaMetrics.Timeout = 10 * time.Second
	}
	if c.Alert.EvalInterval <= 0 {
		c.Alert.EvalInterval = 60 * time.Second
	}
	if c.Registry.TTL <= 0 {
		c.Registry.TTL = 90 * time.Second
	}
	if c.Registry.CleanInterval <= 0 {
		c.Registry.CleanInterval = 60 * time.Second
	}
	if c.UIDir == "" {
		c.UIDir = "./web/ui/dist"
	}
	return nil
}

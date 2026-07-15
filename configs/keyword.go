// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

package configs

import (
	"github.com/abrance/monitorbeat/define"
)

const (
	defaultKeywordEncoding  = "utf-8"
	defaultKeywordBufSize   = 64 * 1024
	defaultKeywordFromBegin = true
)

// KeywordConfig 控制单文件日志抽取。
//
// P1.2 MVP 限定：
//   - 单文件路径，不支持 glob
//   - 不持久化 offset（每次启动按 FromBegin 决定从文件头或当前 EOF 开始）
//   - 编码仅支持 utf-8 / gb18030
type KeywordConfig struct {
	BaseTaskParam `yaml:",inline"`

	File       string `yaml:"file"`
	Pattern    string `yaml:"pattern"`
	Encoding   string `yaml:"encoding"`
	BufferSize int    `yaml:"buffer_size"`

	// FromBegin 用指针以区分"未配置"与"false"。
	FromBegin *bool `yaml:"from_begin"`
}

func (k *KeywordConfig) GetType() string { return define.ModuleKeyword }

func (k *KeywordConfig) Clean() error {
	k.BaseTaskParam.fillDefaults(define.ModuleKeyword)
	if k.Encoding == "" {
		k.Encoding = defaultKeywordEncoding
	}
	if k.BufferSize <= 0 {
		k.BufferSize = defaultKeywordBufSize
	}
	if k.FromBegin == nil {
		df := defaultKeywordFromBegin
		k.FromBegin = &df
	}
	return nil
}

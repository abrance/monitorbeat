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
//   - 编码仅支持 utf-8 / gb18030
//
// P2 enhancement:
//   - offset_registry 持久化读取偏移，重启断点续读
type KeywordConfig struct {
	BaseTaskParam `yaml:",inline"`

	File       string `yaml:"file"`
	Pattern    string `yaml:"pattern"`
	Encoding   string `yaml:"encoding"`
	BufferSize int    `yaml:"buffer_size"`

	// FromBegin 用指针以区分"未配置"与"false"。
	FromBegin *bool `yaml:"from_begin"`

	// OffsetRegistry 持久化 offset 的 JSON 文件路径。空表示不持久化。
	OffsetRegistry string `yaml:"offset_registry"`
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

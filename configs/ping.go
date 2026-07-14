// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import (
	"time"

	"github.com/abrance/monitorbeat/define"
)

const defaultPingBackend = "icmp"

type PingConfig struct {
	BaseTaskParam `yaml:",inline"`
	Target        string        `yaml:"target"`
	Backend       string        `yaml:"backend"`
	Count         int           `yaml:"count"`
	PayloadSize   int           `yaml:"payload_size"`
	MaxRTT        time.Duration `yaml:"max_rtt"`
	SendInterval  time.Duration `yaml:"send_interval"`
	Privileged    bool          `yaml:"privileged"`
}

func (p *PingConfig) GetType() string { return define.ModulePing }

func (p *PingConfig) Clean() error {
	p.BaseTaskParam.fillDefaults(define.ModulePing)
	if p.Count <= 0 {
		p.Count = 2
	}
	if p.PayloadSize < 8 {
		p.PayloadSize = 56
	}
	if p.MaxRTT <= 0 {
		p.MaxRTT = time.Second
	}
	if p.SendInterval <= 0 {
		p.SendInterval = 500 * time.Microsecond
	}
	if p.Backend == "" {
		p.Backend = defaultPingBackend
	}
	return nil
}

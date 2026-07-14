// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import "github.com/abrance/monitorbeat/define"

type UDPConfig struct {
	BaseTaskParam `yaml:",inline"`
	Address       string `yaml:"address"`
	Payload       string `yaml:"payload"`
	ExpectReply   bool   `yaml:"expect_response"`
}

func (c *UDPConfig) GetType() string { return define.ModuleUDP }

func (c *UDPConfig) Clean() error {
	c.BaseTaskParam.fillDefaults(define.ModuleUDP)
	return nil
}

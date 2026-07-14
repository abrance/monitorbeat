// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import "github.com/abrance/monitorbeat/define"

type TCPConfig struct {
	BaseTaskParam `yaml:",inline"`
	Address       string `yaml:"address"`
}

func (c *TCPConfig) GetType() string { return define.ModuleTCP }

func (c *TCPConfig) Clean() error {
	c.BaseTaskParam.fillDefaults(define.ModuleTCP)
	return nil
}

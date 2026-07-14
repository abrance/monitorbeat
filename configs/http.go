// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import "github.com/abrance/monitorbeat/define"

type HTTPConfig struct {
	BaseTaskParam `yaml:",inline"`
	URL           string            `yaml:"url"`
	Method        string            `yaml:"method"`
	Headers       map[string]string `yaml:"headers"`
	Body          string            `yaml:"body"`
	ExpectedCode  int               `yaml:"expected_status"`
}

func (c *HTTPConfig) GetType() string { return define.ModuleHTTP }

func (c *HTTPConfig) Clean() error {
	c.BaseTaskParam.fillDefaults(define.ModuleHTTP)
	if c.Method == "" {
		c.Method = "GET"
	}
	if c.ExpectedCode == 0 {
		c.ExpectedCode = 200
	}
	return nil
}

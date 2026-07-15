// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import (
	"time"

	"github.com/abrance/monitorbeat/define"
)

const defaultDmesgPeriod = 60 * time.Second

// DmesgConfig controls kernel ring buffer monitoring.
//
// P2 MVP:
//   - Runs "dmesg" command periodically
//   - Matches output against known exception patterns
//   - Emits one dmesg_event per run
type DmesgConfig struct {
	BaseTaskParam `yaml:",inline"`
}

func (c *DmesgConfig) GetType() string { return define.ModuleDmesg }

func (c *DmesgConfig) Clean() error {
	c.BaseTaskParam.fillDefaults(define.ModuleDmesg)
	if c.Period <= 0 {
		c.Period = defaultDmesgPeriod
	}
	return nil
}

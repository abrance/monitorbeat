// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package configs

import (
	"time"

	"github.com/abrance/monitorbeat/define"
)

const (
	defaultMetricbeatPeriod  = 60 * time.Second
	defaultMetricbeatTimeout = 10 * time.Second
)

// MetricbeatConfig controls Prometheus metrics pull (lightweight placeholder).
//
// P2 MVP:
//   - HTTP GET to configured URL
//   - Parse response as prometheus text format
//   - Emit one metricbeat_event per run with all parsed metrics + labels
type MetricbeatConfig struct {
	BaseTaskParam `yaml:",inline"`

	URL    string `yaml:"url"`
	Format string `yaml:"format"` // "prometheus" (default)
}

func (c *MetricbeatConfig) GetType() string { return define.ModuleMetricbeat }

func (c *MetricbeatConfig) Clean() error {
	c.BaseTaskParam.fillDefaults(define.ModuleMetricbeat)
	if c.Period <= 0 {
		c.Period = defaultMetricbeatPeriod
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultMetricbeatTimeout
	}
	if c.Format == "" {
		c.Format = "prometheus"
	}
	return nil
}

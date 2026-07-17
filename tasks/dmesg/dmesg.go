// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package dmesg implements kernel ring buffer monitoring.
//
// P2 MVP:
//   - Runs "dmesg" command periodically via os/exec
//   - Matches output against known kernel exception patterns
//   - Emits one dmesg_event per run with matched exceptions
//
// Reference: bkmonitorbeat/tasks/dmesg/ (uses /dev/kmsg + listen scheduler).
// MVP uses os/exec "dmesg" to avoid listen scheduler dependency.
package dmesg

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/internal/logging"
	"github.com/abrance/monitorbeat/tasks"
)

const EventType = "dmesg_event"

// exception patterns from bkmonitorbeat reference
var exceptionPatterns = []struct {
	Name    string
	Pattern string
}{
	{"ext3_fs_error", `EXT3-fs error`},
	{"disk_io_error", `I/O error`},
	{"table_full_drop_pkg", `table full, dropping packet`},
	{"out_of_socket_mem", `Out of socket memory`},
	{"allocation_failed", `allocation failed`},
	{"neighbour_table_overf", `neighbour table overflow`},
	{"mce_error", `^MCE`},
	{"run_in_m_clock_mode", `Running in modulated clock mode`},
	{"transmit_time_out", `transmit timed out`},
	{"oom", `(Out of Memory|out_of_memory|oom-kill)`},
	{"alloc_kernel_sgl", `Failed to alloc kernel SGL buffer for IOCTL`},
	{"nmi_received", `Uhhuh. NMI received`},
	{"page_alloc_fail", `page allocation failure`},
	{"nic_link_change", `NIC Link is`},
}

var compiledPatterns []struct {
	Name string
	Re   *regexp.Regexp
}

func init() {
	compiledPatterns = make([]struct {
		Name string
		Re   *regexp.Regexp
	}, len(exceptionPatterns))
	for i, ep := range exceptionPatterns {
		compiledPatterns[i].Name = ep.Name
		compiledPatterns[i].Re = regexp.MustCompile(ep.Pattern)
	}

	tasks.RegisterBuilder(define.ModuleDmesg, func(tc define.TaskConfig) (define.Task, error) {
		cfg, ok := tc.(*configs.DmesgConfig)
		if !ok {
			return nil, fmt.Errorf("dmesg: config type mismatch: %T", tc)
		}
		return New(cfg), nil
	})
}

// Gather is dmesg task runtime.
type Gather struct {
	tasks.BaseTask
	cfg *configs.DmesgConfig
}

// New constructs dmesg task.
func New(cfg *configs.DmesgConfig) define.Task {
	g := &Gather{cfg: cfg}
	g.SetConfig(cfg)
	g.SetStatus(define.StatusReady)
	return g
}

type matchResult struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// Run executes dmesg and emits matched exceptions.
func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
	start := time.Now()
	matches := g.collect(ctx)
	if matches == nil {
		matches = make([]matchResult, 0)
	}
	data := map[string]any{
		"dimensions": map[string]string{
			"hostname": tasks.Hostname(),
		},
		"metrics": map[string]float64{
			"total":   float64(len(matches)),
			"cost_ms": float64(time.Since(start).Milliseconds()),
		},
		"exceptions": matches,
	}
	select {
	case e <- define.NewEvent(EventType, data):
	case <-ctx.Done():
	}
}

func (g *Gather) collect(ctx context.Context) []matchResult {
	cmd := exec.CommandContext(ctx, "dmesg")
	out, err := cmd.Output()
	if err != nil {
		logging.Error("dmesg: exec failed", "err", err)
		return nil
	}

	lines := strings.Split(string(out), "\n")
	seen := make(map[string]bool)
	var results []matchResult

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, cp := range compiledPatterns {
			if cp.Re.MatchString(line) {
				key := cp.Name + ":" + line
				if seen[key] {
					continue
				}
				seen[key] = true
				results = append(results, matchResult{
					Name:    cp.Name,
					Message: line,
				})
				break
			}
		}
	}
	return results
}

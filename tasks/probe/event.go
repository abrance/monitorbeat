// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package probe

import (
	"os"
	"strconv"

	"github.com/abrance/monitorbeat/define"
)

func sourceHostname() string {
	if hostname := os.Getenv("MONITORBEAT_HOSTNAME"); hostname != "" {
		return hostname
	}
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

func BuildEvent(probeType, target string, taskID int32, result Result) define.Event {
	metrics := make(map[string]float64, len(result.Metrics)+2)
	for k, v := range result.Metrics {
		metrics[k] = v
	}
	if result.Success {
		metrics["success"] = 1
	} else {
		metrics["success"] = 0
	}
	metrics["duration_ms"] = DurationMillis(result.Duration)

	data := map[string]any{
		"dimensions": map[string]string{
			"hostname":   sourceHostname(),
			"probe_type": probeType,
			"target":     target,
			"task_id":    strconv.FormatInt(int64(taskID), 10),
		},
		"metrics": metrics,
		"error":   result.Error,
	}

	return define.NewEvent(probeType+"_event", data)
}

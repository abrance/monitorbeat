// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package parse 提供脚本输出解析，支持 prometheus text 和 custom key=value 格式。
package parse

import (
	"bufio"
	"strconv"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func init() {
	model.NameValidationScheme = model.LegacyValidation
}

// Parse 按 format 解析 stdout，返回 metrics 和 labels。
// format: "prometheus" 或 "custom"。
func Parse(format, stdout string) (metrics map[string]float64, labels map[string]string, err error) {
	switch format {
	case "prometheus":
		return parsePrometheus(stdout)
	case "custom":
		return parseCustom(stdout)
	default:
		return parseCustom(stdout)
	}
}

func parsePrometheus(out string) (map[string]float64, map[string]string, error) {
	metrics := make(map[string]float64)
	allLabels := make(map[string]string)

	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(out))
	if err != nil {
		// if parsing fails, return what we have (may be partial)
		return metrics, allLabels, nil
	}
	for _, mf := range families {
		for _, m := range mf.GetMetric() {
			key := mf.GetName()
			val := extractValue(m)
			if key != "" {
				metrics[key] = val
			}
			for _, lp := range m.GetLabel() {
				if lp.GetName() != "" && lp.GetValue() != "" {
					allLabels[lp.GetName()] = lp.GetValue()
				}
			}
		}
	}
	return metrics, allLabels, nil
}

func extractValue(m *dto.Metric) float64 {
	if m.GetGauge() != nil {
		return m.GetGauge().GetValue()
	}
	if m.GetCounter() != nil {
		return m.GetCounter().GetValue()
	}
	if m.GetUntyped() != nil {
		return m.GetUntyped().GetValue()
	}
	return 0
}

func parseCustom(out string) (map[string]float64, map[string]string, error) {
	metrics := make(map[string]float64)
	labels := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(parts[1])
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			labels[key] = valStr
			continue
		}
		metrics[key] = val
	}
	return metrics, labels, nil
}

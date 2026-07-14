// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package basereport

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/abrance/monitorbeat/configs"
	"github.com/abrance/monitorbeat/define"
)

// newTestGather 构造一个全开的 basereport 实例，cpu 采样 100ms 加速测试。
func newTestGather() *Gather {
	cfg := &configs.BasereportConfig{
		Cpu:  configs.CpuConfig{InfoPeriod: 100 * time.Millisecond},
		Mem:  configs.MemConfig{Enabled: true},
		Disk: configs.DiskConfig{Enabled: true, Paths: []string{"/"}},
		Load: configs.LoadConfig{Enabled: true},
		Net:  configs.NetConfig{Enabled: true},
	}
	_ = cfg.Clean()
	return New(cfg).(*Gather)
}

// 验证：Run 发出的事件 type = basereport，且 data 包含 dimensions + metrics 两个 key。
func TestGather_EventShape(t *testing.T) {
	g := newTestGather()
	ch := make(chan define.Event, 1)

	g.Run(context.Background(), ch)
	select {
	case ev := <-ch:
		if ev.GetType() != define.ModuleBasereport {
			t.Fatalf("type = %s, want %s", ev.GetType(), define.ModuleBasereport)
		}
		data, ok := ev.GetData().(map[string]any)
		if !ok {
			t.Fatalf("data type = %T, want map[string]any", ev.GetData())
		}
		if _, ok := data["dimensions"]; !ok {
			t.Fatal("missing dimensions key")
		}
		metrics, ok := data["metrics"].(map[string]float64)
		if !ok {
			t.Fatalf("metrics type = %T, want map[string]float64", data["metrics"])
		}
		if _, ok := metrics["cpu_usage"]; !ok {
			t.Fatal("missing cpu_usage metric")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event in 2s")
	}
}

// 验证：dimensions 至少包含 hostname（非空字符串）。
func TestGather_Dimensions_Hostname(t *testing.T) {
	g := newTestGather()
	dims := g.dimensions()
	if dims["hostname"] == "" {
		t.Fatal("hostname should be non-empty")
	}
}

// 验证：禁用 mem/disk/load/net 时 metrics 只有 cpu_usage。
func TestGather_OnlyCpu(t *testing.T) {
	cfg := &configs.BasereportConfig{
		Cpu: configs.CpuConfig{InfoPeriod: 50 * time.Millisecond},
	}
	_ = cfg.Clean()
	g := New(cfg).(*Gather)
	ch := make(chan define.Event, 1)
	g.Run(context.Background(), ch)
	ev := <-ch
	metrics := ev.GetData().(map[string]any)["metrics"].(map[string]float64)
	if len(metrics) != 1 {
		t.Fatalf("metrics size = %d, want 1 (cpu only): %+v", len(metrics), metrics)
	}
	if _, ok := metrics["cpu_usage"]; !ok {
		t.Fatalf("missing cpu_usage: %+v", metrics)
	}
}

// 注：cpu.Percent 的 ctx 取消语义被故意不测：gopsutil 在采样窗口期间
// 是阻塞 syscall，无法中断。Run 内的 ctx.Done 检查只能保护"采样返回后
// 是否还把事件发出"，无法把 1s 窗口缩短。这是已知平台限制。

// 验证：sanitizeMetricKey 把路径转成合法 metric key。
func TestSanitizeMetricKey(t *testing.T) {
	cases := map[string]string{
		"/":            "root",
		"":             "root",
		"/var/log":     "var_log",
		"/var/lib/foo": "var_lib_foo",
		"/mnt/data.1":  "mnt_data_1",
		"/run/user/1000": "run_user_1000",
	}
	for in, want := range cases {
		if got := sanitizeMetricKey(in); got != want {
			t.Errorf("sanitizeMetricKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// 验证：事件可被 JSON 序列化（与 console/file output 兼容）。
func TestGather_EventJSONSerializable(t *testing.T) {
	g := newTestGather()
	ch := make(chan define.Event, 1)
	g.Run(context.Background(), ch)
	ev := <-ch
	payload := map[string]any{
		"timestamp": ev.GetTimestamp(),
		"type":      ev.GetType(),
		"data":      ev.GetData(),
	}
	if _, err := json.Marshal(payload); err != nil {
		t.Fatalf("event not json serializable: %v", err)
	}
}

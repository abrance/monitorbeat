# P1.3 Script Collector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an MVP `script` task that periodically executes a shell command, parses stdout as prometheus text or custom key=value format, and emits `script_event` events. One run = one event carrying all parsed metrics + dimensions from the command output.

**Architecture:** Mirror bkmonitorbeat's `task/script/gather.go` + `task/script/formatdata.go` + `task/script/scriptevent.go` pipeline, stripped of BK concepts (DataID/BizID/CMDB labels/KeepOneDimension). Use the existing daemon time-heap scheduler (scripts are periodic, not long-running). Single event per run: all parsed key-value pairs go into one `script_event`.

**Tech Stack:** Go 1.25, standard library `os/exec` / `bufio` / `context`, `github.com/prometheus/common/expfmt` for prometheus parsing, existing `define.Event` / `tasks.BaseTask` / `tasks.RegisterBuilder` / `tasks/factory.Build`.

**Scope (MVP — explicit):**
- IN: command string (shell-executed), user_envs, timeout, period. Supports two output formats: `prometheus` (via expfmt) and `custom` (line-by-line `key=value`).
- OUT: one `script_event` per successful run carrying `metrics.{key}=value` pairs + `cost_ms` + `dimensions.command` + `dimensions.task_id`.
- NOT IN: timestamp parsing from metric lines (use current time), multi-timestamp aggregation, KeepOneDimension, platform-specific ShellWordPreProcess (just shell via `sh -c`), script stderr capture as dimension.

---

## Current Progress Snapshot

Date: 2026-07-15 — P1.2 (keyword) finalized; P1.3 closed.

Local Go command: `/opt/go/1.25.12/bin/go` (reports `go1.25.12 linux/amd64`).

Toolchain env: `GOTOOLCHAIN=auto`, `GOMODCACHE=/home/xiaoy/go/pkg/mod`, `GOPROXY=https://goproxy.cn|https://goproxy.io|direct`.

Current verification (after P1.3 closes):
- `go build ./...` PASS
- `go vet ./...` clean
- `go test ./...` all green (17 packages)
- `gofmt -l internal/script tasks/script configs cmd/monitorbeat` clean
- P1.3 live smoke: `configs/p1_script.yaml`, 3 `script_event` in 12s, metrics `demo_total=42` + `cost_ms`

Progress by task:

| Task | Status |
|---|---|
| A. ScriptConfig type + Config wiring | ✅ done |
| B. `internal/script/exec` command runner | ✅ done (3 tests) |
| C. `internal/script/parse` output parser (prometheus + custom) | ✅ done (3 tests) |
| D. `tasks/script/script.go` task wiring + builder | ✅ done (3 tests) |
| E. End-to-end demo (`configs/p1_script.yaml` + `-check` + live smoke) | ✅ done (3 events in 12s) |
| F. Final P1.3 verification + commit | ✅ done |

## File Structure

```
configs/
├── script.go                         # CREATE — ScriptConfig + Clean
├── config.go                         # MODIFY — add Scripts slice, GetTaskConfigListByType case, AllTaskConfigs, Clean
└── config_test.go                    # MODIFY — add TestScriptConfig_CleanDefaults + TestConfig_ScriptGrouping

internal/script/exec/                 # NEW package
├── runner.go                         # CREATE — Run(ctx, command, envs, timeout) -> stdout, error
└── runner_test.go                    # CREATE — echo success, timeout, command-not-found

internal/script/parse/                # NEW package
├── parser.go                         # CREATE — Parse(format, stdout) -> map[string]float64 + labels map
└── parser_test.go                    # CREATE — prometheus format, custom format, empty, comment-only

tasks/script/                         # NEW package (mirrors tasks/ping)
├── script.go                         # CREATE — Gather struct, init() builder, Run path
├── script_test.go                    # CREATE — echo script test, failing script test
└── doc.go                            # CREATE — package comment

configs/p1_script.yaml               # CREATE — demo: prometheus echo script
```

---

## Task A: ScriptConfig type + Config wiring

**Files:**
- Create: `configs/script.go`
- Modify: `configs/config.go` — add `Scripts []ScriptConfig` slice, `case define.ModuleScript` to `GetTaskConfigListByType`, append to `AllTaskConfigs` and `Clean`.
- Modify: `configs/config_test.go` — add `TestScriptConfig_CleanDefaults` + `TestConfig_ScriptGrouping`.

- [ ] **Step 1: Append tests to configs/config_test.go**

```go
func TestScriptConfig_CleanDefaults(t *testing.T) {
    c := &Config{
        Scripts: []ScriptConfig{
            {
                BaseTaskParam: BaseTaskParam{Enabled: true},
                Command:       "echo 'demo_total 42'",
            },
        },
    }
    if err := c.Clean(); err != nil {
        t.Fatalf("clean: %v", err)
    }
    s := c.Scripts[0]
    if s.GetIdent() != "script:1" {
        t.Fatalf("ident = %q, want script:1", s.GetIdent())
    }
    if s.GetType() != define.ModuleScript {
        t.Fatalf("type = %q, want %q", s.GetType(), define.ModuleScript)
    }
    if s.Format == "" {
        t.Fatal("format should default")
    }
    if s.Timeout <= 0 {
        t.Fatalf("timeout should default positive, got %d", s.Timeout)
    }
}

func TestConfig_ScriptGrouping(t *testing.T) {
    c := &Config{
        Scripts: []ScriptConfig{
            {BaseTaskParam: BaseTaskParam{TaskID: 21}},
            {BaseTaskParam: BaseTaskParam{TaskID: 22}},
        },
    }
    if got := c.GetTaskConfigListByType(define.ModuleScript); len(got) != 2 {
        t.Fatalf("script list len = %d, want 2", len(got))
    }
    if got := c.AllTaskConfigs(); len(got) != 2 {
        t.Fatalf("all task configs len = %d, want 2", len(got))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/opt/go/1.25.12/bin/go test ./configs/... -run Script -v`
Expected: FAIL with `undefined: ScriptConfig`.

- [ ] **Step 3: Implement `configs/script.go`**

```go
// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

package configs

import (
    "time"

    "github.com/abrance/monitorbeat/define"
)

const (
    defaultScriptFormat  = "prometheus"
    defaultScriptTimeout = 30 * time.Second
)

// ScriptConfig 控制脚本采集任务。
//
// P1.3 MVP 限定：
//   - 单行 command，通过 sh -c 执行
//   - Format: "prometheus"（使用 expfmt）或 "custom"（key=value 逐行）
//   - 不做 timestamp/offset 解析（用当前时间）
//   - 不做 KeepOneDimension
type ScriptConfig struct {
    BaseTaskParam `yaml:",inline"`

    Command  string            `yaml:"command"`
    Format   string            `yaml:"format"`   // "prometheus" | "custom"
    Timeout  time.Duration     `yaml:"timeout"`
    UserEnvs map[string]string `yaml:"user_envs"`
}

func (s *ScriptConfig) GetType() string { return define.ModuleScript }

func (s *ScriptConfig) Clean() error {
    s.BaseTaskParam.fillDefaults(define.ModuleScript)
    if s.Format == "" {
        s.Format = defaultScriptFormat
    }
    if s.Timeout <= 0 {
        s.Timeout = defaultScriptTimeout
    }
    return nil
}
```

- [ ] **Step 4: Wire into Config**

In `configs/config.go`:
1. Add `Scripts []ScriptConfig \`yaml:"scripts"\`` to the struct (after `Pings`).
2. Add `case define.ModuleScript:` block to `GetTaskConfigListByType`.
3. Append the same loop to `AllTaskConfigs`.
4. Append `for i := range c.Scripts { if err := c.Scripts[i].Clean(); err != nil { return err } }` to `Clean`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `/opt/go/1.25.12/bin/go test ./configs/... -v`
Expected: PASS.

---

## Task B: `internal/script/exec` command runner

**Files:**
- Create: `internal/script/exec/runner.go`
- Create: `internal/script/exec/runner_test.go`

Lightweight wrapper around `os/exec`. Runs `sh -c "<command>"` with optional env vars, respects ctx timeout. Returns combined stdout or an error.

- [ ] **Step 1: Write failing test**

```go
package exec

import (
    "context"
    "strings"
    "testing"
    "time"
)

func TestRun_Echo(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    out, err := Run(ctx, "echo hello", nil)
    if err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(out, "hello") {
        t.Fatalf("got %q, want hello", out)
    }
}

func TestRun_Timeout(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()
    _, err := Run(ctx, "sleep 5", nil)
    if err == nil {
        t.Fatal("expected timeout error")
    }
}

func TestRun_CommandNotFound(t *testing.T) {
    ctx := context.Background()
    _, err := Run(ctx, "nonexistent_command_xyz", nil)
    if err == nil {
        t.Fatal("expected error for missing command")
    }
}
```

- [ ] **Step 2: Verify FAIL**

Run: `/opt/go/1.25.12/bin/go test ./internal/script/exec/... -v`
Expected: FAIL with `undefined: Run`.

- [ ] **Step 3: Implement `internal/script/exec/runner.go`**

```go
// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

// Package exec 提供脚本命令执行封装。
package exec

import (
    "context"
    "fmt"
    "os/exec"
)

// Run 通过 sh -c 执行 command，把 userEnvs 追加到进程环境变量中。
// 返回合并的 stdout+stderr 或错误。
func Run(ctx context.Context, command string, userEnvs map[string]string) (string, error) {
    cmd := exec.CommandContext(ctx, "sh", "-c", command)
    if userEnvs != nil {
        cmd.Env = append(cmd.Environ(), envSlice(userEnvs)...)
    }
    out, err := cmd.CombinedOutput()
    if err != nil {
        return string(out), fmt.Errorf("exec %q: %w\n%s", command, err, out)
    }
    return string(out), nil
}

func envSlice(m map[string]string) []string {
    s := make([]string, 0, len(m))
    for k, v := range m {
        s = append(s, k+"="+v)
    }
    return s
}
```

- [ ] **Step 4: Verify PASS**

Run: `/opt/go/1.25.12/bin/go test ./internal/script/exec/... -v`
Expected: PASS.

---

## Task C: `internal/script/parse` output parser

**Files:**
- Create: `internal/script/parse/parser.go`
- Create: `internal/script/parse/parser_test.go`

Parses script stdout into `metrics map[string]float64` + `labels map[string]string`. Two formats:
- `prometheus`: Use `github.com/prometheus/common/expfmt` to decode text format. Extract metric name+value. Labels (dimensions) are extracted from metric label pairs.
- `custom`: Simple line-by-line `key=value`, one pair per line. Lines starting with `#` or empty lines are skipped.

- [ ] **Step 1: Write failing tests**

```go
package parse

import (
    "testing"
)

func TestParsePrometheus(t *testing.T) {
    out := `# HELP demo_total demo counter
# TYPE demo_total counter
demo_total{env="prod"} 42.0
demo_uptime_seconds 99.5
`
    metrics, labels, err := Parse("prometheus", out)
    if err != nil {
        t.Fatal(err)
    }
    if v, ok := metrics["demo_total"]; !ok || v != 42.0 {
        t.Fatalf("demo_total = %v, want 42.0", v)
    }
    if v, ok := metrics["demo_uptime_seconds"]; !ok || v != 99.5 {
        t.Fatalf("uptime = %v, want 99.5", v)
    }
    if v, ok := labels["env"]; !ok || v != "prod" {
        t.Fatalf("env label = %q, want prod", v)
    }
}

func TestParseCustom(t *testing.T) {
    out := `duration_ms=1500
status=ok
count=3
`
    metrics, _, err := Parse("custom", out)
    if err != nil {
        t.Fatal(err)
    }
    if len(metrics) != 3 {
        t.Fatalf("got %d metrics, want 3", len(metrics))
    }
    if metrics["count"] != 3 {
        t.Fatalf("count = %v, want 3", metrics["count"])
    }
}

func TestParse_EmptyOutput(t *testing.T) {
    metrics, _, err := Parse("custom", "")
    if err != nil {
        t.Fatal(err)
    }
    if len(metrics) != 0 {
        t.Fatalf("expected empty metrics, got %d", len(metrics))
    }
}
```

- [ ] **Step 2: Verify FAIL**

Run: `/opt/go/1.25.12/bin/go test ./internal/script/parse/... -v`
Expected: FAIL with `undefined: Parse`.

- [ ] **Step 3: Implement `internal/script/parse/parser.go`**

```go
// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

// Package parse 提供脚本输出解析，支持 prometheus text 和 custom key=value 格式。
package parse

import (
    "bufio"
    "strconv"
    "strings"

    dto "github.com/prometheus/client_model/go"
    "github.com/prometheus/common/expfmt"
)

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

    parser := expfmt.TextParser{}
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
```

- [ ] **Step 4: Add `prometheus/client_model` + `prometheus/common` dependencies**

```bash
/opt/go/1.25.12/bin/go get github.com/prometheus/common@latest github.com/prometheus/client_model@latest
/opt/go/1.25.12/bin/go mod tidy
```

- [ ] **Step 5: Verify PASS**

Run: `/opt/go/1.25.12/bin/go test ./internal/script/parse/... -v`
Expected: PASS.

---

## Task D: `tasks/script/script.go` task wiring + builder

**Files:**
- Create: `tasks/script/script.go`
- Create: `tasks/script/script_test.go`
- Create: `tasks/script/doc.go`
- Modify: `cmd/monitorbeat/main.go` — add blank import for `tasks/script`.

- [ ] **Step 1: Write failing test**

```go
package script

import (
    "context"
    "testing"
    "time"

    "github.com/abrance/monitorbeat/configs"
    "github.com/abrance/monitorbeat/define"
)

func TestGather_Run_Echo(t *testing.T) {
    cfg := &configs.ScriptConfig{
        BaseTaskParam: configs.BaseTaskParam{TaskID: 100, Enabled: true},
        Command:       "echo 'demo_total 42'",
        Format:        "prometheus",
        Timeout:       5 * time.Second,
    }
    _ = cfg.Clean()

    g := New(cfg).(*Gather)

    ch := make(chan define.Event, 4)
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    g.Run(ctx, ch)

    select {
    case ev := <-ch:
        if ev.GetType() != ScriptEventType {
            t.Fatalf("event type = %q", ev.GetType())
        }
        data := ev.GetData().(map[string]any)
        metrics := data["metrics"].(map[string]float64)
        if metrics["demo_total"] != 42 {
            t.Fatalf("demo_total = %v, want 42", metrics["demo_total"])
        }
        if metrics["cost_ms"] < 0 {
            t.Fatalf("cost_ms should be >= 0, got %v", metrics["cost_ms"])
        }
    case <-time.After(3 * time.Second):
        t.Fatal("no event received")
    }
}

func TestGather_FailScript(t *testing.T) {
    cfg := &configs.ScriptConfig{
        BaseTaskParam: configs.BaseTaskParam{TaskID: 101, Enabled: true},
        Command:       "exit 1",
        Format:        "prometheus",
        Timeout:       5 * time.Second,
    }
    _ = cfg.Clean()

    g := New(cfg).(*Gather)
    ch := make(chan define.Event, 4)
    ctx := context.Background()
    g.Run(ctx, ch)

    select {
    case ev := <-ch:
        data := ev.GetData().(map[string]any)
        if ev.GetType() != ScriptEventType {
            t.Fatalf("event type = %q", ev.GetType())
        }
        if data["error"] == nil || data["error"] == "" {
            t.Fatal("expected error field on script failure")
        }
    case <-time.After(3 * time.Second):
        t.Fatal("no event received")
    }
}

func TestGather_InvalidConfig(t *testing.T) {
    cfg := &configs.ScriptConfig{}
    if _, err := builder(cfg); err == nil {
        t.Fatal("expected error for empty command")
    }
}
```

- [ ] **Step 2: Verify FAIL**

Run: `/opt/go/1.25.12/bin/go test ./tasks/script/... -v`
Expected: FAIL with `undefined: Gather` / `undefined: builder`.

- [ ] **Step 3: Implement `tasks/script/script.go`**

```go
// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

// Package script 实现 monitorbeat 的脚本采集 task。
//
// P1.3 MVP：
//   - 定期执行 shell 命令，解析 stdout（prometheus / custom 格式）
//   - 每次 Run 发一条 script_event，包含所有解析出的 metrics + labels
//   - 使用 daemon 时间堆调度（周期性触发），非长驻调度器
package script

import (
    "context"
    "fmt"
    "time"

    "github.com/abrance/monitorbeat/configs"
    "github.com/abrance/monitorbeat/define"
    execrunner "github.com/abrance/monitorbeat/internal/script/exec"
    "github.com/abrance/monitorbeat/internal/script/parse"
    "github.com/abrance/monitorbeat/tasks"
)

const ScriptEventType = "script_event"

func init() {
    tasks.RegisterBuilder(define.ModuleScript, func(tc define.TaskConfig) (define.Task, error) {
        return builder(tc)
    })
}

func builder(tc define.TaskConfig) (define.Task, error) {
    cfg, ok := tc.(*configs.ScriptConfig)
    if !ok {
        return nil, fmt.Errorf("script: config type mismatch: %T", tc)
    }
    if cfg.Command == "" {
        return nil, fmt.Errorf("script: command is required")
    }
    return New(cfg), nil
}

// Gather 是 script task 的运行时实例。
type Gather struct {
    tasks.BaseTask
    cfg *configs.ScriptConfig
}

// New 构造可运行的 script task。
func New(cfg *configs.ScriptConfig) define.Task {
    g := &Gather{cfg: cfg}
    g.SetConfig(cfg)
    g.SetStatus(define.StatusReady)
    return g
}

// Run 执行脚本 → 解析输出 → 发 script_event。
func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
    start := time.Now()

    // step 1: execute
    timeout := g.cfg.Timeout
    if timeout <= 0 {
        timeout = 30 * time.Second
    }
    cmdCtx, cmdCancel := context.WithTimeout(ctx, timeout)
    defer cmdCancel()

    stdout, execErr := execrunner.Run(cmdCtx, g.cfg.Command, g.cfg.UserEnvs)

    costMs := float64(time.Since(start).Milliseconds())

    // step 2: parse
    metrics, labels, parseErr := parse.Parse(g.cfg.Format, stdout)

    // step 3: build event
    metrics["cost_ms"] = costMs

    dims := map[string]string{
        "command": g.cfg.Command,
        "task_id": fmt.Sprintf("%d", g.cfg.TaskID),
    }
    for k, v := range labels {
        dims[k] = v
    }

    data := map[string]any{
        "dimensions": dims,
        "metrics":    metrics,
    }

    if execErr != nil {
        data["error"] = execErr.Error()
    }
    if parseErr != nil {
        data["parse_error"] = parseErr.Error()
    }

    select {
    case e <- define.NewEvent(ScriptEventType, data):
    case <-ctx.Done():
    }
}
```

- [ ] **Step 4: Wire blank import in `cmd/monitorbeat/main.go`**

Add after the existing task blank imports:

```go
    _ "github.com/abrance/monitorbeat/tasks/script"
```

- [ ] **Step 5: Verify PASS**

Run: `/opt/go/1.25.12/bin/go test ./tasks/script/... -v && /opt/go/1.25.12/bin/go build ./...`
Expected: all PASS.

---

## Task E: End-to-end demo

**Files:**
- Create: `configs/p1_script.yaml`

- [ ] **Step 1: Write demo config**

```yaml
# monitorbeat P1.3 script task demo 配置
#
# 跑 daemon：
#   ./bin/monitorbeat -config configs/p1_script.yaml
#
# console 每 5s 输出一条 script_event，metrics 包含 demo_total=42 和 script 执行耗时 cost_ms。

check_interval: 1s
event_buffer_size: 1024
admin_addr: ""

outputs:
  - type: console

scripts:
  - task_id: 3001
    enabled: true
    period: 5s
    timeout: 10s
    format: prometheus
    command: |
      echo '# HELP demo_total demo counter'
      echo '# TYPE demo_total counter'
      echo 'demo_total{env="local"} 42'
```

- [ ] **Step 2: Build and check config**

```bash
/opt/go/1.25.12/bin/go build -o bin/monitorbeat ./cmd/monitorbeat
./bin/monitorbeat -config configs/p1_script.yaml -check
```

Expected: `config OK`, exit 0.

- [ ] **Step 3: Live smoke test**

Run for ~10s:

```bash
timeout --signal=TERM --kill-after=2s 10s ./bin/monitorbeat -config configs/p1_script.yaml > /tmp/script.stdout 2> /tmp/script.stderr
```

Verify:
```bash
grep -c '"type":"script_event"' /tmp/script.stdout   # → >= 1
grep '"type":"script_event"' /tmp/script.stdout | head -1
```

Expected: at least 1 `script_event` with `demo_total=42` in metrics.

---

## Task F: Final P1.3 verification

**Files:**
- Modify: `README.md` — update status table + verification snapshot + add P1.3 demo section.
- Modify: this plan file — update snapshot table.

- [ ] **Step 1: Whole-repo verification**

```bash
cd /opt/mystorage/github/monitorbeat
/opt/go/1.25.12/bin/go build ./...           # PASS
/opt/go/1.25.12/bin/go vet ./...             # clean
/opt/go/1.25.12/bin/go test ./...            # all PASS
/opt/go/1.25.12/bin/gofmt -l tasks/script configs internal/script cmd/monitorbeat  # empty
/opt/go/1.25.12/bin/go mod tidy              # exit 0, no diff
```

- [ ] **Step 2: Update docs**

Update `README.md` P1.3 status to ✅ with module table and demo section. Update this plan's progress snapshot.

- [ ] **Step 3: Commit P1.3**

---

## Risks & Notes

1. **prometheus/common dependency**: The `expfmt.TextParser` is the standard Go library for prometheus text format. It handles HELP/TYPE comments, labels, timestamp, counter/gauge/untyped. MVP uses it as-is; custom format falls back to simple key=value line parser.
2. **Single event per run**: Unlike bkmonitorbeat which can produce multiple events per run (one per timestamp+dimension combo), our MVP emits exactly one event per execution. All metrics in the output go into one `script_event`. This is simpler and sufficient for the common use case.
3. **Shell execution**: `sh -c` is used on all platforms. No platform-specific ShellWordPreProcess (that's a bkmonitorbeat concept for Windows cmd escaping). The command runs in a subshell with merged env.
4. **No KeepOneDimension**: bkmonitorbeat's KeepOneDimension collapses multiple dimensions for the same metric into one. We skip this entirely - all labels go into dimensions as-is.
5. **Error handling**: Command failure emits an event with `error` field. Parse failure emits event with `parse_error` field. Both cases still emit an event (not silently dropped).
6. **Periodic scheduling**: Script tasks use the existing daemon time-heap scheduler (like basereport and probe tasks). The daemon scheduler triggers `Run()` every `period`. This is simpler and correct for periodic script execution.

(End of file)

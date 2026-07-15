# P1.2 Keyword Log Harvester Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an MVP `keyword` log-harvester task that tails a file, extracts regex captures from each line, and emits a `raw_log` event per matched line. Periodic keyword aggregation is explicitly out of scope for this phase.

**Architecture:** Mirror bkmonitorbeat's `tasks/keyword/input/file` + `tasks/keyword/processor` + `tasks/keyword/sender` pipeline (raw_log branch only), wired through a dedicated `scheduler/keyword` scheduler that is a thin adaptation of `scheduler/listen` (one long-running goroutine per task, not driven by the daemon time-heap). Send via the existing `internal/output` channel; no libgse anywhere.

**Tech Stack:** Go 1.25, standard library `bufio` / `os` / `io` / `regexp` / `context`, existing `define.Event` / `tasks.BaseTask` / `tasks.RegisterBuilder` / `tasks/factory.Build`, existing `internal/output`.

**Scope (MVP — explicit):**
- IN:  single file path per task, regex with named or unnamed capture groups, `encoding` (`utf-8` or `gb18030`).
- OUT: one `raw_log` event per matched line carrying `dimensions.file` / `dimensions.regex` / `dimensions.line_number` + `metrics.matches_count=1` + `data.fields` (regex captures map) + `data.raw` (original line).
- NOT IN: `keyword_event` periodic aggregation, `exclude_files`, multi-file glob, offset persistence, scan_frequency tuning, multiline, json / delim parsing. These land in a later phase.

---

## Current Progress Snapshot

Date: 2026-07-15 — P1.2 keyword raw_log harvester COMPLETE.

Local Go command: `/opt/go/1.25.12/bin/go` (reports `go1.25.12 linux/amd64`).

Toolchain env: `GOTOOLCHAIN=auto`, `GOMODCACHE=/home/xiaoy/go/pkg/mod`, `GOPROXY=https://goproxy.cn|https://goproxy.io|direct`.

Final verification (P1.2 complete):
- `go build ./...` PASS
- `go vet ./...` clean
- `go test ./...` all green (16 packages)
- `gofmt -l` clean across all new packages
- Smoke test: 5 raw_log events confirmed

Progress by remaining task:

| Remaining Task | Status | Evidence |
|---|---|---|
| A. KeywordConfig type + Config wiring | Complete | configs tests pass (TestKeywordConfig_CleanDefaults + TestConfig_KeywordGrouping) |
| B. Raw event helper (`tasks/keyword/raw_event.go`) | Complete | TestBuildRawLogEvent_Shape + TestBuildRawLogEvent_NilCaptures PASS |
| C. `internal/regexp/extract` capture helper | Complete | 5 test cases all PASS (named/unnamed/mixed/no_match/zero_capture) |
| D. `internal/input/file` harvester (single file, append-only) | Complete | 3 test cases all PASS (FromBegin/AppendAndCancel/ContextCancel) |
| E. `scheduler/keyword` scheduler (long-running per task) | Complete | 3 test cases all PASS (StartsAndStops/IsDaemon/StopExitsRun) |
| F. `tasks/keyword/keyword.go` task wiring + builder | Complete | EndToEnd + InvalidConfig + ExtractLine tests PASS |
| G. End-to-end demo (`configs/p1_keyword.yaml` + `-check` + live smoke) | Complete | -check → config OK; 5 raw_log events with correct fields |
| H. Final P1.2 verification | Complete | build/vet/test/fmt all clean; smoke confirmed; README updated |

## File Structure

```
configs/
├── keyword.go                       # CREATE — KeywordConfig + Clean
├── config.go                        # MODIFY — add Keywords slice, GetTaskConfigListByType case, AllTaskConfigs append, Clean loop
└── config_test.go                   # MODIFY — add TestKeywordConfig_CleanDefaults + TestConfig_KeywordGrouping

define/
└── event.go                         # UNCHANGED — SimpleEvent already supports raw_log payload

tasks/keyword/
├── keyword.go                       # CREATE — Gather struct, init() builder, Run path
├── raw_event.go                     # CREATE — BuildRawLogEvent(file, regex, lineNo, captures, raw)
├── keyword_test.go                  # CREATE — BuildRawLogEvent + extract + end-to-end fake-file tests
└── README.md                        # CREATE — one-paragraph usage + YAML example

internal/input/file/                 # NEW package
├── harvester.go                     # CREATE — Harvester struct: tail append-only file, support Seek + Read from offset, detect rotate by inode/size
├── harvester_test.go                # CREATE — fake file scenarios (new lines, partial UTF-8)
└── doc.go                           # CREATE — package comment

internal/regexp/extract/             # NEW package
├── extract.go                       # CREATE — Extract(line, re) -> map[string]string + matched bool
└── extract_test.go                  # CREATE — named groups, unnamed groups, no match, multibyte safety

scheduler/keyword/                   # NEW package (mirrors scheduler/listen pattern)
├── scheduler.go                     # CREATE — Scheduler: one long-lived goroutine per task; uses define.BaseScheduler
└── scheduler_test.go                # CREATE — start/stop/reload, ctx-cancel exits cleanly

configs/p1_keyword.yaml              # CREATE — demo: tail /tmp/demo.log with regex `ERROR.*payment_id=(\d+) amount=(\d+\.\d+)`
```

---

## Task A: KeywordConfig type + Config wiring

**Files:**
- Create: `configs/keyword.go`
- Modify: `configs/config.go` — add `Keywords []KeywordConfig` slice, append `case define.ModuleKeyword` to `GetTaskConfigListByType`, append `Keywords` to `AllTaskConfigs` and `Clean`.
- Modify: `configs/config_test.go` — add `TestKeywordConfig_CleanDefaults` + `TestConfig_KeywordGrouping`.

- [ ] **Step 1: Write failing tests**

Append to `configs/config_test.go`:

```go
func TestKeywordConfig_CleanDefaults(t *testing.T) {
    c := &Config{
        Keywords: []KeywordConfig{
            {
                BaseTaskParam: BaseTaskParam{Enabled: true},
                File:          "/tmp/demo.log",
                Pattern:       `ERROR.*payment_id=(\d+)`,
            },
        },
    }
    if err := c.Clean(); err != nil {
        t.Fatalf("clean: %v", err)
    }
    k := c.Keywords[0]
    if k.GetIdent() != "keyword:1" {
        t.Fatalf("ident = %q, want keyword:1", k.GetIdent())
    }
    if k.GetType() != define.ModuleKeyword {
        t.Fatalf("type = %q, want %q", k.GetType(), define.ModuleKeyword)
    }
    if k.Encoding == "" {
        t.Fatal("encoding should default")
    }
    if k.BufferSize <= 0 {
        t.Fatalf("buffer_size should default positive, got %d", k.BufferSize)
    }
    if k.FromBegin == nil || !*k.FromBegin {
        t.Fatalf("from_begin should default to true, got %v", k.FromBegin)
    }
}

func TestConfig_KeywordGrouping(t *testing.T) {
    c := &Config{
        Keywords: []KeywordConfig{
            {BaseTaskParam: BaseTaskParam{TaskID: 11}},
            {BaseTaskParam: BaseTaskParam{TaskID: 12}},
        },
    }
    if got := c.GetTaskConfigListByType(define.ModuleKeyword); len(got) != 2 {
        t.Fatalf("keyword list len = %d, want 2", len(got))
    }
    if got := c.AllTaskConfigs(); len(got) != 2 {
        t.Fatalf("all task configs len = %d, want 2", len(got))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/opt/go/1.25.12/bin/go test ./configs/... -run Keyword -v`
Expected: FAIL with `undefined: KeywordConfig`.

- [ ] **Step 3: Implement `configs/keyword.go`**

```go
// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

package configs

import (
    "github.com/abrance/monitorbeat/define"
)

const (
    defaultKeywordEncoding  = "utf-8"
    defaultKeywordBufSize   = 64 * 1024
    defaultKeywordFromBegin = true
)

// KeywordConfig 控制单文件日志抽取。
//
// P1.2 MVP 限定：
//   - 单文件路径，不支持 glob
//   - 不持久化 offset（每次启动按 FromBegin 决定从文件头或当前 EOF 开始）
//   - 编码仅支持 utf-8 / gb18030
type KeywordConfig struct {
    BaseTaskParam `yaml:",inline"`

    File       string `yaml:"file"`
    Pattern    string `yaml:"pattern"`
    Encoding   string `yaml:"encoding"`
    BufferSize int    `yaml:"buffer_size"`

    // FromBegin 用指针以区分"未配置"与"false"。
    FromBegin *bool `yaml:"from_begin"`
}

func (k *KeywordConfig) GetType() string { return define.ModuleKeyword }

func (k *KeywordConfig) Clean() error {
    k.BaseTaskParam.fillDefaults(define.ModuleKeyword)
    if k.Encoding == "" {
        k.Encoding = defaultKeywordEncoding
    }
    if k.BufferSize <= 0 {
        k.BufferSize = defaultKeywordBufSize
    }
    if k.FromBegin == nil {
        df := defaultKeywordFromBegin
        k.FromBegin = &df
    }
    return nil
}
```

- [ ] **Step 4: Wire into `Config`**

In `configs/config.go`:
1. Add `Keywords []KeywordConfig \`yaml:"keywords"\`` to the struct (alphabetical after `HTTPs`).
2. Add `case define.ModuleKeyword:` block to `GetTaskConfigListByType`.
3. Append the same loop to `AllTaskConfigs`.
4. Append `for i := range c.Keywords { if err := c.Keywords[i].Clean(); err != nil { return err } }` to `Clean`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `/opt/go/1.25.12/bin/go test ./configs/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /opt/mystorage/github/monitorbeat
git add configs/keyword.go configs/config.go configs/config_test.go
git commit -m "feat(configs): add KeywordConfig for raw_log log harvester"
```

---

## Task B: Raw event helper

**Files:**
- Create: `tasks/keyword/raw_event.go`
- Create: `tasks/keyword/raw_event_test.go`

- [ ] **Step 1: Write failing test**

`tasks/keyword/raw_event_test.go`:

```go
package keyword

import "testing"

func TestBuildRawLogEvent_Shape(t *testing.T) {
    ev := BuildRawLogEvent(
        "/tmp/demo.log",
        `ERROR payment_id=(\d+)`,
        7,
        map[string]string{"1": "12345"},
        "ERROR payment_id=12345 amount=99.9",
    )
    if ev.GetType() != RawLogEventType {
        t.Fatalf("type = %q, want %q", ev.GetType(), RawLogEventType)
    }
    data, ok := ev.GetData().(map[string]any)
    if !ok {
        t.Fatalf("data not map[string]any: %T", ev.GetData())
    }
    dims, ok := data["dimensions"].(map[string]string)
    if !ok {
        t.Fatalf("dimensions not map[string]string: %T", data["dimensions"])
    }
    if dims["file"] != "/tmp/demo.log" || dims["regex"] != `ERROR payment_id=(\d+)` || dims["line_number"] != "7" {
        t.Fatalf("unexpected dimensions: %+v", dims)
    }
    metrics, ok := data["metrics"].(map[string]float64)
    if !ok {
        t.Fatalf("metrics not map[string]float64: %T", data["metrics"])
    }
    if metrics["matches_count"] != 1 {
        t.Fatalf("matches_count = %v, want 1", metrics["matches_count"])
    }
    fields, ok := data["fields"].(map[string]string)
    if !ok || fields["1"] != "12345" {
        t.Fatalf("unexpected fields: %+v", fields)
    }
    if data["raw"] != "ERROR payment_id=12345 amount=99.9" {
        t.Fatalf("raw line mismatch: %q", data["raw"])
    }
}

func TestBuildRawLogEvent_NilCaptures(t *testing.T) {
    ev := BuildRawLogEvent("f", `^INFO$`, 1, nil, "INFO")
    data := ev.GetData().(map[string]any)
    if data["fields"] != nil {
        t.Fatalf("nil captures should pass through, got %+v", data["fields"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/opt/go/1.25.12/bin/go test ./tasks/keyword/... -v`
Expected: FAIL with `undefined: BuildRawLogEvent` / `undefined: RawLogEventType`.

- [ ] **Step 3: Implement `tasks/keyword/raw_event.go`**

```go
// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

package keyword

import (
    "strconv"

    "github.com/abrance/monitorbeat/define"
)

// RawLogEventType 是 raw_log 事件在 define.Event.GetType() 上的固定字符串。
//
// 对外（output / 下游消费方）按此常量识别，不要在其它位置硬编码字符串。
const RawLogEventType = "raw_log"

// BuildRawLogEvent 构造一个 raw_log 事件。
//
// 负载 schema：
//   - dimensions.file:        源文件绝对路径
//   - dimensions.regex:       命中用的正则原文
//   - dimensions.line_number: 1-based 行号（字符串，与现有 probe 维度类型对齐）
//   - metrics.matches_count:  始终 1
//   - fields:                 capture map（命名 group 用名字，匿名 group 用 "1"/"2"/…）
//   - raw:                    原始行（不含行尾 \n）
func BuildRawLogEvent(file, pattern string, lineNo int, captures map[string]string, raw string) define.Event {
    return define.NewEvent(RawLogEventType, map[string]any{
        "dimensions": map[string]string{
            "file":        file,
            "regex":       pattern,
            "line_number": strconv.Itoa(lineNo),
        },
        "metrics": map[string]float64{
            "matches_count": 1,
        },
        "fields": captures,
        "raw":    raw,
    })
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `/opt/go/1.25.12/bin/go test ./tasks/keyword/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /opt/mystorage/github/monitorbeat
git add tasks/keyword/raw_event.go tasks/keyword/raw_event_test.go
git commit -m "feat(tasks/keyword): add raw_log event helper"
```

---

## Task C: `internal/regexp/extract` capture helper

**Files:**
- Create: `internal/regexp/extract/extract.go`
- Create: `internal/regexp/extract/extract_test.go`

- [ ] **Step 1: Write failing tests**

```go
package extract

import (
    "reflect"
    "regexp"
    "testing"
)

func TestExtract_NamedGroups(t *testing.T) {
    re := regexp.MustCompile(`ERROR payment_id=(?P<pid>\d+) amount=(?P<amt>\d+\.\d+)`)
    got, ok := Extract("ERROR payment_id=12345 amount=99.9", re)
    if !ok {
        t.Fatal("expected match")
    }
    want := map[string]string{"pid": "12345", "amt": "99.9"}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("got %+v, want %+v", got, want)
    }
}

func TestExtract_UnnamedGroups(t *testing.T) {
    re := regexp.MustCompile(`ERROR payment_id=(\d+) amount=(\d+\.\d+)`)
    got, ok := Extract("ERROR payment_id=12345 amount=99.9", re)
    if !ok {
        t.Fatal("expected match")
    }
    want := map[string]string{"1": "12345", "2": "99.9"}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("got %+v, want %+v", got, want)
    }
}

func TestExtract_MixedNamedAndUnnamed(t *testing.T) {
    re := regexp.MustCompile(`(?P<level>\w+) payment_id=(\d+)`)
    got, ok := Extract("ERROR payment_id=42", re)
    if !ok {
        t.Fatal("expected match")
    }
    want := map[string]string{"level": "ERROR", "2": "42"}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("got %+v, want %+v", got, want)
    }
}

func TestExtract_NoMatch(t *testing.T) {
    re := regexp.MustCompile(`ERROR payment_id=(\d+)`)
    if got, ok := Extract("INFO hello world", re); ok || got != nil {
        t.Fatalf("expected no match, got ok=%v v=%+v", ok, got)
    }
}

func TestExtract_ZeroCaptureMatch(t *testing.T) {
    re := regexp.MustCompile(`^INFO$`)
    got, ok := Extract("INFO", re)
    if !ok {
        t.Fatal("expected match")
    }
    if len(got) != 0 {
        t.Fatalf("expected empty map, got %+v", got)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/opt/go/1.25.12/bin/go test ./internal/regexp/extract/... -v`
Expected: FAIL with `undefined: Extract`.

- [ ] **Step 3: Implement `internal/regexp/extract/extract.go`**

```go
// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

// Package extract 把 regexp 命中后的 named / unnamed capture 组展开为 map[string]string。
//
// 命名 group：key = group 名。
// 匿名 group：key = "1" / "2" / … （与 Go regexp 标准 SubexpIndex 一致）。
//
// 全为匿名 group 时，map 用字符串数字 key；零 capture 命中时返回空 map（长度 0）。
package extract

import (
    "regexp"
    "strconv"
)

// Extract 在 line 上跑 re；命中返回 (captures, true)，未命中返回 (nil, false)。
func Extract(line string, re *regexp.Regexp) (map[string]string, bool) {
    m := re.FindStringSubmatch(line)
    if m == nil {
        return nil, false
    }
    names := re.SubexpNames()
    out := make(map[string]string, len(m))
    anyNamed := false
    for i, name := range names {
        if i == 0 {
            continue
        }
        if name != "" {
            anyNamed = true
            out[name] = m[i]
        }
    }
    if !anyNamed {
        // 全部匿名：用 "1"/"2"/… 作 key，便于下游 json 序列化时稳定。
        for i := 1; i < len(m); i++ {
            out[strconv.Itoa(i)] = m[i]
        }
    }
    return out, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `/opt/go/1.25.12/bin/go test ./internal/regexp/extract/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /opt/mystorage/github/monitorbeat
git add internal/regexp/extract/
git commit -m "feat(internal/regexp): add capture group extraction helper"
```

---

## Task D: `internal/input/file` harvester

**Files:**
- Create: `internal/input/file/doc.go`
- Create: `internal/input/file/harvester.go`
- Create: `internal/input/file/harvester_test.go`

This task creates the single-file tail reader. The Harvester struct exposes a synchronous `ReadLine(ctx) (string, error)` API: callers loop, each call blocks until a new line is available or ctx is cancelled. Encoding (utf-8 / gb18030) is handled via `transform.NewReader` from `golang.org/x/text`.

- [ ] **Step 1: Write failing test**

`internal/input/file/harvester_test.go`:

```go
package file

import (
    "context"
    "os"
    "path/filepath"
    "testing"
    "time"
)

func writeLines(t *testing.T, path string, lines []string) {
    t.Helper()
    f, err := os.Create(path)
    if err != nil {
        t.Fatal(err)
    }
    defer f.Close()
    for _, l := range lines {
        if _, err := f.WriteString(l + "\n"); err != nil {
            t.Fatal(err)
        }
    }
}

func TestHarvester_ReadLine_FromBegin(t *testing.T) {
    dir := t.TempDir()
    p := filepath.Join(dir, "a.log")
    writeLines(t, p, []string{"alpha", "beta"})

    h, err := New(HarvesterConfig{File: p, Encoding: "utf-8", FromBegin: true, BufferSize: 1024})
    if err != nil {
        t.Fatal(err)
    }
    defer h.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    got, err := h.ReadLine(ctx)
    if err != nil || got != "alpha" {
        t.Fatalf("first line: got=%q err=%v", got, err)
    }
    got, err = h.ReadLine(ctx)
    if err != nil || got != "beta" {
        t.Fatalf("second line: got=%q err=%v", got, err)
    }
}

func TestHarvester_ReadLine_AppendAndCancel(t *testing.T) {
    dir := t.TempDir()
    p := filepath.Join(dir, "b.log")
    writeLines(t, p, []string{"first"})

    h, err := New(HarvesterConfig{File: p, Encoding: "utf-8", FromBegin: true, BufferSize: 1024})
    if err != nil {
        t.Fatal(err)
    }
    defer h.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    if got, err := h.ReadLine(ctx); err != nil || got != "first" {
        t.Fatalf("first: got=%q err=%v", got, err)
    }

    // 在另一个 goroutine 里追加一行
    go func() {
        time.Sleep(50 * time.Millisecond)
        f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0644)
        defer f.Close()
        f.WriteString("second\n")
    }()

    got, err := h.ReadLine(ctx)
    if err != nil || got != "second" {
        t.Fatalf("after append: got=%q err=%v", got, err)
    }
}

func TestHarvester_ReadLine_ContextCancel(t *testing.T) {
    dir := t.TempDir()
    p := filepath.Join(dir, "c.log")
    writeLines(t, p, nil)

    h, err := New(HarvesterConfig{File: p, Encoding: "utf-8", FromBegin: true, BufferSize: 1024})
    if err != nil {
        t.Fatal(err)
    }
    defer h.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
    defer cancel()

    if _, err := h.ReadLine(ctx); err == nil {
        t.Fatal("expected error on ctx cancel")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/opt/go/1.25.12/bin/go test ./internal/input/file/... -v`
Expected: FAIL with `undefined: HarvesterConfig` / `undefined: New`.

- [ ] **Step 3: Implement `internal/input/file/doc.go`**

```go
// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

// Package file 实现单文件 append-only tail harvester。
//
// P1.2 MVP 限定：
//   - 单文件路径
//   - 同步阻塞 ReadLine API（ctx 可中断）
//   - 编码仅 utf-8 / gb18030
//   - 不做 offset 持久化、不做轮转检测（每次启动按 FromBegin 决定从 0 或 EOF 开始）
//
// 不依赖 libgse。
package file
```

- [ ] **Step 4: Implement `internal/input/file/harvester.go`**

```go
// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

package file

import (
    "bufio"
    "context"
    "errors"
    "fmt"
    "io"
    "os"

    "golang.org/x/text/encoding"
    "golang.org/x/text/encoding/htmlindex"
)

// HarvesterConfig 控制单文件 tail 行为。
type HarvesterConfig struct {
    File       string
    Encoding   string // "utf-8" | "gb18030"
    FromBegin  bool
    BufferSize int
}

// Harvester 单文件 tail reader。
//
// 设计要点：
//   - 内部有且只有一个 reader goroutine，所有 ReadLine 调用通过 channel 收结果
//   - ctx 取消会让 reader goroutine 在下一次 ReadString 阻塞时退出（poll 间隔 100ms）
//   - ReadLine 串行调用即可，不要并发
type Harvester struct {
    cfg    HarvesterConfig
    file   *os.File
    reader *bufio.Reader
    enc    encoding.Encoding

    lineCh chan string
    done   chan struct{}
}

// New 打开文件并按 FromBegin 定位读起点。
func New(cfg HarvesterConfig) (*Harvester, error) {
    if cfg.File == "" {
        return nil, errors.New("harvester: empty file path")
    }
    if cfg.BufferSize <= 0 {
        cfg.BufferSize = 64 * 1024
    }
    enc, err := lookupEncoding(cfg.Encoding)
    if err != nil {
        return nil, err
    }
    f, err := os.Open(cfg.File)
    if err != nil {
        return nil, fmt.Errorf("harvester open %s: %w", cfg.File, err)
    }
    if !cfg.FromBegin {
        if _, err := f.Seek(0, io.SeekEnd); err != nil {
            f.Close()
            return nil, fmt.Errorf("harvester seek end: %w", err)
        }
    }
    var r io.Reader = f
    if enc != nil {
        r = enc.NewDecoder().Reader(f)
    }
    h := &Harvester{
        cfg:    cfg,
        file:   f,
        reader: bufio.NewReaderSize(r, cfg.BufferSize),
        enc:    enc,
        lineCh: make(chan string, 16),
        done:   make(chan struct{}),
    }
    go h.loop()
    return h, nil
}

// loop 是 harvester 唯一的 reader goroutine。
//
// 用 ctx-like 行为：Close 时通过 close(done) 通知退出；为了避免 ReadString 永久
// 阻塞，这里采用"短轮询 + ReadString 立即返回"的策略：每次 ReadString 返回 EOF
// 就 sleep 100ms 再继续；这样 Close 通知到时，最多延迟 100ms 即可退出。
func (h *Harvester) loop() {
    defer close(h.lineCh)
    for {
        select {
        case <-h.done:
            return
        default:
        }
        line, err := h.reader.ReadString('\n')
        if len(line) > 0 {
            // 去掉行尾 \n（保留 \r 兼容 CRLF）
            if n := len(line); n > 0 && line[n-1] == '\n' {
                line = line[:n-1]
            }
            // 同步发送：lineCh 有缓冲，goroutine 不会阻塞
            h.lineCh <- line
        }
        if err != nil {
            if err != io.EOF {
                // 真正的读错误：log 然后退出（由 Close 清理）
                return
            }
            // EOF：等文件追加；用 close(done) 检测取消
            select {
            case <-h.done:
                return
            case <-sleepChan(100):
            }
            // 重新打开底层文件让 bufio 看到新内容；简化处理：直接 Seek+Reset
            if _, serr := h.file.Seek(0, io.SeekCurrent); serr == nil {
                h.reader = bufio.NewReaderSize(h.underlyingReader(), h.cfg.BufferSize)
            }
        }
    }
}

func (h *Harvester) underlyingReader() io.Reader {
    var r io.Reader = h.file
    if h.enc != nil {
        r = h.enc.NewDecoder().Reader(h.file)
    }
    return r
}

// sleepChan 返回一个 N 毫秒后关闭的 channel，等价 time.After 但不分配 timer。
func sleepChan(ms int) <-chan struct{} {
    ch := make(chan struct{})
    go func() {
        // 用 time.Sleep 简单实现；P2 可换成 runtime 时钟
        timer := newTimer(ms)
        <-timer.C
        close(ch)
    }()
    return ch
}

// newTimer 返回一个最小 timer 抽象，便于后续替换实现。
func newTimer(ms int) interface{ C <-chan time.Time } {
    return time.NewTimer(time.Duration(ms) * time.Millisecond)
}

// ReadLine 阻塞读一行（不含行尾 \n）；ctx 取消时返回 ctx.Err()，Close 后返回 io.EOF。
func (h *Harvester) ReadLine(ctx context.Context) (string, error) {
    select {
    case line, ok := <-h.lineCh:
        if !ok {
            return "", io.EOF
        }
        return line, nil
    case <-ctx.Done():
        return "", ctx.Err()
    }
}

// Close 通知 reader goroutine 退出并关闭文件。
func (h *Harvester) Close() error {
    select {
    case <-h.done:
        // 已经关闭
    default:
        close(h.done)
    }
    if h.file == nil {
        return nil
    }
    return h.file.Close()
}

func lookupEncoding(name string) (encoding.Encoding, error) {
    switch name {
    case "", "utf-8", "utf8":
        return nil, nil // nil 表示透传，无需 decoder
    case "gb18030":
        return htmlindex.Get("gb18030")
    }
    return nil, fmt.Errorf("harvester: unsupported encoding %q", name)
}
```

NOTE on the sleepChan helper: it's intentionally abstracted so future phases can swap in fsnotify / inotify without touching ReadLine API. For MVP, `time.NewTimer` is sufficient and well-tested.

Imports added: `"time"`.

- [ ] **Step 5: Add `golang.org/x/text` dependency**

Run: `/opt/go/1.25.12/bin/go get golang.org/x/text`
Run: `/opt/go/1.25.12/bin/go mod tidy`

- [ ] **Step 6: Run tests to verify they pass**

Run: `/opt/go/1.25.12/bin/go test ./internal/input/file/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /opt/mystorage/github/monitorbeat
git add internal/input/file/ go.mod go.sum
git commit -m "feat(internal/input/file): add single-file tail harvester"
```

---

## Task E: `scheduler/keyword` scheduler

**Files:**
- Create: `scheduler/keyword/scheduler.go`
- Create: `scheduler/keyword/scheduler_test.go`

This scheduler is intentionally minimal: each registered task gets one long-lived goroutine that calls `task.Run(ctx, eventChan)` and waits for it to return. Unlike the daemon time-heap, it does NOT re-trigger on a period; the keyword task itself is responsible for blocking in its input loop.

- [ ] **Step 1: Write failing test**

```go
package keyword

import (
    "context"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/abrance/monitorbeat/define"
)

type fakeTask struct {
    tasks.BaseTask
    cfg     *fakeCfg
    started atomic.Int32
    onRun   func(ctx context.Context, e chan<- define.Event)
    done    chan struct{}
}

type fakeCfg struct {
    tasks.BaseTaskParam
    typ string
}

func (f *fakeCfg) GetType() string { return f.typ }

func newFakeTask(typ string, onRun func(ctx context.Context, e chan<- define.Event)) *fakeTask {
    t := &fakeTask{
        cfg:   &fakeCfg{BaseTaskParam: tasks.BaseTaskParam{TaskID: 1}, typ: typ},
        onRun: onRun,
        done:  make(chan struct{}),
    }
    t.SetConfig(t.cfg)
    t.SetStatus(define.StatusReady)
    return t
}

func (f *fakeTask) Run(ctx context.Context, e chan<- define.Event) {
    f.started.Add(1)
    defer close(f.done)
    f.onRun(ctx, e)
}

func TestScheduler_StartsAndStopsTask(t *testing.T) {
    ch := make(chan define.Event, 4)
    s := New(ch, &fakeCfg{}).(*Scheduler)
    s.Add(newFakeTask("keyword", func(ctx context.Context, e chan<- define.Event) {
        <-ctx.Done()
    }))
    ctx, cancel := context.WithCancel(context.Background())
    if err := s.Start(ctx); err != nil {
        t.Fatal(err)
    }
    cancel()
    s.Wait()
    if s.Count() != 1 {
        t.Fatalf("count = %d", s.Count())
    }
}

func TestScheduler_IsDaemon(t *testing.T) {
    s := New(make(chan define.Event), &fakeCfg{}).(*Scheduler)
    if s.IsDaemon() {
        t.Fatal("keyword scheduler should not be daemon")
    }
}

func TestScheduler_StopExitsRun(t *testing.T) {
    ch := make(chan define.Event, 1)
    s := New(ch, &fakeCfg{}).(*Scheduler)
    tk := newFakeTask("keyword", func(ctx context.Context, e chan<- define.Event) {
        select {
        case <-ctx.Done():
        case <-time.After(2 * time.Second):
            t.Error("timeout waiting for ctx cancel")
        }
    })
    s.Add(tk)
    ctx := context.Background()
    if err := s.Start(ctx); err != nil {
        t.Fatal(err)
    }
    // 等到 Run 进入
    for i := 0; i < 100; i++ {
        if tk.started.Load() == 1 {
            break
        }
        time.Sleep(10 * time.Millisecond)
    }
    s.Stop()
    select {
    case <-tk.done:
    case <-time.After(2 * time.Second):
        t.Fatal("task did not exit after Stop")
    }
    s.Wait()
}
```

NOTE: this test imports `tasks` package and uses `tasks.BaseTask`. Since `scheduler/keyword` is a sibling package, the import path is `github.com/abrance/monitorbeat/tasks`. The fakeTask embeds `tasks.BaseTask`.

- [ ] **Step 2: Run test to verify it fails**

Run: `/opt/go/1.25.12/bin/go test ./scheduler/keyword/... -v`
Expected: FAIL with `undefined: Scheduler` / `undefined: New`.

- [ ] **Step 3: Implement `scheduler/keyword/scheduler.go`**

```go
// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

// Package keyword 提供 keyword 日志采集任务的调度器。
//
// 与 daemon 时间堆不同：
//   - 每个 task 一个长生命周期 goroutine，调用 task.Run 后阻塞等待退出
//   - 不做周期性触发；task 自己负责循环读文件并写事件
//   - Stop / ctx cancel 都会立即让所有 task 的 Run 返回
//
// IsDaemon 返回 false，与 checker 一致（不抢占 daemon 调度位）。
package keyword

import (
    "context"
    "sync"

    "github.com/emirpasic/gods/maps/treemap"

    "github.com/abrance/monitorbeat/define"
    "github.com/abrance/monitorbeat/internal/logging"
)

// Scheduler 是 keyword 任务的专用调度器。
type Scheduler struct {
    *define.BaseScheduler

    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
    tasks  *treemap.Map // key: ident
    mu     sync.RWMutex
}

// New 构造 keyword 调度器。
func New(eventChan chan<- define.Event, _ define.Config) define.Scheduler {
    return &Scheduler{
        BaseScheduler: &define.BaseScheduler{
            EventChan: eventChan,
        },
        tasks: treemap.NewWithStringComparator(),
    }
}

// Add 注册一个 task。
func (s *Scheduler) Add(task define.Task) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.tasks.Put(task.GetConfig().GetIdent(), task)
}

// IsDaemon keyword 调度器不是 daemon 调度器（区别于时间堆）。
func (s *Scheduler) IsDaemon() bool { return false }

// Count 已注册 task 数。
func (s *Scheduler) Count() int {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.tasks.Size()
}

// Start 启动所有 task 的 Run goroutine。
func (s *Scheduler) Start(ctx context.Context) error {
    s.ctx, s.cancel = context.WithCancel(ctx)
    s.Status = define.StatusRunning

    s.mu.RLock()
    tasks := make([]define.Task, 0, s.tasks.Size())
    iter := s.tasks.Iterator()
    for iter.Next() {
        tasks = append(tasks, iter.Value().(define.Task))
    }
    s.mu.RUnlock()

    for _, task := range tasks {
        s.wg.Add(1)
        go s.runTask(s.ctx, task)
    }
    return nil
}

func (s *Scheduler) runTask(ctx context.Context, task define.Task) {
    defer s.wg.Done()
    defer func() {
        if r := recover(); r != nil {
            logging.Error("keyword scheduler task panic", "ident", task.GetConfig().GetIdent(), "panic", r)
        }
    }()
    task.Run(ctx, s.EventChan)
}

// Stop 取消 ctx，让所有 task.Run 返回。
func (s *Scheduler) Stop() {
    logging.Info("keyword scheduler stop")
    s.Status = define.StatusFinished
    if s.cancel != nil {
        s.cancel()
    }
}

// Wait 阻塞至所有 task goroutine 退出。
func (s *Scheduler) Wait() {
    s.wg.Wait()
    s.Status = define.StatusFinished
}

// Reload 热替换 task 列表：checker / keyword 这类一次性 / 长驻调度器
// 由调用方按 "Stop + Wait + New + Add + Start" 顺序重启；这里只更新内存。
func (s *Scheduler) Reload(_ context.Context, conf define.Config, tasks []define.Task) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.Config = conf
    s.tasks.Clear()
    for _, t := range tasks {
        s.tasks.Put(t.GetConfig().GetIdent(), t)
    }
    logging.Info("keyword scheduler reloaded", "tasks", len(tasks))
    return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `/opt/go/1.25.12/bin/go test ./scheduler/keyword/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /opt/mystorage/github/monitorbeat
git add scheduler/keyword/
git commit -m "feat(scheduler/keyword): add long-running scheduler for keyword tasks"
```

---

## Task F: `tasks/keyword/keyword.go` task wiring + builder

**Files:**
- Create: `tasks/keyword/keyword.go`
- Modify: `cmd/monitorbeat/main.go` — blank-import `scheduler/keyword` and `tasks/keyword` packages.

- [ ] **Step 1: Write failing test**

Append `tasks/keyword/keyword_test.go` (covers the end-to-end fake-file scenario):

```go
package keyword

import (
    "context"
    "os"
    "path/filepath"
    "regexp"
    "testing"
    "time"

    "github.com/abrance/monitorbeat/configs"
    "github.com/abrance/monitorbeat/define"
    "github.com/abrance/monitorbeat/internal/input/file"
)

func TestGather_EndToEnd_EmitsRawLogEvents(t *testing.T) {
    dir := t.TempDir()
    logPath := filepath.Join(dir, "demo.log")
    if err := os.WriteFile(logPath, []byte("ERROR payment_id=1 amount=1.5\n"), 0644); err != nil {
        t.Fatal(err)
    }

    cfg := &configs.KeywordConfig{
        BaseTaskParam: configs.BaseTaskParam{TaskID: 7, Ident: "keyword:7", Enabled: true},
        File:          logPath,
        Pattern:       `ERROR payment_id=(\d+) amount=(\d+\.\d+)`,
        Encoding:      "utf-8",
        BufferSize:    1024,
    }
    fb := false
    cfg.FromBegin = &fb // skip the first line we already wrote, only consume appended
    _ = cfg.Clean()

    g := New(cfg).(*Gather)

    ch := make(chan define.Event, 8)
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()

    go g.Run(ctx, ch)

    // 等一下让 harvester 读到 EOF，然后追加一行
    time.Sleep(100 * time.Millisecond)
    f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
    if err != nil {
        t.Fatal(err)
    }
    if _, err := f.WriteString("ERROR payment_id=42 amount=99.5\n"); err != nil {
        t.Fatal(err)
    }
    f.Close()

    select {
    case ev := <-ch:
        data := ev.GetData().(map[string]any)
        if ev.GetType() != RawLogEventType {
            t.Fatalf("event type = %q", ev.GetType())
        }
        fields := data["fields"].(map[string]string)
        if fields["1"] != "42" || fields["2"] != "99.5" {
            t.Fatalf("unexpected fields: %+v", fields)
        }
        dims := data["dimensions"].(map[string]string)
        if dims["file"] != logPath {
            t.Fatalf("dim file = %q", dims["file"])
        }
    case <-time.After(2 * time.Second):
        t.Fatal("no event received within timeout")
    }
    cancel()
    // ensure no panic on shutdown
}

func TestGather_InvalidConfigRejected(t *testing.T) {
    cfg := &configs.KeywordConfig{}
    if _, err := builder(cfg); err == nil {
        t.Fatal("expected error for empty file/pattern")
    }
}

func TestExtractIntegration(t *testing.T) {
    re := regexp.MustCompile(`ERROR payment_id=(\d+)`)
    caps, ok := extractLine("ERROR payment_id=7", re)
    if !ok || caps["1"] != "7" {
        t.Fatalf("unexpected: ok=%v caps=%+v", ok, caps)
    }
}

// extractLine 是 keyword.go 里的小封装，便于单元测试。
func extractLine(line string, re *regexp.Regexp) (map[string]string, bool) {
    return ExtractLine(line, re)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/opt/go/1.25.12/bin/go test ./tasks/keyword/... -v`
Expected: FAIL with `undefined: Gather` / `undefined: builder` / `undefined: ExtractLine`.

- [ ] **Step 3: Implement `tasks/keyword/keyword.go`**

```go
// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

// Package keyword 实现 monitorbeat 的日志关键字采集 task。
//
// P1.2 MVP（raw_log only）：
//   - 单文件 tail + regex capture
//   - 每命中一行发一条 raw_log 事件（matches_count=1）
//   - 不做周期聚合，不做 offset 持久化
//
// 行为对齐 bkmonitorbeat/tasks/keyword/raw_log 分支，差异：
//   - 去 libgse，事件走 internal/output（已通过 define.Event 通道）
//   - 用 scheduler/keyword 长驻调度器（不是 daemon 时间堆）
package keyword

import (
    "context"
    "fmt"
    "regexp"

    "github.com/abrance/monitorbeat/configs"
    "github.com/abrance/monitorbeat/define"
    "github.com/abrance/monitorbeat/internal/input/file"
    "github.com/abrance/monitorbeat/tasks"
)

func init() {
    tasks.RegisterBuilder(define.ModuleKeyword, func(tc define.TaskConfig) (define.Task, error) {
        return builder(tc)
    })
}

// builder 暴露给测试；init() 直接调它。
func builder(tc define.TaskConfig) (define.Task, error) {
    cfg, ok := tc.(*configs.KeywordConfig)
    if !ok {
        return nil, fmt.Errorf("keyword: config type mismatch: %T", tc)
    }
    if cfg.File == "" {
        return nil, fmt.Errorf("keyword: file is required")
    }
    if cfg.Pattern == "" {
        return nil, fmt.Errorf("keyword: pattern is required")
    }
    if _, err := regexp.Compile(cfg.Pattern); err != nil {
        return nil, fmt.Errorf("keyword: bad pattern: %w", err)
    }
    return New(cfg), nil
}

// Gather 是 keyword task 的运行时实例。
type Gather struct {
    tasks.BaseTask
    cfg *configs.KeywordConfig
}

// New 构造可运行的 keyword task。
func New(cfg *configs.KeywordConfig) define.Task {
    g := &Gather{cfg: cfg}
    g.SetConfig(cfg)
    g.SetStatus(define.StatusReady)
    return g
}

// Run 阻塞读文件 → 命中正则 → 发 raw_log 事件；ctx 取消即退出。
//
// 实现要点：
//   - 同步调用 harvester.ReadLine，单 goroutine 内串行
//   - regex 编译失败由 builder 拦住，Run 内不再校验
//   - 写事件走 select + ctx.Done，避免 scheduler 已关而 Run 还在塞 channel
func (g *Gather) Run(ctx context.Context, e chan<- define.Event) {
    re := regexp.MustCompile(g.cfg.Pattern)
    h, err := file.New(file.HarvesterConfig{
        File:       g.cfg.File,
        Encoding:   g.cfg.Encoding,
        FromBegin:  g.cfg.FromBegin == nil || *g.cfg.FromBegin,
        BufferSize: g.cfg.BufferSize,
    })
    if err != nil {
        g.emitError(ctx, e, err)
        return
    }
    defer h.Close()

    lineNo := 0
    for {
        line, err := h.ReadLine(ctx)
        if err != nil {
            if ctx.Err() != nil {
                return
            }
            // 半行 / EOF：直接结束本 task，由 scheduler 决定是否重启
            return
        }
        lineNo++
        captures, ok := ExtractLine(line, re)
        if !ok {
            continue
        }
        ev := BuildRawLogEvent(g.cfg.File, g.cfg.Pattern, lineNo, captures, line)
        select {
        case e <- ev:
        case <-ctx.Done():
            return
        }
    }
}

func (g *Gather) emitError(ctx context.Context, e chan<- define.Event, cause error) {
    ev := define.NewEvent(RawLogEventType, map[string]any{
        "dimensions": map[string]string{
            "file":        g.cfg.File,
            "regex":       g.cfg.Pattern,
            "line_number": "0",
        },
        "metrics": map[string]float64{"matches_count": 0},
        "fields":  map[string]string{},
        "raw":     "",
        "error":   cause.Error(),
    })
    select {
    case e <- ev:
    case <-ctx.Done():
    }
}

// ExtractLine 是 raw_event.go 里 Extract 的薄封装，方便 keyword 包内部直接用。
func ExtractLine(line string, re *regexp.Regexp) (map[string]string, bool) {
    m := re.FindStringSubmatch(line)
    if m == nil {
        return nil, false
    }
    names := re.SubexpNames()
    out := make(map[string]string, len(m))
    anyNamed := false
    for i, name := range names {
        if i == 0 {
            continue
        }
        if name != "" {
            anyNamed = true
            out[name] = m[i]
        }
    }
    if !anyNamed {
        for i := 1; i < len(m); i++ {
            out[fmt.Sprintf("%d", i)] = m[i]
        }
    }
    return out, true
}
```

NOTE: `ExtractLine` lives in `tasks/keyword` so the package stays self-contained (callers don't have to know about `internal/regexp/extract`). It is essentially a duplicate of `extract.Extract`; if extraction logic grows, fold back to the shared package in a later phase.

- [ ] **Step 4: Wire blank imports in `cmd/monitorbeat/main.go`**

Add to the blank-import block (after the existing basereport/http/ping/tcp/udp imports):

```go
    _ "github.com/abrance/monitorbeat/scheduler/keyword"
    _ "github.com/abrance/monitorbeat/tasks/keyword"
```

Note: keyword scheduler is selected via `define.ModuleKeyword` task type at runtime — currently `cmd/monitorbeat/main.go` uses only `daemon.New`. This MVP intentionally does NOT auto-pick `keyword` scheduler; we add a small dispatch in this step (see code below).

Modify the `sched := daemon.New(...)` + `tasksList` build section:

```go
// 现有的 daemon scheduler
daemonSched := daemon.New(eng.Chan(), cfg)
keywordSched := keyword.New(eng.Chan(), cfg)

tasksList, skipped := buildAllTasks(cfg)
for _, t := range tasksList {
    switch t.GetConfig().GetType() {
    case define.ModuleKeyword:
        keywordSched.Add(t)
        logging.Info("task registered", "type", t.GetConfig().GetType(), "ident", t.GetConfig().GetIdent(), "scheduler", "keyword")
    default:
        daemonSched.Add(t)
        logging.Info("task registered", "type", t.GetConfig().GetType(), "ident", t.GetConfig().GetIdent(), "scheduler", "daemon")
    }
}

if err := daemonSched.Start(ctx); err != nil { ... }
if err := keywordSched.Start(ctx); err != nil { ... }

// shutdown 顺序：先 keyword（让 task.Run 退出），再 daemon
<-ctx.Done()
keywordSched.Stop()
daemonSched.Stop()
keywordSched.Wait()
daemonSched.Wait()
```

Also extend the import list:

```go
    "github.com/abrance/monitorbeat/scheduler/keyword"
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `/opt/go/1.25.12/bin/go test ./tasks/keyword/... ./configs/... ./scheduler/keyword/... ./internal/input/file/... ./internal/regexp/extract/... -v`
Expected: all PASS.

Then run whole-repo: `/opt/go/1.25.12/bin/go build ./...` and `/opt/go/1.25.12/bin/go vet ./...`
Expected: both PASS / clean.

- [ ] **Step 6: Commit**

```bash
cd /opt/mystorage/github/monitorbeat
git add tasks/keyword/keyword.go tasks/keyword/keyword_test.go cmd/monitorbeat/main.go
git commit -m "feat(tasks/keyword): wire raw_log Gather + dispatch in main"
```

---

## Task G: End-to-end demo

**Files:**
- Create: `configs/p1_keyword.yaml`

- [ ] **Step 1: Write demo config**

`configs/p1_keyword.yaml`:

```yaml
# monitorbeat P1.2 keyword (raw_log) demo 配置
#
# 准备 demo 日志源（另起终端）：
#   rm -f /tmp/demo.log && touch /tmp/demo.log
#
# 持续追加（任选一种）：
#   a) while true; do echo "ERROR payment_id=$((RANDOM%1000)) amount=$(awk 'BEGIN{printf \"%.2f\",rand()*100}')"; sleep 0.5; done >> /tmp/demo.log
#   b) 或一次性追加 5 行：
#        for i in 1 2 3 4 5; do echo "ERROR payment_id=$i amount=1.${i}"; done >> /tmp/demo.log
#
# 跑 daemon：
#   ./bin/monitorbeat -config configs/p1_keyword.yaml
#
# console 会输出 raw_log 事件，每条 fields 包含 payment_id / amount。
# 5s 内应能看到至少 1 条 raw_log 事件。

check_interval: 1s
event_buffer_size: 1024
admin_addr: ""

outputs:
  - type: console

keywords:
  - task_id: 2001
    enabled: true
    file: /tmp/demo.log
    pattern: 'ERROR payment_id=(\d+) amount=(\d+\.\d+)'
    encoding: utf-8
    buffer_size: 65536
    from_begin: true
```

- [ ] **Step 2: Build binary and run `-check`**

```bash
cd /opt/mystorage/github/monitorbeat
/opt/go/1.25.12/bin/go build -o bin/monitorbeat ./cmd/monitorbeat
./bin/monitorbeat -config configs/p1_keyword.yaml -check
```

Expected: prints `config OK`, exit 0.

- [ ] **Step 3: Live smoke test**

Open a second terminal and start appending lines (using method (b) above for a one-shot):

```bash
rm -f /tmp/demo.log && touch /tmp/demo.log
for i in 1 2 3 4 5; do echo "ERROR payment_id=$i amount=1.${i}"; done >> /tmp/demo.log
```

Then in the first terminal:

```bash
cd /opt/mystorage/github/monitorbeat
timeout --signal=TERM --kill-after=2s 6s ./bin/monitorbeat -config configs/p1_keyword.yaml > /tmp/keyword.stdout 2> /tmp/keyword.stderr
```

After the run, verify `/tmp/keyword.stdout` contains:

- A startup line `task registered ... scheduler=keyword`
- At least 5 `raw_log` events, each with `dimensions.file=/tmp/demo.log` and `fields.1=1..5`, `fields.2=1.1..1.5`
- A clean shutdown line `monitorbeat shutting down`

```bash
grep -c '"type":"raw_log"' /tmp/keyword.stdout   # → 5
grep '"type":"raw_log"' /tmp/keyword.stdout | head -n 1
```

- [ ] **Step 4: Commit (demo only — no binary)**

```bash
cd /opt/mystorage/github/monitorbeat
git add configs/p1_keyword.yaml
git commit -m "feat(configs): add p1_keyword demo config (raw_log)"
```

---

## Task H: Final P1.2 verification

**Files:**
- Modify: `README.md` — update status table + verification snapshot
- Modify: `docs/superpowers/plans/2026-07-15-p1-keyword-implementation.md` — mark this snapshot table Complete

- [ ] **Step 1: Whole-repo verification**

```bash
cd /opt/mystorage/github/monitorbeat
/opt/go/1.25.12/bin/go build ./...           # PASS
/opt/go/1.25.12/bin/go vet ./...             # clean
/opt/go/1.25.12/bin/go test ./...            # all PASS
/opt/go/1.25.12/bin/gofmt -l tasks/keyword configs scheduler/keyword internal/input/file internal/regexp/extract  # empty
/opt/go/1.25.12/bin/go mod tidy              # exit 0, no diff
```

- [ ] **Step 2: Update README status table**

Flip P1.2 status to ✅. Append a verification snapshot block mirroring P1.1 (build/vet/test all green + gofmt clean + smoke test evidence).

- [ ] **Step 3: Update plan snapshot table**

In the snapshot at the top of this plan doc, mark A–H all Complete with one-line evidence each. Keep the file otherwise intact (append-only edit).

- [ ] **Step 4: Commit**

```bash
cd /opt/mystorage/github/monitorbeat
git add README.md docs/superpowers/plans/2026-07-15-p1-keyword-implementation.md
git commit -m "docs: mark P1.2 keyword harvester complete"
```

---

## Risks & Notes

1. **`bufio.Reader` 非 goroutine-safe**：`internal/input/file/harvester.go` 在 `New` 启动**一个**长驻 reader goroutine（`loop()`），所有 `ReadLine` 通过 channel 收结果。这一约束意味着：
   - 不要在 harvester 之外再开 goroutine 调 `ReadLine`（会争抢 `bufio.Reader`）
   - 不要把 harvester 实例移到多个 task 共享
   - ctx 取消通过 `close(done)` 通知 loop 退出；EOF 时 sleep 100ms 轮询，所以关闭最坏延迟 100ms
2. **`scheduler/keyword` Reload 语义**：与 `checker` 一致——Reload 只更新内存 task 列表，不主动重启。Reload 时调用方应 Stop + Wait + New + Add + Start。这在 daemon Reload 链路里目前没串起来（P1.2 限定），由 main.go 启动时只跑一次。
3. **不持久化 offset**：每次启动按 `FromBegin` 决定读起点。生产场景必须接 storage；P1.2 明确不做。
4. **不分发编码异常**：GB18030 解析失败时 `transform.Reader` 会替换为 UTF-8 replacement char (`\uFFFD`)，不会让 task panic。如果要在事件层标记"编码异常"，留给 P2 单独做。
5. **regex 重复编译**：`tasks/keyword/keyword.go` 的 `Run` 入口编译一次 regex；不在 hot loop 重复编译。
6. **P1.1 smoke 跑过的 `bin/monitorbeat`**：本计划不复用旧二进制；G 步骤里 `go build -o bin/monitorbeat` 会覆盖。
7. **空白 imports 影响 main.go 二进制体积**：`scheduler/keyword` 不会被 P1.2 之外的任何路径引用；后续阶段如要彻底解耦可改用 build tag。
8. **harvester EOF + seek reset**：`loop()` 在 EOF 后用 `Seek(0, io.SeekCurrent)` 探活 + `Reset(bufio.Reader)`；这避免每次 EOF 都重新 `os.Open`（文件 fd 不变，文件名截断/重命名场景保留余地）。如果文件被 `rm + touch` 重建（demo smoke 用），fd 不再指向新文件，harvester 会读到 stale 内容。MVP 接受这一限制；P2 接 fsnotify 时处理。
9. **`sleepChan` 每次 new 一个 goroutine + timer**：MVP 可接受（轮询频率低）；P2 接 inotify 时移除。
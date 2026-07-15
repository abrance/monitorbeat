# P1.4 HTTP Output Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:test-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a new `output.http` to monitorbeat that POSTs events as JSON to a configured HTTP endpoint, with automatic fallback to a local JSONL file on network error, timeout, or non-2xx response. The fallback path is owned by the HTTP output itself (not shared with the global `file` output).

**Architecture:** A new `internal/output/http.go` implementing the existing `Output` interface (`Init/Publish/Close/Name`). The HTTP output registers itself via `init()` in the same package as the existing `console`/`file` outputs. Config lives in `configs/config.go` as a typed struct decoded from the `outputs:` map. The engine instantiates outputs via the existing factory; HTTP output is picked up automatically.

**Tech Stack:** Go 1.25, stdlib `net/http` (no extra deps), `gopkg.in/yaml.v3` (already in use), `httptest` in tests.

**Scope (MVP — explicit):**
- IN: URL, timeout, retry_max, headers, auth (bearer/basic), insecure_skip_verify, fallback_path, fallback_max_size, fallback_max_backups.
- OUT: gzip, batching, mTLS, OAuth2, circuit breaker, metrics export.
- NOT IN: shared fallback with `file` output (deliberate, see design §2).

---

## Current Progress Snapshot

Date: 2026-07-15 — P1.3 (script) finalized; P1.4 design approved, plan writing.

Local Go command: `/opt/go/1.25.12/bin/go` (reports `go1.25.12 linux/amd64`).

Toolchain env: `GOTOOLCHAIN=auto`, `GOMODCACHE=/home/xiaoy/go/pkg/mod`, `GOPROXY=https://goproxy.cn|https://goproxy.io|direct`.

Current verification (before P1.4 starts):
- `go build ./...` PASS
- `go vet ./...` clean
- `go test ./...` all green (19 packages)

Reference design: `docs/superpowers/specs/2026-07-15-p1-4-http-output-design.md`.

Progress by task:

| Task | Status |
|---|---|
| A. Confirm existing output contract (factory + config shape) | Pending |
| B. `HTTPOutputConfig` in `configs/config.go` + tests | Pending |
| C. `internal/output/http.go` implementation | Pending |
| D. `internal/output/http_test.go` (10 cases per spec §6) | Pending |
| E. End-to-end demo (`configs/p1_http.yaml` + live smoke) | Pending |
| F. Final P1.4 verification + README + commit | Pending |

## File Structure

```
configs/
├── config.go                         # MODIFY — add HTTPOutputConfig struct
├── config_test.go                    # MODIFY — add TestHTTPOutputConfig_Clean*
└── p1_http.yaml                      # CREATE — demo config

internal/output/
├── http.go                           # CREATE — HTTPOutput + fallback writer
└── http_test.go                      # CREATE — 10 test cases

cmd/monitorbeat/
└── main.go                           # (no change — outputs register via init() in same package)

README.md                             # MODIFY — P1.4 status + demo section
```

---

## Task A: Confirm existing output contract

**Goal:** Verify the exact shape of the `Output` interface, how `factory.go` looks up builders, and how the `outputs:` block is decoded from yaml. This avoids wrong assumptions in Tasks B/C.

- [ ] **Step 1: Read `internal/output/output.go`, `internal/output/factory.go`, `internal/output/file.go`, `internal/output/console.go` verbatim**

Use `codegraph_codegraph_explore` with query "Output interface factory.go RegisterBuilder Init Publish Close". Capture:
- Exact `Output` interface signature.
- `RegisterBuilder` signature + how it's invoked (each file's `init()` vs central switch in `factory.go`).
- How `outputs:` is decoded in `configs/config.go` (map[string]any, list of typed structs, etc.).
- How engine iterates outputs and what config shape gets passed to `Output.Init`.

- [ ] **Step 2: Record findings in this plan before editing**

Add a "Findings" block below before Task B. If findings conflict with the plan, edit the plan first, then implement.

---

## Task B: `HTTPOutputConfig` in `configs/config.go`

**Files:**
- Modify: `configs/config.go` — add `HTTPOutputConfig` + `HTTPAuthConfig` structs and `Clean()`.
- Modify: `configs/config_test.go` — add tests for the new type.

- [ ] **Step 1: Write failing tests first**

Insert into `configs/config_test.go`:

```go
func TestHTTPOutputConfig_CleanDefaults(t *testing.T) {
    c := &HTTPOutputConfig{URL: "http://127.0.0.1:9999/v1/events"}
    if err := c.Clean(); err != nil {
        t.Fatalf("clean: %v", err)
    }
    if c.Timeout <= 0 {
        t.Fatalf("timeout should default positive, got %v", c.Timeout)
    }
    if c.RetryMax < 0 {
        t.Fatalf("retry_max should be >= 0, got %d", c.RetryMax)
    }
    if c.FallbackMaxSize <= 0 {
        t.Fatalf("fallback_max_size should default positive, got %d", c.FallbackMaxSize)
    }
    if c.FallbackMaxBackups < 0 {
        t.Fatalf("fallback_max_backups should be >= 0, got %d", c.FallbackMaxBackups)
    }
}

func TestHTTPOutputConfig_RequiresURL(t *testing.T) {
    c := &HTTPOutputConfig{}
    if err := c.Clean(); err == nil {
        t.Fatal("expected error for empty url")
    }
}
```

- [ ] **Step 2: Verify FAIL**

Run: `/opt/go/1.25.12/bin/go test ./configs/... -run HTTPOutput -v`
Expected: FAIL with `undefined: HTTPOutputConfig`.

- [ ] **Step 3: Implement `HTTPOutputConfig`**

Add to `configs/config.go` (place next to existing output config types, e.g. after `FileOutputConfig`):

```go
// HTTPOutputConfig configures the http output.
type HTTPOutputConfig struct {
    URL                string            `yaml:"url"`
    Timeout            time.Duration     `yaml:"timeout"`
    RetryMax           int               `yaml:"retry_max"`
    Headers            map[string]string `yaml:"headers"`
    Auth               HTTPAuthConfig    `yaml:"auth"`
    InsecureSkipVerify bool              `yaml:"insecure_skip_verify"`
    FallbackPath       string            `yaml:"fallback_path"`
    FallbackMaxSize    int               `yaml:"fallback_max_size"`     // MB
    FallbackMaxBackups int               `yaml:"fallback_max_backups"`
}

// HTTPAuthConfig configures optional request authentication.
type HTTPAuthConfig struct {
    Type   string `yaml:"type"`   // "bearer" | "basic" | ""
    Token  string `yaml:"token"`
    User   string `yaml:"user"`
    Passwd string `yaml:"passwd"`
}

func (c *HTTPOutputConfig) Clean() error {
    if c.URL == "" {
        return fmt.Errorf("http output: url is required")
    }
    if c.Timeout <= 0 {
        c.Timeout = 5 * time.Second
    }
    if c.RetryMax < 0 {
        c.RetryMax = 0
    }
    if c.FallbackMaxSize <= 0 {
        c.FallbackMaxSize = 50
    }
    if c.FallbackMaxBackups < 0 {
        c.FallbackMaxBackups = 0
    }
    return nil
}
```

Wiring into the top-level `Config` depends on Task A finding:
- If `outputs:` decodes to `map[string]any`, the typed struct is applied at factory build time (in `internal/output/http.go` init).
- If a typed `Outputs map[string]OutputConfig` exists, add `HTTP HTTPOutputConfig` to the sum-type and decode via yaml node.

- [ ] **Step 4: Verify PASS**

Run: `/opt/go/1.25.12/bin/go test ./configs/... -v`
Expected: PASS.

---

## Task C: `internal/output/http.go` implementation

**Files:**
- Create: `internal/output/http.go`
- Modify: `internal/output/factory.go` — only if the factory uses a central switch; otherwise init() self-registration suffices.

- [ ] **Step 1: Write failing tests** (see Task D; tests must fail before this implementation)

- [ ] **Step 2: Implement `internal/output/http.go`**

The implementation outline (full file):

```go
// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

package output

import (
    "bytes"
    "context"
    "crypto/tls"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "sync"
    "time"

    "github.com/abrance/monitorbeat/configs"
    "github.com/abrance/monitorbeat/define"
)

const httpOutputName = "http"

// HTTPOutput posts events as JSON to an HTTP endpoint, falling back to a
// JSONL file on any failure (network error, timeout, non-2xx).
type HTTPOutput struct {
    cfg    configs.HTTPOutputConfig
    client *http.Client

    fbMu   sync.Mutex
    fbFile *os.File
    fbSize int64
    closed bool
}

func init() {
    RegisterBuilder(httpOutputName, func(cfg map[string]any) (Output, error) {
        raw, err := json.Marshal(cfg)
        if err != nil {
            return nil, fmt.Errorf("http output: marshal cfg: %w", err)
        }
        var c configs.HTTPOutputConfig
        if err := json.Unmarshal(raw, &c); err != nil {
            return nil, fmt.Errorf("http output: decode cfg: %w", err)
        }
        if err := c.Clean(); err != nil {
            return nil, err
        }
        return NewHTTPOutput(c), nil
    })
}

func NewHTTPOutput(cfg configs.HTTPOutputConfig) *HTTPOutput {
    transport := &http.Transport{
        TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
    }
    return &HTTPOutput{
        cfg: cfg,
        client: &http.Client{
            Timeout:   cfg.Timeout,
            Transport: transport,
        },
    }
}

func (h *HTTPOutput) Name() string { return httpOutputName }

// Init is a no-op; the output is fully configured via NewHTTPOutput. The
// fallback file is opened lazily on the first failed POST.
func (h *HTTPOutput) Init(_ map[string]any) error { return nil }

func (h *HTTPOutput) Publish(ctx context.Context, event define.Event) error {
    h.fbMu.Lock()
    defer h.fbMu.Unlock()
    if h.closed {
        return fmt.Errorf("http output: closed")
    }
    body, err := json.Marshal(event.GetData())
    if err != nil {
        return fmt.Errorf("http output: marshal event: %w", err)
    }
    if h.tryPost(ctx, body) {
        return nil
    }
    return h.writeFallback(body)
}

func (h *HTTPOutput) tryPost(ctx context.Context, body []byte) bool {
    var lastErr error
    for attempt := 0; attempt <= h.cfg.RetryMax; attempt++ {
        if attempt > 0 {
            backoff := time.Duration(100*(1<<(attempt-1))) * time.Millisecond
            select {
            case <-time.After(backoff):
            case <-ctx.Done():
                return false
            }
        }
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.URL, bytes.NewReader(body))
        if err != nil {
            lastErr = err
            continue
        }
        req.Header.Set("Content-Type", "application/json; charset=utf-8")
        for k, v := range h.cfg.Headers {
            req.Header.Set(k, v)
        }
        applyAuth(req, h.cfg.Auth)
        resp, err := h.client.Do(req)
        if err != nil {
            lastErr = err
            continue
        }
        _, _ = io.Copy(io.Discard, resp.Body)
        _ = resp.Body.Close()
        if resp.StatusCode >= 200 && resp.StatusCode < 300 {
            return true
        }
        lastErr = fmt.Errorf("http output: status %d", resp.StatusCode)
    }
    if lastErr != nil {
        log.Printf("http output: post failed: %v", lastErr)
    }
    return false
}

func applyAuth(req *http.Request, a configs.HTTPAuthConfig) {
    switch a.Type {
    case "bearer":
        if a.Token != "" {
            req.Header.Set("Authorization", "Bearer "+a.Token)
        }
    case "basic":
        if a.User != "" {
            req.SetBasicAuth(a.User, a.Passwd)
        }
    }
}

func (h *HTTPOutput) writeFallback(body []byte) error {
    if h.cfg.FallbackPath == "" {
        return fmt.Errorf("http output: post failed and no fallback_path configured")
    }
    if h.fbFile == nil {
        f, err := os.OpenFile(h.cfg.FallbackPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
        if err != nil {
            return fmt.Errorf("http output: open fallback: %w", err)
        }
        info, err := f.Stat()
        if err != nil {
            _ = f.Close()
            return fmt.Errorf("http output: stat fallback: %w", err)
        }
        h.fbFile = f
        h.fbSize = info.Size()
    }
    n, err := h.fbFile.Write(append(body, '\n'))
    if err != nil {
        return fmt.Errorf("http output: write fallback: %w", err)
    }
    h.fbSize += int64(n)
    if h.cfg.FallbackMaxSize > 0 {
        maxBytes := int64(h.cfg.FallbackMaxSize) * 1024 * 1024
        if h.fbSize >= maxBytes {
            if err := h.rotate(); err != nil {
                return fmt.Errorf("http output: rotate fallback: %w", err)
            }
        }
    }
    return nil
}

func (h *HTTPOutput) rotate() error {
    if h.fbFile != nil {
        _ = h.fbFile.Close()
        h.fbFile = nil
    }
    for i := h.cfg.FallbackMaxBackups; i > 0; i-- {
        src := fmt.Sprintf("%s.%d", h.cfg.FallbackPath, i)
        dst := fmt.Sprintf("%s.%d", h.cfg.FallbackPath, i+1)
        if _, err := os.Stat(src); err == nil {
            if err := os.Rename(src, dst); err != nil {
                return err
            }
        }
    }
    if err := os.Rename(h.cfg.FallbackPath, h.cfg.FallbackPath+".1"); err != nil && !os.IsNotExist(err) {
        return err
    }
    h.fbSize = 0
    return nil
}

func (h *HTTPOutput) Close() error {
    h.fbMu.Lock()
    defer h.fbMu.Unlock()
    h.closed = true
    if h.fbFile != nil {
        err := h.fbFile.Close()
        h.fbFile = nil
        return err
    }
    return nil
}
```

- [ ] **Step 3: Verify build**

Run: `/opt/go/1.25.12/bin/go build ./...`
Expected: PASS.

---

## Task D: `internal/output/http_test.go` (10 cases per spec §6)

**Files:**
- Create: `internal/output/http_test.go`

- [ ] **Step 1: Write the 10 cases (TDD: red phase first)**

Cases from spec §6:

1. `Clean()` with empty URL → error.
2. `Publish` with unreachable port + no fallback path → error.
3. `Publish` with 200 OK → 1 POST hit, fallback file not created.
4. `Publish` with unreachable port + valid fallback → 1 JSONL line in fallback, err = nil.
5. `Publish` with 500 response → fallback written, err = nil.
6. `Publish` with context canceled mid-flight → fallback written, err = nil.
7. `Publish` with HTTP fail + fallback parent dir unwritable → error returned.
8. `Publish` success then failure → fallback only contains the failed event.
9. `Close` then `Publish` → error (no panic).
10. Bearer auth header is present on outgoing request.

Use `httptest.NewServer` for live sinks, `t.TempDir()` for fallback files. All 10 cases share a small helper to build a `HTTPOutput` from cfg + override fn.

The full test file is ~150 LOC; agent writes it directly during this task.

- [ ] **Step 2: Run tests, verify PASS**

Run: `/opt/go/1.25.12/bin/go test ./internal/output/... -v -run HTTP`
Expected: all 10 PASS.

- [ ] **Step 3: Run race detector**

Run: `/opt/go/1.25.12/bin/go test -race ./internal/output/... -run HTTP`
Expected: PASS (validates the `fbMu` mutex around `fbFile`/`fbSize`).

---

## Task E: End-to-end demo

**Files:**
- Create: `configs/p1_http.yaml`

- [ ] **Step 1: Write demo config**

```yaml
# monitorbeat P1.4 http output demo
# Run alongside basereport, http output posts events to a local sink, console
# mirrors them so both streams are visible.
#
# 1) Build:
#      /opt/go/1.25.12/bin/go build -o bin/monitorbeat ./cmd/monitorbeat
# 2) Config check:
#      ./bin/monitorbeat -config configs/p1_http.yaml -check
# 3) Stand up a local POST-accepting sink (httptest or a one-line netcat loop):
#      while true; do nc -l -p 9999 -q 0; done
# 4) Run daemon (Ctrl-C after ~15s):
#      ./bin/monitorbeat -config configs/p1_http.yaml

check_interval: 1s
event_buffer_size: 1024
admin_addr: ""

outputs:
  - type: console
  - type: http
    url: "http://127.0.0.1:9999/v1/events"
    timeout: 3s
    retry_max: 1
    headers:
      X-Source: monitorbeat-p14
    fallback_path: /tmp/p14-fallback.jsonl
    fallback_max_size: 10
    fallback_max_backups: 2

basereports:
  - task_id: 4101
    enabled: true
    period: 5s
    cpu_usage: true
    mem_usage: true
```

- [ ] **Step 2: Config check**

Run: `./bin/monitorbeat -config configs/p1_http.yaml -check`
Expected: `config OK`, exit 0.

- [ ] **Step 3: Live smoke — success path**

With a netcat loop listening on 9999:
```bash
timeout --signal=TERM --kill-after=2s 12s ./bin/monitorbeat -config configs/p1_http.yaml \
  > /tmp/p14.stdout 2> /tmp/p14.stderr
```

Verify:
- `/tmp/p14.stdout` has `basereport_event` JSON lines (console mirror).
- The netcat side receives POST bodies with `Content-Type: application/json` and the configured `X-Source` header.
- `/tmp/p14-fallback.jsonl` does NOT exist (no failures).

- [ ] **Step 4: Live smoke — failure → fallback path**

Kill the netcat sink first. Re-run with the same config but a non-existent port:
```bash
sed -i 's/9999/19999/' configs/p1_http.yaml
timeout --signal=TERM --kill-after=2s 12s ./bin/monitorbeat -config configs/p1_http.yaml \
  > /tmp/p14.stdout 2> /tmp/p14.stderr
```

Verify:
- `/tmp/p14-fallback.jsonl` exists, contains ≥1 JSONL line, each line is valid JSON.
- `/tmp/p14.stdout` still shows `basereport_event` (console unaffected).
- Process exits 0 on SIGTERM (clean shutdown).

---

## Task F: Final P1.4 verification + README + commit

**Files:**
- Modify: `README.md` — mark P1.4 done, append demo section, update verification snapshot.
- Modify: this plan file — update progress table.

- [ ] **Step 1: Whole-repo verification**

```bash
cd /opt/mystorage/github/monitorbeat
/opt/go/1.25.12/bin/go build ./...           # PASS
/opt/go/1.25.12/bin/go vet ./...             # clean
/opt/go/1.25.12/bin/go test ./...            # all PASS (19 + http pkg)
/opt/go/1.25.12/bin/go test -race ./internal/output/...  # PASS
/opt/go/1.25.12/bin/gofmt -l internal/output configs cmd/monitorbeat  # empty
/opt/go/1.25.12/bin/go mod tidy              # exit 0, no diff
```

- [ ] **Step 2: Update docs**

In `README.md`:
- Status table: add P1.4 row, mark ☑.
- Add P1.4 module detail table.
- Update verification snapshot line to include 10 new http output tests.
- Append "P1.4 HTTP 输出快速演示" section.

- [ ] **Step 3: Commit P1.4**

Single commit covering:
- `configs/config.go` + `configs/config_test.go` (HTTPOutputConfig)
- `internal/output/http.go` + `internal/output/http_test.go`
- `configs/p1_http.yaml`
- `README.md` updates
- `docs/superpowers/specs/2026-07-15-p1-4-http-output-design.md` (already written)
- `docs/superpowers/plans/2026-07-15-p1-4-http-output.md` (this file)

Commit message (Conventional Commits, English):
```
feat(output): add http output with file fallback

Adds a new `output.http` that POSTs events as JSON to a configured HTTP
endpoint. On any failure (network error, timeout, non-2xx) the event is
appended as a JSONL line to a configured local fallback file with size-based
rotation. The fallback is owned by the http output, deliberately not shared
with the global `output.file`.

- new: internal/output/http.go + http_test.go (10 cases)
- new: configs.HTTPOutputConfig + HTTPAuthConfig
- new: configs/p1_http.yaml demo
- README: P1.4 status + demo section

P1.4 of three-phase plan; prerequisite for VM / OTEL receiver integration.
```

---

## Findings (Task A — completed)

Actual contract (verbatim from repo):

- **Output interface** (`internal/output/output.go:21`):
  ```go
  type Output interface {
      Name() string
      Init(cfg map[string]any) error
      Publish(ctx context.Context, ev define.Event) error
      Close() error
  }
  ```

- **Builder registration pattern**: NO `RegisterBuilder` / `init()` pattern. There is no `factory.go`. Each output is constructed explicitly in `cmd/monitorbeat/main.go:202 buildOutput()` via a `switch oc.Type`. **HTTP output must be added as a new `case "http":` in that switch.**

- **Outputs yaml shape** (`configs/config.go:48`): a list, not a map:
  ```go
  type OutputConfig struct {
      Type   string         `yaml:"type"`
      Params map[string]any `yaml:",inline"`
  }
  // Config.Outputs []OutputConfig
  ```
  Inline params end up in `Output.Init(map[string]any)`. Existing `File` output reads `path` / `max_size_mb` from this map (see `file.go:53-77`). HTTP output must parse its own fields out of the same map.

- **Wiring** (`cmd/monitorbeat/main.go:186 wireOutputs`): `buildOutput` → `o.Init(oc.Params)` → `eng.AddOutput(o)`. HTTP output is constructed with all config baked in (no separate cfg), then `Init` does the network/file setup.

- **No side-effect import** for outputs in main.go: outputs are in the same package, accessed via `output.NewConsole()` etc. So `internal/output/http.go` needs no `init()` and no extra wiring in main.go beyond the new `case "http":` in `buildOutput`.

- **`define.Event`** has `GetType() / GetData() / GetTimestamp()` — same shape used by `file.go` and `console.go` when serializing to `{timestamp, type, data}`. HTTP output should serialize the same envelope for downstream parity with file/console.

### Implications for Tasks B–F

- **Task B**: `HTTPOutputConfig` is a *typed parsing* helper only — the real source of truth at runtime is the `map[string]any` from yaml inline. Keep `HTTPOutputConfig` for `Clean()` defaults + validation, but the runtime http.go will `json.Marshal(oc.Params)` → `json.Unmarshal` into `HTTPOutputConfig` (same approach the plan already had). Place the struct + `Clean()` in `configs/config.go` next to other output-related types (put it right after `OutputConfig`).

- **Task C**: drop the `init() + RegisterBuilder` block. The new file should be plain — no package-level registration. Construction happens in `buildOutput`.

- **Task F**: in `main.go`, add `case "http":` to `buildOutput`. No side-effect import, no factory file.

- **YAML shape for users** (unchanged from spec §3):
  ```yaml
  outputs:
    - type: console
    - type: http
      url: "http://127.0.0.1:9999/v1/events"
      timeout: 5s
      ...
  ```

---

(End of file)

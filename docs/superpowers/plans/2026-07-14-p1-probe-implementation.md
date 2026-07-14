# P1.1 Probe Tasks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build clean-room `ping`, `tcp`, `udp`, and `http` probe tasks that run through the existing daemon scheduler and emit normalized probe events.

**Architecture:** Keep P0 runtime unchanged. Add typed configs, a thin shared `tasks/probe` event/result helper package, and four independent task packages registered through `tasks.RegisterBuilder`. Ping uses Go ICMP by default with command fallback; TCP/UDP/HTTP use standard library network clients.

**Tech Stack:** Go 1.22, `golang.org/x/net/icmp`, `golang.org/x/net/ipv4`, `golang.org/x/net/ipv6`, `net`, `net/http`, `net/http/httptrace`, `httptest`, existing `define.Task`/`define.Event` contracts.

---

## File Structure

- Modify `go.mod`, `go.sum`: add `golang.org/x/net` if not already present transitively.
- Modify `define/task.go`: add event/task constants if needed (`ping_event`, `tcp_event`, `udp_event`, `http_event` should be event constants, not task module constants).
- Modify `configs/config.go`: add `Pings`, `TCPs`, `UDPs`, `HTTPs` slices to `Config`, update `AllTaskConfigs()` and `GetTaskConfigListByType()`.
- Create `configs/probe.go`: shared probe config helpers if repeated defaults emerge.
- Create `configs/ping.go`, `configs/tcp.go`, `configs/udp.go`, `configs/http.go`: typed task configs and `Clean()` defaults.
- Create `tasks/probe/event.go`: normalized event payload builder.
- Create `tasks/probe/result.go`: `Result`, duration helpers, success/error helpers.
- Create `tasks/ping/ping.go`: task wrapper and builder registration.
- Create `tasks/ping/icmp.go`: Go ICMP backend.
- Create `tasks/ping/command.go`: system `ping` fallback backend and parser.
- Create `tasks/tcp/tcp.go`: TCP task and probe function.
- Create `tasks/udp/udp.go`: UDP task and probe function.
- Create `tasks/http/http.go`: HTTP task and probe function with `httptrace`.
- Create tests beside each package: `*_test.go`.
- Modify `cmd/monitorbeat/main.go`: blank-import new task packages for builder registration.
- Create `configs/p1_probe.yaml`: demo config for four probe tasks.

## Task 1: Shared probe event model

**Files:**
- Create: `tasks/probe/result.go`
- Create: `tasks/probe/event.go`
- Test: `tasks/probe/event_test.go`

- [ ] **Step 1: Write failing tests**

Create tests for:

```go
func TestEventDataBuildsNormalizedShape(t *testing.T)
func TestResultSuccessAndFailureMetrics(t *testing.T)
func TestDurationMillis(t *testing.T)
```

Expected behavior:

- Dimensions include `probe_type`, `target`, `task_id`.
- Metrics include numeric `success` and `duration_ms`.
- Error string is top-level `error`.
- `DurationMillis(1500*time.Microsecond) == 1.5`.

- [ ] **Step 2: Run failing tests**

Run: `go test ./tasks/probe -run Test -v`
Expected: FAIL because package/files do not exist.

- [ ] **Step 3: Implement minimal shared package**

Implement:

```go
type Result struct {
    Success bool
    Duration time.Duration
    Metrics map[string]float64
    Error string
}

func DurationMillis(d time.Duration) float64
func BuildEvent(probeType, target string, taskID int32, r Result) define.Event
```

`BuildEvent` should return `define.NewEvent(probeType+"_event", data)`.

- [ ] **Step 4: Verify**

Run: `go test ./tasks/probe -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
GIT_MASTER=1 git add tasks/probe
GIT_MASTER=1 git commit -m "feat(probe): add normalized event helpers" -m "Ultraworked with [Sisyphus](https://github.com/code-yeongyu/oh-my-openagent)" -m "Co-authored-by: Sisyphus <clio-agent@sisyphuslabs.ai>"
```

## Task 2: Probe task configs

**Files:**
- Modify: `configs/config.go`
- Create: `configs/ping.go`
- Create: `configs/tcp.go`
- Create: `configs/udp.go`
- Create: `configs/http.go`
- Modify: `configs/config_test.go`

- [ ] **Step 1: Write failing config tests**

Add tests for:

```go
func TestConfig_ProbeTaskGrouping(t *testing.T)
func TestProbeConfigs_CleanDefaults(t *testing.T)
```

Expected defaults:

- Ping: `Count=2`, `PayloadSize=56`, `MaxRTT=1s`, `SendInterval=500us`, `Backend="icmp"`, `Privileged=false`.
- TCP/UDP/HTTP: positive timeout/period inherited from `BaseTaskParam`.
- `Ident` uses `<type>:<task_id>`.

- [ ] **Step 2: Run failing tests**

Run: `go test ./configs -run 'Probe|Config' -v`
Expected: FAIL because configs do not exist.

- [ ] **Step 3: Implement config types**

Create typed configs:

```go
type PingConfig struct { BaseTaskParam; Target string; Count int; PayloadSize int; MaxRTT time.Duration; SendInterval time.Duration; Backend string; Privileged bool }
type TCPConfig struct { BaseTaskParam; Address string }
type UDPConfig struct { BaseTaskParam; Address string; Payload string; ExpectResponse bool }
type HTTPConfig struct { BaseTaskParam; URL string; Method string; Headers map[string]string; Body string; ExpectedStatus int }
```

Use `define.ModulePing`, `define.ModuleTCP`, `define.ModuleUDP`, `define.ModuleHTTP` for task type.

- [ ] **Step 4: Wire Config slices**

Update `Config`:

```go
Pings []PingConfig `yaml:"pings"`
TCPs []TCPConfig `yaml:"tcps"`
UDPs []UDPConfig `yaml:"udps"`
HTTPs []HTTPConfig `yaml:"https"`
```

Update `Clean()`, `AllTaskConfigs()`, `GetTaskConfigListByType()`.

- [ ] **Step 5: Verify and commit**

Run: `go test ./configs -v`
Expected: PASS.

```bash
GIT_MASTER=1 git add configs
GIT_MASTER=1 git commit -m "feat(config): add probe task configs" -m "Ultraworked with [Sisyphus](https://github.com/code-yeongyu/oh-my-openagent)" -m "Co-authored-by: Sisyphus <clio-agent@sisyphuslabs.ai>"
```

## Task 3: TCP probe task

**Files:**
- Create: `tasks/tcp/tcp.go`
- Test: `tasks/tcp/tcp_test.go`

- [ ] **Step 1: Write failing tests**

Use local listener:

```go
ln, _ := net.Listen("tcp", "127.0.0.1:0")
```

Tests:

- reachable server emits `tcp_event`, `success=1`, `connect_ms >= 0`.
- unreachable address emits `success=0` and non-empty error.

- [ ] **Step 2: Run failing tests**

Run: `go test ./tasks/tcp -v`
Expected: FAIL.

- [ ] **Step 3: Implement task**

Implement `init()` registration and `Run(ctx,e)` using `net.Dialer.DialContext("tcp", cfg.Address)` with timeout from config.

- [ ] **Step 4: Verify and commit**

Run: `go test ./tasks/tcp -v`
Expected: PASS.

```bash
GIT_MASTER=1 git add tasks/tcp
GIT_MASTER=1 git commit -m "feat(tcp): add TCP probe task" -m "Ultraworked with [Sisyphus](https://github.com/code-yeongyu/oh-my-openagent)" -m "Co-authored-by: Sisyphus <clio-agent@sisyphuslabs.ai>"
```

## Task 4: UDP probe task

**Files:**
- Create: `tasks/udp/udp.go`
- Test: `tasks/udp/udp_test.go`

- [ ] **Step 1: Write failing tests**

Create a local UDP echo server with `net.ListenPacket("udp", "127.0.0.1:0")`.

Tests:

- echo server with `expect_response=true` emits `udp_event`, `success=1`, `bytes_written`, `bytes_read`, `round_trip_ms`.
- no-response server with `expect_response=true` emits `success=0` and error.
- write-only mode with `expect_response=false` succeeds after write.

- [ ] **Step 2: Run failing tests**

Run: `go test ./tasks/udp -v`
Expected: FAIL.

- [ ] **Step 3: Implement task**

Use `net.Dialer.DialContext("udp", cfg.Address)`, `SetDeadline`, `Write`, optional `Read`.

- [ ] **Step 4: Verify and commit**

Run: `go test ./tasks/udp -v`
Expected: PASS.

```bash
GIT_MASTER=1 git add tasks/udp
GIT_MASTER=1 git commit -m "feat(udp): add UDP probe task" -m "Ultraworked with [Sisyphus](https://github.com/code-yeongyu/oh-my-openagent)" -m "Co-authored-by: Sisyphus <clio-agent@sisyphuslabs.ai>"
```

## Task 5: HTTP probe task

**Files:**
- Create: `tasks/http/http.go`
- Test: `tasks/http/http_test.go`

- [ ] **Step 1: Write failing tests**

Use `httptest.Server` and `httptest.NewTLSServer`.

Tests:

- HTTP 200 emits `http_event`, `success=1`, `status_code=200`, `total_ms`, `ttfb_ms`.
- Expected status mismatch emits `success=0` and error.
- TLS server emits `tls_ms` metric.

- [ ] **Step 2: Run failing tests**

Run: `go test ./tasks/http -v`
Expected: FAIL.

- [ ] **Step 3: Implement task**

Use `http.NewRequestWithContext`, `http.Client`, and `httptrace.ClientTrace` with DNS/connect/TLS/TTFB timestamps.

- [ ] **Step 4: Verify and commit**

Run: `go test ./tasks/http -v`
Expected: PASS.

```bash
GIT_MASTER=1 git add tasks/http
GIT_MASTER=1 git commit -m "feat(http): add HTTP probe task" -m "Ultraworked with [Sisyphus](https://github.com/code-yeongyu/oh-my-openagent)" -m "Co-authored-by: Sisyphus <clio-agent@sisyphuslabs.ai>"
```

## Task 6: Ping ICMP backend

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `tasks/ping/ping.go`
- Create: `tasks/ping/icmp.go`
- Create: `tasks/ping/command.go`
- Test: `tasks/ping/ping_test.go`

- [ ] **Step 1: Write failing tests**

Tests:

- ICMP message builder produces Echo request with ID/Seq/payload.
- Ping result aggregation computes available/loss/min/max/avg.
- Command backend parser parses Linux `ping` output.
- localhost ICMP e2e runs only when socket setup succeeds; otherwise `t.Skip`.

- [ ] **Step 2: Run failing tests**

Run: `go test ./tasks/ping -v`
Expected: FAIL.

- [ ] **Step 3: Add dependency**

Run: `go get golang.org/x/net@latest` if current `go.mod` does not expose `icmp`, `ipv4`, and `ipv6` packages.

- [ ] **Step 4: Implement simplified ICMP backend**

Implement a single-run backend with no legacy batch queue:

- Resolve target with `net.ResolveIPAddr`.
- Open `icmp.ListenPacket("udp4"|"udp6")` when `Privileged=false`.
- Open `icmp.ListenPacket("ip4:icmp"|"ip6:ipv6-icmp")` when `Privileged=true`.
- For `count` attempts, send Echo message, wait until `max_rtt`, record RTT or timeout.
- Compute metrics.

- [ ] **Step 5: Implement command fallback backend**

Keep fallback behind `Backend == "command"`; do not auto-fallback silently in initial implementation unless explicit tests cover that behavior.

- [ ] **Step 6: Verify and commit**

Run: `go test ./tasks/ping -v`
Expected: PASS or SKIP for permission-dependent e2e.

```bash
GIT_MASTER=1 git add go.mod go.sum tasks/ping
GIT_MASTER=1 git commit -m "feat(ping): add ICMP probe task" -m "Ultraworked with [Sisyphus](https://github.com/code-yeongyu/oh-my-openagent)" -m "Co-authored-by: Sisyphus <clio-agent@sisyphuslabs.ai>"
```

## Task 7: Runtime wiring and demo

**Files:**
- Modify: `cmd/monitorbeat/main.go`
- Create: `configs/p1_probe.yaml`
- Optional test: `cmd/monitorbeat/main_test.go`

- [ ] **Step 1: Wire blank imports**

Add side-effect imports for `tasks/ping`, `tasks/tcp`, `tasks/udp`, `tasks/http`.

- [ ] **Step 2: Add demo config**

Create `configs/p1_probe.yaml` with:

- ping `127.0.0.1`.
- tcp `127.0.0.1:<documented local test port placeholder>` for manual smoke; include unreachable example commented out.
- udp local echo example comments.
- http `https://example.com` for manual smoke, plus note that unit tests use local servers.

- [ ] **Step 3: Verify build/test/vet**

Run:

```bash
go test ./...
go vet ./...
go build ./...
```

Expected: all exit 0.

- [ ] **Step 4: Smoke test**

Run a local smoke harness manually:

- Start TCP listener.
- Start UDP echo server.
- Run `bin/monitorbeat -config configs/p1_probe.yaml`.
- Confirm `ping_event`, `tcp_event`, `udp_event`, and `http_event` appear within 60 seconds.

- [ ] **Step 5: Commit**

```bash
GIT_MASTER=1 git add cmd/monitorbeat/main.go configs/p1_probe.yaml
GIT_MASTER=1 git commit -m "feat(cmd): wire P1 probe tasks" -m "Ultraworked with [Sisyphus](https://github.com/code-yeongyu/oh-my-openagent)" -m "Co-authored-by: Sisyphus <clio-agent@sisyphuslabs.ai>"
```

## Final Verification

- [ ] Run `go test ./...`.
- [ ] Run `go vet ./...`.
- [ ] Run `go build ./...`.
- [ ] Run smoke test and capture event examples.
- [ ] Run `GIT_MASTER=1 git status` and confirm clean working tree.
- [ ] Summarize commits and remaining known limitations.

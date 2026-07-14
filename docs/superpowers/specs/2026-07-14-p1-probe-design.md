# P1.1 Probe Tasks Design

Date: 2026-07-14
Status: approved for implementation planning

## Goal

Implement P1.1 clean-room probe tasks for `ping`, `tcp`, `udp`, and `http`.
The goal is demonstrable, testable network probing with zero `libgse`, `libbeat`,
CMDB, or BK-specific coupling. We intentionally do not preserve every historical
bkmonitorbeat field or edge behavior.

Success criteria:

- `go build ./...` and `go test ./...` pass.
- Demo config runs `ping`, `tcp`, `udp`, and `http` together.
- Each task emits at least one event within 60 seconds.
- `tcp` reports both reachable and timeout/error cases correctly.
- `udp` can validate a local UDP echo server response.
- `http` reports status code, TLS duration, time to first byte, and total duration.
- Unit tests cover each probe implementation and the shared event shape.

## Scope

In scope:

- `configs.PingConfig`, `configs.TCPConfig`, `configs.UDPConfig`, `configs.HTTPConfig`.
- `tasks/probe` shared utilities for event payloads, durations, and result shape.
- Four independent task packages: `tasks/ping`, `tasks/tcp`, `tasks/udp`, `tasks/http`.
- Registration via `tasks.RegisterBuilder`.
- Demo YAML for P1.1 probes.
- Unit tests with local TCP/UDP/HTTP servers.

Out of scope:

- Keyword/script tasks (P1.2/P1.3).
- HTTP output (P1.4).
- Full compatibility with legacy bkmonitorbeat event fields.
- Raw ICMP sockets requiring elevated privileges.
- Distributed probe orchestration or remote agent behavior.

## Architecture

Use a thin shared probe layer plus four task-specific packages.

`tasks/probe` owns cross-cutting pieces:

- `Result`: normalized success/error/duration/status data.
- `EventData`: `{dimensions, metrics, error}` payload builder.
- Duration helpers that store milliseconds as `float64`.
- Common dimensions: `target`, `task_id`, `probe_type`.

Each task package owns protocol behavior only:

- `tasks/ping`: invokes system `ping` through `exec.CommandContext`.
- `tasks/tcp`: uses `net.Dialer.DialContext("tcp", address)`.
- `tasks/udp`: uses `net.Dialer.DialContext("udp", address)`, writes payload, optionally waits for echo.
- `tasks/http`: uses `net/http` plus `httptrace` for DNS/connect/TLS/TTFB timing.

All tasks embed `tasks.BaseTask`, register their builder in `init()`, and emit one
`define.Event` per scheduled run through the existing daemon scheduler.

## Configuration

All probe configs embed `BaseTaskParam` and use `Clean()` to fill default `TaskID`
and `Ident`.

Common fields:

- `task_id`: numeric task id.
- `enabled`: whether to build the task.
- `period`: run interval.
- `timeout`: per-run timeout.
- `target`: primary target string used in event dimensions.

Task fields:

- ping: `target`, `count`, `payload_size`.
- tcp: `address` (`host:port`).
- udp: `address`, `payload`, `expect_response`.
- http: `url`, `method`, `headers`, `body`, `expected_status`.

`configs.Config` gains slices:

- `Pings []PingConfig yaml:"pings"`
- `TCPs []TCPConfig yaml:"tcps"`
- `UDPs []UDPConfig yaml:"udps"`
- `HTTPs []HTTPConfig yaml:"https"`

`AllTaskConfigs()` and `GetTaskConfigListByType()` include the new task types.

## Event Contract

Event type names:

- `ping_event`
- `tcp_event`
- `udp_event`
- `http_event`

Payload shape:

```json
{
  "dimensions": {
    "probe_type": "tcp",
    "target": "127.0.0.1:22",
    "task_id": "1"
  },
  "metrics": {
    "success": 1,
    "duration_ms": 12.3
  },
  "error": ""
}
```

Protocol-specific metrics:

- ping: `rtt_ms`, `packet_loss_percent`, `packets_sent`, `packets_received`.
- tcp: `connect_ms`.
- udp: `round_trip_ms`, `bytes_written`, `bytes_read`.
- http: `status_code`, `dns_ms`, `connect_ms`, `tls_ms`, `ttfb_ms`, `total_ms`, `content_length`.

`success` is always numeric: `1` for success, `0` for failure. Errors are stored
in the top-level `error` string, not mixed into metrics.

## Protocol Design

### Ping

Use system `ping` instead of raw ICMP. This avoids root/capability requirements
and keeps P1.1 portable across normal Linux deployments. The task parses command
output for RTT and packet loss. If `ping` is missing, unit tests skip the e2e path
but parser tests still run.

Linux command form:

```text
ping -c <count> -W <timeout_seconds> -s <payload_size> <target>
```

### TCP

Use `net.Dialer` with a context deadline. Success means connect completed before
deadline. Failure emits `success=0`, `connect_ms`, and `error`.

### UDP

Use UDP dial, write payload, and optionally read a response. If `expect_response`
is false, successful write is enough. If true, the read must complete before
timeout.

### HTTP

Use a per-request context and `httptrace.ClientTrace` to record timing checkpoints.
TLS duration is recorded only for HTTPS. Redirects use the default client behavior
unless a later requirement asks for stricter control.

## Error Handling

Probe failures are data, not task crashes. A failed connect, timeout, DNS error,
non-matching HTTP status, or UDP read timeout emits an event with `success=0`.

Implementation errors that prevent probe setup, such as invalid URL or missing
address, are also emitted as failed probe events so the demo surface remains
observable.

The task should not panic on malformed config. `Clean()` validates enough to set
reasonable defaults; task `Run()` converts runtime failures into event errors.

## Testing

Unit tests:

- `tasks/probe`: event builder produces stable shape.
- `tasks/tcp`: local `net.Listen("tcp", "127.0.0.1:0")` success and unreachable address failure.
- `tasks/udp`: local UDP echo server success; no-response timeout failure.
- `tasks/http`: `httptest.Server` for status/TTFB; `httptest.NewTLSServer` for TLS timing.
- `tasks/ping`: parser tests for Linux ping output; e2e localhost test skipped when `ping` binary missing.
- `configs`: grouping and defaults for four new task configs.

Smoke test:

- Build `bin/monitorbeat`.
- Start local TCP server, UDP echo server, and HTTP test server where needed.
- Run `monitorbeat -config configs/p1_probe.yaml`.
- Confirm at least one event for each event type within 60 seconds.

## Integration Plan

The existing P0 stack stays intact:

- Daemon scheduler handles all four probe tasks through `BaseTaskParam.Period`.
- `tasks.factory` builds tasks by module type.
- Console/file outputs remain unchanged.
- Admin and reloader remain unchanged.

New code should preserve the current public contracts (`define.Task`,
`define.TaskConfig`, `define.Event`) and avoid changing P0 behavior.

## Open Decisions Resolved

- Strategy: clean rewrite, not legacy-compatible port.
- Ping transport: system `ping`, not raw ICMP.
- Event shape: normalized `{dimensions, metrics, error}`.
- Scheduling: daemon scheduler, not checker scheduler.
- Tests: local network servers, no external service dependency for unit tests.

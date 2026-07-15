# Agent Guidance

## Reference Implementation

- Future opencode agents working in this repository may reference the `bkmonitor-datalink` repository's `bkmonitorbeat` implementation for architecture, code organization, task patterns, configuration handling, scheduling, and protocol ideas.
- Use `bkmonitorbeat` as an implementation reference only. Do not copy or introduce BlueKing-specific product names, branding, service assumptions, identifiers, package paths, deployment conventions, authentication flows, telemetry fields, or other proprietary coupling.
- When adapting ideas from `bkmonitorbeat`, translate them into this repository's neutral `monitorbeat` domain and preserve this repository's existing style, names, dependencies, and configuration model.
- If a `bkmonitorbeat` pattern depends on BlueKing-specific infrastructure or contracts, treat it as non-portable and design a local equivalent instead of importing that dependency or behavior.

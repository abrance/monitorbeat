import type { AlertRule } from '../types'

export type Severity = 'ok' | 'pending' | 'firing'

// Threshold band: when the current value is within this absolute distance
// from the threshold we treat it as 'pending'. We use an absolute window
// rather than a percentage so small-unit metrics (e.g. load1, bytes)
// don't stay perpetually yellow. 10% of threshold magnitude is a sane
// default that scales naturally with the value space.
function isPending(value: number, rule: AlertRule): boolean {
  if (rule.threshold === 0) return false
  const window = Math.max(Math.abs(rule.threshold) * 0.1, 1)
  if (rule.condition === 'gt') {
    return value > rule.threshold - window && value <= rule.threshold
  }
  return value < rule.threshold + window && value >= rule.threshold
}

function isFiring(value: number, rule: AlertRule): boolean {
  if (rule.condition === 'gt') return value > rule.threshold
  return value < rule.threshold
}

// Severity for a single metric+hostname pairing. Disabled or silence-acked
// rules are ignored so we don't keep nagging after the user has acted.
// `silence_until` is a RFC3339 string; we treat it as still silencing
// when it's in the future.
function isSilenced(rule: AlertRule): boolean {
  if (!rule.enabled) return true
  // The states array carries per-host acknowledgement. We pull the
  // matching host's state and check if it is currently silenced.
  const state = rule.states?.find((s) => s.hostname === rule.hostname)
  if (!state?.silence_until) return false
  const until = Date.parse(state.silence_until)
  return !isNaN(until) && until > Date.now()
}

export function severityFor(value: number | undefined, rule: AlertRule | undefined): Severity {
  if (value === undefined || value === null || !rule || isSilenced(rule)) return 'ok'
  if (isFiring(value, rule)) return 'firing'
  if (isPending(value, rule)) return 'pending'
  return 'ok'
}

// Build a (metric,hostname) -> rule index for fast lookup on pages
// with many StatCards. Rules with hostname === '' apply to all hosts.
export function indexRulesByMetric(rules: AlertRule[]): Map<string, AlertRule[]> {
  const m = new Map<string, AlertRule[]>()
  for (const r of rules) {
    const key = r.metric
    const arr = m.get(key) ?? []
    arr.push(r)
    m.set(key, arr)
  }
  return m
}

// Resolve the applicable rule for a metric+hostname pair. Prefer a
// hostname-specific rule; fall back to the global one (hostname === '').
export function ruleFor(
  index: Map<string, AlertRule[]>,
  metric: string,
  hostname: string,
): AlertRule | undefined {
  const candidates = index.get(metric)
  if (!candidates) return undefined
  return (
    candidates.find((r) => r.hostname === hostname) ??
    candidates.find((r) => r.hostname === '') ??
    undefined
  )
}
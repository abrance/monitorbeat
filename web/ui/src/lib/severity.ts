import type { AlertRule } from '../types'

export type Severity = 'ok' | 'pending' | 'firing'

// The alert subsystem is now PromQL-only. Overview/HostDetail's
// CPU/Mem/Disk/Load1 cards used to color themselves by checking
// `rule.metric` against the metric name and `rule.condition/threshold`
// against the value. With PromQL rules there's no single metric name
// to match against, so the legacy helpers collapse to "always ok".
//
// The index/lookup helpers are kept so existing call sites still type-
// check and behave deterministically; they just always return undefined.

function isPending(value: number, rule: AlertRule): boolean {
  // Legacy metric/condition/threshold are gone; nothing can be "near" a
  // threshold from outside the evaluator.
  void value
  void rule
  return false
}

function isFiring(value: number, rule: AlertRule): boolean {
  void value
  void rule
  return false
}

function isSilenced(rule: AlertRule): boolean {
  if (!rule.enabled) return true
  // No metric→state index any more, so we conservatively say no.
  return false
}

export function severityFor(value: number | undefined, rule: AlertRule | undefined): Severity {
  void value
  if (!rule || isSilenced(rule)) return 'ok'
  if (isFiring(value ?? 0, rule)) return 'firing'
  if (isPending(value ?? 0, rule)) return 'pending'
  return 'ok'
}

// Build a (metric,hostname) -> rule index for fast lookup on pages
// with many StatCards. Returns an empty index under PromQL-only rules —
// callers still receive a Map, but lookups always miss.
export function indexRulesByMetric(_rules: AlertRule[]): Map<string, AlertRule[]> {
  return new Map<string, AlertRule[]>()
}

// Resolve the applicable rule for a metric+hostname pair. Always
// undefined now that rules don't expose a metric name.
export function ruleFor(
  _index: Map<string, AlertRule[]>,
  _metric: string,
  _hostname: string,
): AlertRule | undefined {
  return undefined
}
import { useCallback, useEffect, useState } from 'react'
import type {
  Host,
  Summary,
  MetricSeries,
  EventsResult,
  ProbeResult,
  Healthz,
  AlertRule,
  AlertHistoryItem,
  AlertStatus,
} from '../types'

const API = '/api/v1'

async function get<T>(path: string): Promise<T> {
  const res = await fetch(API + path)
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`HTTP ${res.status}: ${body}`)
  }
  return (await res.json()) as T
}

export interface AsyncState<T> {
  data: T | null
  error: string | null
  loading: boolean
}

export const api = {
  healthz: () => get<Healthz>('/healthz'),
  hosts: () => get<Host[]>('/hosts'),
  summary: (host: string) => get<Summary>(`/host/${encodeURIComponent(host)}/summary`),
  range: (host: string, metrics: string[], from: number, to: number, step: number) =>
    get<MetricSeries[]>(
      `/query/range?host=${encodeURIComponent(host)}&from=${from}&to=${to}&step=${step}` +
        metrics.map((m) => `&metric=${encodeURIComponent(m)}`).join(''),
    ),
  metricNames: (host: string) => get<string[]>(`/metrics/names?host=${encodeURIComponent(host)}`),
  events: (host: string, type: string, from: number, to: number, step: number) =>
    get<EventsResult>(
      `/events?host=${encodeURIComponent(host)}&type=${encodeURIComponent(type)}&from=${from}&to=${to}&step=${step}`,
    ),
  probes: (host: string, from: number, to: number, step: number) =>
    get<ProbeResult>(`/probes?host=${encodeURIComponent(host)}&from=${from}&to=${to}&step=${step}`),

  // Alert APIs
  alertRules: () => get<AlertRule[]>('/alerts/rules'),
  createAlertRule: (rule: Partial<AlertRule>) =>
    post<AlertRule>('/alerts/rules', rule),
  updateAlertRule: (id: number, rule: Partial<AlertRule>) =>
    put<AlertRule>(`/alerts/rules/${id}`, rule),
  deleteAlertRule: (id: number) =>
    del(`/alerts/rules/${id}`),
  acknowledgeAlert: (ruleId: number, hostname: string, silenceHours = 0) =>
    post<{status: string}>('/alerts/acknowledge', {rule_id: ruleId, hostname, silence_hours: silenceHours}),
  alertHistory: (params?: {rule_id?: number; hostname?: string; state?: string; limit?: number; offset?: number}) => {
    const q = new URLSearchParams()
    if (params?.rule_id) q.set('rule_id', String(params.rule_id))
    if (params?.hostname) q.set('hostname', params.hostname)
    if (params?.state) q.set('state', params.state)
    if (params?.limit) q.set('limit', String(params.limit))
    if (params?.offset) q.set('offset', String(params.offset))
    return get<{total: number; items: AlertHistoryItem[]}>('/alerts/history?' + q.toString())
  },
  alertStatus: () => get<AlertStatus>('/alerts/status'),
}

// HTTP helpers for POST/PUT/DELETE
async function post<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(API + path, {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`)
  return res.json()
}

async function put<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(API + path, {
    method: 'PUT', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`)
  return res.json()
}

async function del(path: string): Promise<void> {
  const res = await fetch(API + path, {method: 'DELETE'})
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`)
}

export function useAsync<T>(
  fn: () => Promise<T>,
  deps: unknown[],
  refetchInterval?: number,
): AsyncState<T> & { refetch: () => Promise<void> } {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const execute = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const d = await fn()
      setData(d)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  useEffect(() => {
    let cancelled = false
    execute().catch(() => {})
    if (refetchInterval && refetchInterval > 0) {
      const id = setInterval(() => {
        if (!cancelled) execute().catch(() => {})
      }, refetchInterval)
      return () => {
        cancelled = true
        clearInterval(id)
      }
    }
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [execute])

  return { data, error, loading, refetch: execute }
}

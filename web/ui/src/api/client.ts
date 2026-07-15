import { useEffect, useState } from 'react'
import type {
  Host,
  Summary,
  MetricSeries,
  EventsResult,
  ProbeResult,
  Healthz,
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
}

export interface AsyncState<T> {
  data: T | null
  error: string | null
  loading: boolean
}

export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]): AsyncState<T> {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    let cancelled = false
    setLoading(true)
    fn()
      .then((d) => {
        if (!cancelled) {
          setData(d)
          setError(null)
        }
      })
      .catch((e) => {
        if (!cancelled) setError(String(e))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)
  return { data, error, loading }
}

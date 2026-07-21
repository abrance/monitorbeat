import { useMemo } from 'react'
import { api, useAsync } from '../api/client'
import { HostCard } from '../components/HostCard'
import type { Host, Agent, Point } from '../types'
import { indexRulesByMetric } from '../lib/severity'

// Window for the per-host sparkline trends on Overview. 1h gives the eye
// enough context to spot a creeping CPU or a memory leak at a glance.
const TREND_WINDOW_SEC = 3600
const TREND_STEP_SEC = 60
const TREND_METRICS = ['cpu_usage', 'mem_used_percent', 'disk_root_used_percent']

type TrendMap = Record<string, { cpu_usage?: Point[]; mem_used_percent?: Point[]; disk_root_used_percent?: Point[] }>

export default function Overview() {
  const hosts = useAsync(() => api.hosts(), [])
  const agents = useAsync(() => api.agents(), [])
  const rules = useAsync(() => api.alertRules(), [])
  const useRegistry = agents.data && agents.data.length > 0

  // Primary: registry agents; fallback: VM hosts
  const dataSource: (Host | Agent)[] = useRegistry ? agents.data! : (hosts.data ?? [])

  const summaries = useAsync(async () => {
    if (dataSource.length === 0) return []
    return Promise.all(
      dataSource.map((h) =>
        api.summary(h.hostname).then((summary) => ({ host: h, summary })),
      ),
    )
  }, [dataSource.map(h => h.hostname).join(',')])

  const trends = useAsync<TrendMap>(async () => {
    if (dataSource.length === 0) return {}
    const now = Math.floor(Date.now() / 1000)
    const from = now - TREND_WINDOW_SEC
    const to = now
    const entries = await Promise.all(
      dataSource.map(async (h) => {
        try {
          const series = await api.range(h.hostname, TREND_METRICS, from, to, TREND_STEP_SEC)
          const out: TrendMap[string] = {}
          for (const s of series) {
            if (s.metric === 'cpu_usage' || s.metric === 'mem_used_percent' || s.metric === 'disk_root_used_percent') {
              out[s.metric] = s.points
            }
          }
          return [h.hostname, out] as const
        } catch {
          // Per-host failures must not blank the whole grid; fall back to empty trends.
          return [h.hostname, {}] as const
        }
      }),
    )
    return Object.fromEntries(entries)
  }, [dataSource.map(h => h.hostname).join(',')])

  const ruleIndex = useMemo(
    () => indexRulesByMetric(rules.data ?? []),
    [rules.data],
  )

  const online = useMemo(() => {
    if (useRegistry) {
      return agents.data!.filter(a => a.online).length
    }
    const now = Math.floor(Date.now() / 1000)
    return (summaries.data ?? []).filter((x) => now - (x.host as Host).last_seen < 120).length
  }, [useRegistry, agents.data, summaries.data])

  if (hosts.error && !useRegistry) return <div className="error">failed to load hosts: {hosts.error}</div>
  if (hosts.loading && agents.loading) return <div className="loading">loading…</div>

  return (
    <div>
      <div className="page-head">
        <h1>Hosts</h1>
        <div className="global-stats">
          <span>{dataSource.length} hosts</span>
          <span className="ok">{online} online</span>
        </div>
      </div>
      <div className="host-grid">
        {dataSource.map((h) => {
          const sm = summaries.data?.find((x) => x.host.hostname === h.hostname)?.summary
          const tr = trends.data?.[h.hostname]
          return <HostCard key={h.hostname} host={h} summary={sm} ruleIndex={ruleIndex} trends={tr} />
        })}
      </div>
    </div>
  )
}
import { useState, useMemo } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, useAsync } from '../api/client'
import { StatCard } from '../components/StatCard'
import { MetricChart, EventsChart } from '../components/Chart'
import { indexRulesByMetric, ruleFor, severityFor } from '../lib/severity'
import type { MetricSeries } from '../types'

const DEFAULT_METRICS = ['cpu_usage', 'mem_used_percent', 'disk_root_used_percent', 'load1']

// Bucket metric names by the first `_`-separated segment. Names without
// an underscore (e.g. "load1") go into a synthetic "_" bucket rendered
// first so the common top-level metrics stay visible without expansion.
type MetricGroups = Record<string, string[]>

function groupMetrics(names: string[]): MetricGroups {
  const groups: MetricGroups = {}
  for (const n of names) {
    const idx = n.indexOf('_')
    const key = idx > 0 ? n.slice(0, idx) : '_'
    if (!groups[key]) groups[key] = []
    groups[key].push(n)
  }
  // Stable order: the synthetic "_" bucket first, then alphabetical.
  const ordered: MetricGroups = {}
  if (groups['_']) ordered['_'] = groups['_']
  for (const k of Object.keys(groups).sort()) {
    if (k !== '_') ordered[k] = groups[k]
  }
  return ordered
}

export default function HostDetail() {
  const { hostname = '' } = useParams()
  const summary = useAsync(() => api.summary(hostname), [hostname])

  const now = Math.floor(Date.now() / 1000)
  const from = now - 3600
  const to = now
  const [selected, setSelected] = useState<string[]>(DEFAULT_METRICS)
  const [searchText, setSearchText] = useState('')
  // Collapsed state per group key. Empty Set = all expanded (default).
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())

  const series = useAsync(
    () => api.range(hostname, selected, from, to, 60),
    [hostname, selected.join(',')],
  )
  const names = useAsync(() => api.metricNames(hostname), [hostname])
  const events = useAsync(
    () => api.events(hostname, 'exceptionbeat_event', from, to, 60),
    [hostname],
  )
  const rules = useAsync(() => api.alertRules(), [])
  const ruleIndex = useMemo(
    () => indexRulesByMetric(rules.data ?? []),
    [rules.data],
  )

  const toggle = (m: string) =>
    setSelected((prev) =>
      prev.includes(m) ? prev.filter((x) => x !== m) : [...prev, m],
    )

  const allMetrics = names.data ?? DEFAULT_METRICS
  const groups = useMemo(() => groupMetrics(allMetrics), [allMetrics])

  const filteredGroups = useMemo(() => {
    if (!searchText) return groups
    const q = searchText.toLowerCase()
    const out: MetricGroups = {}
    for (const [k, list] of Object.entries(groups)) {
      const matched = list.filter((m) => m.toLowerCase().includes(q))
      if (matched.length) out[k] = matched
    }
    return out
  }, [groups, searchText])

  const totalCount = Object.values(filteredGroups).reduce((a, b) => a + b.length, 0)

  const toggleGroup = (k: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(k)) next.delete(k)
      else next.add(k)
      return next
    })

  // Build a quick lookup so each MetricPanel can find its series in O(1).
  const seriesByMetric = useMemo(() => {
    const m = new Map<string, MetricSeries>()
    ;(series.data ?? []).forEach((s) => m.set(s.metric, s))
    return m
  }, [series.data])

  return (
    <div>
      <Link to="/" className="back">
        ← Overview
      </Link>
      <div className="page-head">
        <h1>{hostname}</h1>
        {summary.data ? (
          <span className="muted">
            updated {new Date(summary.data.ts * 1000).toLocaleTimeString()}
          </span>
        ) : null}
      </div>

      <div className="stat-row">
        <StatCard
          label="CPU"
          value={summary.data?.cpu_usage}
          unit="%"
          severity={severityFor(summary.data?.cpu_usage, ruleFor(ruleIndex, 'cpu_usage', hostname))}
        />
        <StatCard
          label="Memory"
          value={summary.data?.mem_used_percent}
          unit="%"
          severity={severityFor(summary.data?.mem_used_percent, ruleFor(ruleIndex, 'mem_used_percent', hostname))}
        />
        <StatCard
          label="Disk /"
          value={summary.data?.disk_root_used_percent}
          unit="%"
          severity={severityFor(summary.data?.disk_root_used_percent, ruleFor(ruleIndex, 'disk_root_used_percent', hostname))}
        />
        <StatCard
          label="Load1"
          value={summary.data?.load1}
          severity={severityFor(summary.data?.load1, ruleFor(ruleIndex, 'load1', hostname))}
        />
      </div>

      <section className="panel">
        <h2>Metrics</h2>
        <div className="metric-toolbar">
          <input
            className="metric-search"
            type="text"
            placeholder="Search metrics…"
            value={searchText}
            onChange={(e) => setSearchText(e.target.value)}
          />
          <span className="metric-count">
            {selected.length} / {totalCount} selected
          </span>
        </div>
        <div className="metric-groups">
          {Object.entries(filteredGroups).map(([groupKey, list]) => {
            const isCollapsed = collapsed.has(groupKey)
            const selectedInGroup = list.filter((m) => selected.includes(m)).length
            return (
              <div key={groupKey} className="metric-group">
                <button
                  type="button"
                  className="metric-group-head"
                  onClick={() => toggleGroup(groupKey)}
                  aria-expanded={!isCollapsed}
                >
                  <span className="metric-group-arrow">{isCollapsed ? '▸' : '▾'}</span>
                  <span className="metric-group-name">
                    {groupKey === '_' ? '(other)' : groupKey}
                  </span>
                  <span className="metric-group-count">
                    {selectedInGroup > 0 ? `${selectedInGroup}/` : ''}{list.length}
                  </span>
                </button>
                {!isCollapsed && (
                  <div className="metric-picker">
                    {list.map((m) => (
                      <label key={m} className={selected.includes(m) ? 'chip active' : 'chip'}>
                        <input
                          type="checkbox"
                          checked={selected.includes(m)}
                          onChange={() => toggle(m)}
                        />
                        {m}
                      </label>
                    ))}
                  </div>
                )}
              </div>
            )
          })}
          {totalCount === 0 && !names.loading && (
            <div className="muted metric-empty">no metrics match</div>
          )}
        </div>
      </section>

      {selected.length > 0 && (
        <section className="panel">
          <h2>Charts</h2>
          {series.loading && !series.data ? (
            <div className="loading">loading…</div>
          ) : (
            <div className="metric-panels">
              {selected.map((m) => {
                const s = seriesByMetric.get(m)
                return (
                  <div key={m} className="metric-panel">
                    <div className="metric-panel-head">
                      <span className="metric-panel-name">{m}</span>
                      {s?.unit && <span className="metric-panel-unit">{s.unit}</span>}
                    </div>
                    {s ? (
                      <MetricChart
                        metric={s.metric}
                        unit={s.unit}
                        points={s.points}
                        height={140}
                        from={from}
                        to={to}
                      />
                    ) : (
                      <div className="chart-empty">no data</div>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </section>
      )}

      <section className="panel">
        <h2>Exceptions (exceptionbeat_event)</h2>
        {events.data ? (
          <EventsChart points={events.data.points} height={160} from={from} to={to} />
        ) : (
          <div className="loading">loading…</div>
        )}
      </section>
    </div>
  )
}
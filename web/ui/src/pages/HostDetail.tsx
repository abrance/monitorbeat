import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, useAsync } from '../api/client'
import { StatCard } from '../components/StatCard'
import { Chart } from '../components/Chart'

const DEFAULT_METRICS = ['cpu_usage', 'mem_used_percent', 'disk_root_used_percent', 'load1']

export default function HostDetail() {
  const { hostname = '' } = useParams()
  const summary = useAsync(() => api.summary(hostname), [hostname])

  const now = Math.floor(Date.now() / 1000)
  const from = now - 3600
  const to = now
  const [selected, setSelected] = useState<string[]>(DEFAULT_METRICS)

  const series = useAsync(
    () => api.range(hostname, selected, from, to, 60),
    [hostname, selected.join(',')],
  )
  const names = useAsync(() => api.metricNames(hostname), [hostname])
  const events = useAsync(
    () => api.events(hostname, 'exceptionbeat_event', from, to, 60),
    [hostname],
  )

  const toggle = (m: string) =>
    setSelected((prev) =>
      prev.includes(m) ? prev.filter((x) => x !== m) : [...prev, m],
    )

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
        <StatCard label="CPU" value={summary.data?.cpu_usage} unit="%" />
        <StatCard label="Memory" value={summary.data?.mem_used_percent} unit="%" />
        <StatCard label="Disk /" value={summary.data?.disk_root_used_percent} unit="%" />
        <StatCard label="Load1" value={summary.data?.load1} />
      </div>

      <section className="panel">
        <h2>Metrics</h2>
        <div className="metric-picker">
          {(names.data ?? DEFAULT_METRICS).slice(0, 48).map((m) => (
            <label key={m} className={selected.includes(m) ? 'chip active' : 'chip'}>
              <input type="checkbox" checked={selected.includes(m)} onChange={() => toggle(m)} />
              {m}
            </label>
          ))}
        </div>
        {series.data ? <Chart series={series.data} /> : <div className="loading">loading…</div>}
      </section>

      <section className="panel">
        <h2>Exceptions (exceptionbeat_event)</h2>
        {events.data ? (
          <Chart
            series={[{ metric: 'count', unit: '', points: events.data.points }]}
            height={160}
          />
        ) : (
          <div className="loading">loading…</div>
        )}
      </section>
    </div>
  )
}

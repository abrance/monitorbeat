import { useState } from 'react'
import { api, useAsync } from '../api/client'
import { StatCard } from '../components/StatCard'
import { Chart } from '../components/Chart'
import type { Point } from '../types'

export default function Probes() {
  const hosts = useAsync(() => api.hosts(), [])
  const [host, setHost] = useState('')
  const effectiveHost = host || hosts.data?.[0]?.hostname || ''

  const now = Math.floor(Date.now() / 1000)
  const from = now - 3600
  const to = now
  const probes = useAsync(
    () => (effectiveHost ? api.probes(effectiveHost, from, to, 60) : Promise.resolve(null)),
    [effectiveHost],
  )

  return (
    <div>
      <div className="page-head">
        <h1>Probes</h1>
        <select
          value={effectiveHost}
          onChange={(e) => setHost(e.target.value)}
          className="host-select"
        >
          {(hosts.data ?? []).map((h) => (
            <option key={h.hostname} value={h.hostname}>
              {h.hostname}
            </option>
          ))}
        </select>
      </div>

      {(['ping', 'tcp', 'http'] as const).map((kind) => {
        const ps = probes.data?.[kind]
        const hasUp = ps && ps.up.length > 0
        const upPct = hasUp
          ? (ps!.up.filter((p: Point) => p[1] === 1).length / ps!.up.length) * 100
          : undefined
        return (
          <section className="panel" key={kind}>
            <h2>{kind.toUpperCase()}</h2>
            <div className="stat-row">
              <StatCard label="Success" value={upPct} unit="%" />
            </div>
            {ps && ps.duration.length ? (
              <Chart
                series={[{ metric: 'duration_ms', unit: 'ms', points: ps.duration }]}
                height={180}
              />
            ) : (
              <div className="muted">no probe data</div>
            )}
          </section>
        )
      })}
    </div>
  )
}

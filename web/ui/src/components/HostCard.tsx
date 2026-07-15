import { Link } from 'react-router-dom'
import type { Host, Summary } from '../types'
import { StatCard } from './StatCard'

export function HostCard({ host, summary }: { host: Host; summary?: Summary }) {
  const now = Math.floor(Date.now() / 1000)
  const isOnline = summary ? now - host.last_seen < 120 : false
  return (
    <Link to={`/host/${encodeURIComponent(host.hostname)}`} className="host-card">
      <div className="host-card-head">
        <span className="host-name">{host.hostname}</span>
        <span className={isOnline ? 'badge online' : 'badge offline'}>
          {isOnline ? 'online' : 'offline'}
        </span>
      </div>
      <div className="host-meta">
        {host.platform} {host.arch} · {host.os}
      </div>
      <div className="host-stats">
        <StatCard label="CPU" value={summary?.cpu_usage} unit="%" />
        <StatCard label="Mem" value={summary?.mem_used_percent} unit="%" />
        <StatCard label="Disk" value={summary?.disk_root_used_percent} unit="%" />
      </div>
    </Link>
  )
}

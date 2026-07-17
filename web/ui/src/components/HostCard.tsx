import { Link } from 'react-router-dom'
import type { Host, Agent, Summary } from '../types'
import { StatCard } from './StatCard'

type HostLike = Host | Agent

function isAgent(h: HostLike): h is Agent {
  return 'online' in h
}

export function HostCard({ host, summary }: { host: HostLike; summary?: Summary }) {
  const isOnline = isAgent(host) ? host.online : (summary ? Math.floor(Date.now() / 1000) - host.last_seen < 120 : false)
  const agent = isAgent(host) ? host : null

  return (
    <Link to={`/host/${encodeURIComponent(host.hostname)}`} className="host-card">
      <div className="host-card-head">
        <span className="host-name">{host.hostname}</span>
        <span className={isOnline ? 'badge online' : 'badge offline'}>
          {isOnline ? 'online' : 'offline'}
        </span>
      </div>
      {agent && (
        <div className="host-meta">
          {agent.version && <span className="tag">{agent.version}</span>}
          {agent.tasks?.slice(0, 4).map(t => <span key={t} className="tag tag-task">{t}</span>)}
        </div>
      )}
      <div className="host-stats">
        <StatCard label="CPU" value={summary?.cpu_usage} unit="%" />
        <StatCard label="Mem" value={summary?.mem_used_percent} unit="%" />
        <StatCard label="Disk" value={summary?.disk_root_used_percent} unit="%" />
      </div>
    </Link>
  )
}

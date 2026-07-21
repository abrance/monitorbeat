import { Link } from 'react-router-dom'
import type { Host, Agent, Summary, AlertRule, Point } from '../types'
import { StatCard } from './StatCard'
import { Sparkline } from './Sparkline'
import { ruleFor, severityFor } from '../lib/severity'

type HostLike = Host | Agent

function isAgent(h: HostLike): h is Agent {
  return 'online' in h
}

type Trends = {
  cpu_usage?: Point[]
  mem_used_percent?: Point[]
  disk_root_used_percent?: Point[]
}

// Color sparklines by metric semantics so the eye can lock onto them
// quickly: CPU = blue, mem = violet, disk = amber. Matches the metric
// domain rather than severity so the sparkline stays calm even when
// nothing is alerting.
const TREND_COLORS = {
  cpu_usage: '#4f9cf9',
  mem_used_percent: '#a371f7',
  disk_root_used_percent: '#ffa657',
}

export function HostCard({
  host,
  summary,
  ruleIndex,
  trends,
}: {
  host: HostLike
  summary?: Summary
  ruleIndex?: Map<string, AlertRule[]>
  trends?: Trends
}) {
  const isOnline = isAgent(host) ? host.online : (summary ? Math.floor(Date.now() / 1000) - host.last_seen < 120 : false)
  const agent = isAgent(host) ? host : null
  const ri = ruleIndex ?? new Map()

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
      <div className="host-trends">
        <Sparkline points={trends?.cpu_usage ?? []} color={TREND_COLORS.cpu_usage} />
        <Sparkline points={trends?.mem_used_percent ?? []} color={TREND_COLORS.mem_used_percent} />
        <Sparkline points={trends?.disk_root_used_percent ?? []} color={TREND_COLORS.disk_root_used_percent} />
      </div>
      <div className="host-stats">
        <StatCard
          label="CPU"
          value={summary?.cpu_usage}
          unit="%"
          severity={severityFor(summary?.cpu_usage, ruleFor(ri, 'cpu_usage', host.hostname))}
        />
        <StatCard
          label="Mem"
          value={summary?.mem_used_percent}
          unit="%"
          severity={severityFor(summary?.mem_used_percent, ruleFor(ri, 'mem_used_percent', host.hostname))}
        />
        <StatCard
          label="Disk"
          value={summary?.disk_root_used_percent}
          unit="%"
          severity={severityFor(summary?.disk_root_used_percent, ruleFor(ri, 'disk_root_used_percent', host.hostname))}
        />
      </div>
    </Link>
  )
}
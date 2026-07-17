import { useMemo } from 'react'
import { api, useAsync } from '../api/client'
import { HostCard } from '../components/HostCard'
import type { Host, Agent } from '../types'

export default function Overview() {
  const hosts = useAsync(() => api.hosts(), [])
  const agents = useAsync(() => api.agents(), [])
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
          return <HostCard key={h.hostname} host={h} summary={sm} />
        })}
      </div>
    </div>
  )
}

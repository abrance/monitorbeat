import { useMemo } from 'react'
import { api, useAsync } from '../api/client'
import { HostCard } from '../components/HostCard'

export default function Overview() {
  const hosts = useAsync(() => api.hosts(), [])
  const summaries = useAsync(async () => {
    if (!hosts.data) return []
    return Promise.all(
      hosts.data.map((h) =>
        api.summary(h.hostname).then((summary) => ({ host: h, summary })),
      ),
    )
  }, [hosts.data])

  const online = useMemo(() => {
    const now = Math.floor(Date.now() / 1000)
    return (summaries.data ?? []).filter((x) => now - x.host.last_seen < 120).length
  }, [summaries.data])

  if (hosts.error) return <div className="error">failed to load hosts: {hosts.error}</div>
  if (hosts.loading || !hosts.data) return <div className="loading">loading…</div>

  return (
    <div>
      <div className="page-head">
        <h1>Hosts</h1>
        <div className="global-stats">
          <span>{hosts.data.length} hosts</span>
          <span className="ok">{online} online</span>
        </div>
      </div>
      <div className="host-grid">
        {hosts.data.map((h) => {
          const sm = summaries.data?.find((x) => x.host.hostname === h.hostname)?.summary
          return <HostCard key={h.hostname} host={h} summary={sm} />
        })}
      </div>
    </div>
  )
}

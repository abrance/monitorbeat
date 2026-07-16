import { useState } from 'react'
import { api, useAsync } from '../api/client'

export default function AlertHistory() {
  const [filterHost, setFilterHost] = useState('')
  const [filterState, setFilterState] = useState('')
  const [page, setPage] = useState(0)
  const limit = 20

  const history = useAsync(
    () => api.alertHistory({hostname: filterHost || undefined, state: filterState || undefined, limit, offset: page * limit}),
    [filterHost, filterState, page],
  )

  const handleAck = async (ruleId: number, hostname: string) => {
    await api.acknowledgeAlert(ruleId, hostname, 0)
    window.location.reload()
  }

  return (
    <div>
      <div className="page-head">
        <h1>告警历史</h1>
      </div>

      <div className="filter-row">
        <input className="host-select" placeholder="主机名" value={filterHost} onChange={e => { setFilterHost(e.target.value); setPage(0) }} />
        <select className="host-select" value={filterState} onChange={e => { setFilterState(e.target.value); setPage(0) }}>
          <option value="">全部状态</option>
          <option value="firing">告警中</option>
          <option value="recovered">已恢复</option>
        </select>
      </div>

      {history.error && <div className="error">加载失败: {history.error}</div>}
      {history.loading && <div className="loading">加载中…</div>}

      {history.data && (
        <>
          <table className="data-table">
            <thead>
              <tr>
                <th>规则</th>
                <th>主机</th>
                <th>值</th>
                <th>状态</th>
                <th>确认</th>
                <th>时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {history.data.items.map(h => (
                <tr key={h.id}>
                  <td>{h.rule_name}</td>
                  <td style={{fontFamily:'monospace'}}>{h.hostname}</td>
                  <td>{h.metric_value.toFixed(2)}</td>
                  <td>
                    <span className={`badge ${h.state === 'firing' ? '' : ''}`}
                      style={h.state === 'firing' ?
                        {background:'rgba(248,81,73,0.15)',color:'var(--bad)'} :
                        {background:'rgba(63,185,80,0.15)',color:'var(--ok)'}}>
                      {h.state === 'firing' ? '告警中' : '已恢复'}
                    </span>
                  </td>
                  <td>{h.acknowledged ? '已确认' : '-'}</td>
                  <td style={{fontSize:'12px',color:'var(--muted)'}}>{new Date(h.triggered_at).toLocaleString()}</td>
                  <td>
                    {h.state === 'firing' && !h.acknowledged && (
                      <button className="btn-sm btn-warning" onClick={() => handleAck(h.rule_id, h.hostname)}>处理中</button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>

          <div className="pagination">
            <button className="btn-sm" disabled={page === 0} onClick={() => setPage(p => p - 1)}>上一页</button>
            <span className="page-info">{page * limit + 1} - {Math.min((page + 1) * limit, history.data.total)} / {history.data.total}</span>
            <button className="btn-sm" disabled={(page + 1) * limit >= history.data.total} onClick={() => setPage(p => p + 1)}>下一页</button>
          </div>
        </>
      )}
    </div>
  )
}

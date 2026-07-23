import { useState } from 'react'
import { api, useAsync } from '../api/client'
import type { AlertRule, AlertTestResponse } from '../types'

interface RuleForm {
  name: string
  expr: string
  duration: number
  description: string
}

const emptyForm: RuleForm = {
  name: '',
  expr: '',
  duration: 60,
  description: '',
}

export default function Alerts() {
  const { data: rules, loading, error, refetch } = useAsync(() => api.alertRules(), [])
  const [editing, setEditing] = useState<AlertRule | null>(null)
  const [form, setForm] = useState<RuleForm>(emptyForm)
  const [showModal, setShowModal] = useState(false)
  const [ackRule, setAckRule] = useState<{id: number; fingerprint: string; hostname: string} | null>(null)
  const [ackHours, setAckHours] = useState(0)

  const [testResult, setTestResult] = useState<AlertTestResponse | null>(null)
  const [testError, setTestError] = useState<string | null>(null)
  const [testLoading, setTestLoading] = useState(false)

  const openCreate = () => {
    setEditing(null)
    setForm(emptyForm)
    setTestResult(null)
    setTestError(null)
    setShowModal(true)
  }

  const openEdit = (r: AlertRule) => {
    setEditing(r)
    setForm({
      name: r.name,
      expr: r.expr,
      duration: r.duration,
      description: r.description,
    })
    setTestResult(null)
    setTestError(null)
    setShowModal(true)
  }

  const handleSave = async () => {
    if (editing) {
      await api.updateAlertRule(editing.id, form)
    } else {
      await api.createAlertRule(form)
    }
    setShowModal(false)
    refetch()
  }

  const handleDelete = async (id: number) => {
    if (!confirm('确定删除此告警规则？')) return
    await api.deleteAlertRule(id)
    refetch()
  }

  const handleToggle = async (r: AlertRule) => {
    await api.updateAlertRule(r.id, { ...r, enabled: !r.enabled })
    refetch()
  }

  const handleAck = async () => {
    if (!ackRule) return
    await api.acknowledgeAlert(ackRule.id, ackRule.fingerprint, ackHours)
    setAckRule(null)
    refetch()
  }

  const handleTest = async () => {
    setTestLoading(true)
    setTestError(null)
    setTestResult(null)
    try {
      const res = await api.testAlertExpr(form.expr)
      setTestResult(res)
    } catch (e) {
      // `post` throws `Error("HTTP <code>: <body>")`. Try to surface the
      // backend's `error` field if present.
      const msg = e instanceof Error ? e.message : String(e)
      const m = msg.match(/"error":"([^"]+)"/)
      setTestError(m ? m[1] : msg)
    } finally {
      setTestLoading(false)
    }
  }

  const statusBadge = (r: AlertRule) => {
    if (!r.enabled) return <span className="badge" style={{background:'rgba(139,147,167,0.15)',color:'var(--muted)'}}>Disabled</span>
    const firing = r.states?.filter(s => s.status === 'firing')
    if (!firing || firing.length === 0) return <span className="badge" style={{background:'rgba(63,185,80,0.15)',color:'var(--ok)'}}>OK</span>
    const allAcked = firing.every(s => s.acknowledged)
    if (allAcked) return <span className="badge" style={{background:'rgba(255,193,7,0.15)',color:'#ffc107'}}>处理中</span>
    const n = firing.filter(s => !s.acknowledged).length
    return <span className="badge" style={{background:'rgba(248,81,73,0.15)',color:'var(--bad)'}}>Firing({n})</span>
  }

  if (error) return <div className="error">加载告警规则失败: {error}</div>
  if (loading || !rules) return <div className="loading">加载中…</div>

  return (
    <div>
      <div className="page-head">
        <h1>告警规则</h1>
        <button className="btn" onClick={openCreate}>创建规则</button>
      </div>

      <table className="data-table">
        <thead>
          <tr>
            <th>名称</th>
            <th>PromQL</th>
            <th>持续(s)</th>
            <th>状态</th>
            <th>启用</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          {rules.map(r => (
            <tr key={r.id}>
              <td>{r.name}</td>
              <td style={{fontFamily:'monospace', maxWidth:'40ch', overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap'}} title={r.expr}>{r.expr}</td>
              <td>{r.duration}</td>
              <td>{statusBadge(r)}</td>
              <td>
                <label className="switch">
                  <input type="checkbox" checked={r.enabled} onChange={() => handleToggle(r)} />
                  <span className="switch-slider"></span>
                </label>
              </td>
              <td className="action-cell">
                <button className="btn-sm" onClick={() => openEdit(r)}>编辑</button>
                <button className="btn-sm btn-danger" onClick={() => handleDelete(r.id)}>删除</button>
                {r.states?.filter(s => s.status === 'firing' && !s.acknowledged).length > 0 && (
                  <button className="btn-sm btn-warning" onClick={() => {
                    const target = r.states?.find(s => s.status === 'firing' && !s.acknowledged)
                    if (target) setAckRule({id: r.id, fingerprint: target.fingerprint, hostname: target.hostname})
                  }}>处理中</button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {/* Create/Edit Modal */}
      {showModal && (
        <div className="modal-overlay" onClick={() => setShowModal(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h2>{editing ? '编辑规则' : '创建规则'}</h2>
            <div className="form-grid">
              <label>规则名称</label>
              <input value={form.name} onChange={e => setForm({...form, name: e.target.value})} placeholder="HTTP 探测失败告警" />

              <label>PromQL 表达式</label>
              <textarea
                value={form.expr}
                onChange={e => setForm({...form, expr: e.target.value})}
                placeholder={'success{probe_type="http",target="https://example.com"} == 0'}
                className="mono"
                rows={3}
              />

              <label>持续秒数</label>
              <input type="number" value={form.duration} onChange={e => setForm({...form, duration: +e.target.value})} placeholder="0=立即" />

              <label>描述</label>
              <textarea value={form.description} onChange={e => setForm({...form, description: e.target.value})} rows={2} />
            </div>

            <div className="modal-actions" style={{justifyContent:'flex-start', gap:'0.5rem'}}>
              <button className="btn-sm" disabled={testLoading || !form.expr.trim()} onClick={handleTest}>
                {testLoading ? '查询中…' : '测试查询'}
              </button>
            </div>

            {testError && (
              <div className="panel" style={{borderColor:'var(--bad)', color:'var(--bad)', padding:'0.5rem 0.75rem'}}>
                {testError}
              </div>
            )}
            {testResult && (
              <div className="panel" style={{padding:'0.5rem 0.75rem', fontFamily:'monospace', fontSize:'12px'}}>
                <div style={{fontWeight:'bold', marginBottom:'0.25rem'}}>匹配 {testResult.result.length} 个实例</div>
                {testResult.result.length === 0 ? (
                  <div style={{color:'var(--muted)'}}>无</div>
                ) : testResult.result.map((it) => (
                  <div key={it.fingerprint} style={{borderTop:'1px solid rgba(255,255,255,0.06)', paddingTop:'0.25rem', marginTop:'0.25rem'}}>
                    <div>value = {it.value}</div>
                    {Object.entries(it.labels).map(([k, v]) => (
                      <div key={k} style={{color:'var(--muted)'}}>{k}={v}</div>
                    ))}
                    <div style={{color:'var(--muted)', fontSize:'10px'}}>fp={it.fingerprint.slice(0, 12)}…</div>
                  </div>
                ))}
              </div>
            )}

            <div className="modal-actions">
              <button className="btn" onClick={() => setShowModal(false)}>取消</button>
              <button className="btn btn-primary" onClick={handleSave}>保存</button>
            </div>
          </div>
        </div>
      )}

      {/* Acknowledge Modal */}
      {ackRule && (
        <div className="modal-overlay" onClick={() => setAckRule(null)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h2>标记处理中</h2>
            <p>主机: <strong>{ackRule.hostname || '(unknown)'}</strong></p>
            <div className="form-grid">
              <label>静默时长</label>
              <select value={ackHours} onChange={e => setAckHours(+e.target.value)}>
                <option value={0}>仅当前（恢复前不推送）</option>
                <option value={1}>1 小时</option>
                <option value={6}>6 小时</option>
                <option value={24}>24 小时</option>
              </select>
            </div>
            <div className="modal-actions">
              <button className="btn" onClick={() => setAckRule(null)}>取消</button>
              <button className="btn btn-primary" onClick={handleAck}>确认</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
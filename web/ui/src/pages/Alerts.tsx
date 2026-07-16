import { useState } from 'react'
import { api, useAsync } from '../api/client'
import type { AlertRule } from '../types'

interface RuleForm {
  name: string
  metric: string
  hostname: string
  condition: 'gt' | 'lt'
  threshold: number
  duration: number
  description: string
}

const emptyForm: RuleForm = {
  name: '', metric: '', hostname: '', condition: 'gt',
  threshold: 90, duration: 60, description: '',
}

export default function Alerts() {
  const { data: rules, loading, error, refetch } = useAsync(() => api.alertRules(), [])
  const [editing, setEditing] = useState<AlertRule | null>(null)
  const [form, setForm] = useState<RuleForm>(emptyForm)
  const [showModal, setShowModal] = useState(false)
  const [ackRule, setAckRule] = useState<{id: number; hostname: string} | null>(null)
  const [ackHours, setAckHours] = useState(0)

  const openCreate = () => {
    setEditing(null)
    setForm(emptyForm)
    setShowModal(true)
  }

  const openEdit = (r: AlertRule) => {
    setEditing(r)
    setForm({
      name: r.name,
      metric: r.metric,
      hostname: r.hostname,
      condition: r.condition,
      threshold: r.threshold,
      duration: r.duration,
      description: r.description,
    })
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
    await api.acknowledgeAlert(ackRule.id, ackRule.hostname, ackHours)
    setAckRule(null)
    refetch()
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
            <th>指标</th>
            <th>条件</th>
            <th>阈值</th>
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
              <td style={{fontFamily:'monospace'}}>{r.metric}</td>
              <td>{r.condition === 'gt' ? '>' : '<'}</td>
              <td>{r.threshold}</td>
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
                    if (target) setAckRule({id: r.id, hostname: target.hostname})
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
              <input value={form.name} onChange={e => setForm({...form, name: e.target.value})} placeholder="CPU 过高告警" />

              <label>指标名</label>
              <input value={form.metric} onChange={e => setForm({...form, metric: e.target.value})} placeholder="cpu_usage" className="mono" />

              <label>主机名</label>
              <input value={form.hostname} onChange={e => setForm({...form, hostname: e.target.value})} placeholder="留空=全部主机" />

              <label>条件</label>
              <select value={form.condition} onChange={e => setForm({...form, condition: e.target.value as 'gt' | 'lt'})}>
                <option value="gt">大于 (&gt;)</option>
                <option value="lt">小于 (&lt;)</option>
              </select>

              <label>阈值</label>
              <input type="number" value={form.threshold} onChange={e => setForm({...form, threshold: +e.target.value})} />

              <label>持续秒数</label>
              <input type="number" value={form.duration} onChange={e => setForm({...form, duration: +e.target.value})} placeholder="0=立即" />

              <label>描述</label>
              <textarea value={form.description} onChange={e => setForm({...form, description: e.target.value})} rows={2} />
            </div>
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
            <p>主机: <strong>{ackRule.hostname}</strong></p>
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

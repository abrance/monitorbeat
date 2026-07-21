import type { Severity } from '../lib/severity'

export function StatCard({
  label,
  value,
  unit,
  hint,
  severity = 'ok',
}: {
  label: string
  value?: number
  unit?: string
  hint?: string
  severity?: Severity
}) {
  const has = value !== undefined && value !== null
  return (
    <div className={`stat-card severity-${severity}`}>
      <div className="stat-label">{label}</div>
      <div className="stat-value">
        {has ? value!.toFixed(1) : '—'}
        {has && unit ? <span className="stat-unit"> {unit}</span> : null}
      </div>
      {hint ? <div className="stat-hint">{hint}</div> : null}
    </div>
  )
}
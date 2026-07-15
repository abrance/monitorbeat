export function StatCard({
  label,
  value,
  unit,
  hint,
}: {
  label: string
  value?: number
  unit?: string
  hint?: string
}) {
  const has = value !== undefined && value !== null
  return (
    <div className="stat-card">
      <div className="stat-label">{label}</div>
      <div className="stat-value">
        {has ? value!.toFixed(1) : '—'}
        {has && unit ? <span className="stat-unit"> {unit}</span> : null}
      </div>
      {hint ? <div className="stat-hint">{hint}</div> : null}
    </div>
  )
}

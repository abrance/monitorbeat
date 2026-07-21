import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { MetricSeries, Point } from '../types'

// Y-axis tick formatter: scale raw values to a readable unit suffix.
function scaleValue(v: number, unit: string): [number, string] {
  if (!isFinite(v)) return [v, unit]
  const abs = Math.abs(v)
  if (unit === 'bytes') {
    if (abs >= 1e9) return [v / 1e9, 'GB']
    if (abs >= 1e6) return [v / 1e6, 'MB']
    if (abs >= 1e3) return [v / 1e3, 'KB']
    return [v, 'B']
  }
  if (unit === 's' || unit === 'sec' || unit === 'seconds') {
    if (abs >= 1) return [v, 's']
    if (abs >= 1e-3) return [v * 1e3, 'ms']
    return [v * 1e6, 'µs']
  }
  if (abs !== 0 && (abs >= 1e9 || abs < 1e-3)) {
    if (abs >= 1e9) return [v / 1e9, 'G' + unit]
    if (abs >= 1e6) return [v / 1e6, 'M' + unit]
    if (abs >= 1e3) return [v / 1e3, 'K' + unit]
    return [v * 1e3, 'm' + unit]
  }
  return [v, unit]
}

function fmtTick(v: number, unit: string): string {
  const [scaled, scaledUnit] = scaleValue(v, unit)
  const abs = Math.abs(scaled)
  const digits = abs !== 0 && abs < 10 ? 2 : abs < 100 ? 1 : 0
  return scaled.toFixed(digits) + (scaledUnit ? ' ' + scaledUnit : '')
}

function fmtTooltip(v: number, unit: string): string {
  const [scaled, scaledUnit] = scaleValue(v, unit)
  const abs = Math.abs(scaled)
  const digits = abs !== 0 && abs < 10 ? 3 : abs < 100 ? 2 : 1
  return scaled.toFixed(digits) + (scaledUnit ? ' ' + scaledUnit : '')
}

function pickTimeFormat(fromSec: number, toSec: number): (t: number) => string {
  const span = Math.max(0, toSec - fromSec)
  if (span <= 6 * 3600) return (t) => new Date(t * 1000).toLocaleTimeString()
  if (span <= 7 * 86400) return (t) => {
    const d = new Date(t * 1000)
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return (t) => {
    const d = new Date(t * 1000)
    const m = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    return `${m}-${day}`
  }
}

const TOOLTIP_STYLE = {
  background: '#1b1f29',
  border: '1px solid #2a2f3a',
  color: '#e6e9f0',
}

// Single-series line chart. Independent Y axis per panel — fixes the
// "different units crammed onto one axis" problem and avoids the
// previous O(series × points) alignment scan since there's nothing to align.
export function MetricChart({
  metric,
  unit,
  points,
  height = 180,
  from,
  to,
}: {
  metric: string
  unit?: string
  points: Point[]
  height?: number
  from?: number
  to?: number
}) {
  if (points.length === 0) return <div className="chart-empty">no data</div>
  const data = points.map(([t, v]) => ({ t, v }))
  const inferredFrom = from ?? data[0].t
  const inferredTo = to ?? data[data.length - 1].t
  const fmtTime = pickTimeFormat(inferredFrom, inferredTo)
  const yUnit = unit ?? ''

  return (
    <ResponsiveContainer width="100%" height={height}>
      <LineChart data={data} margin={{ top: 8, right: 16, left: 8, bottom: 8 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="#2a2f3a" />
        <XAxis
          dataKey="t"
          tickFormatter={fmtTime}
          stroke="#8b93a7"
          fontSize={11}
          minTickGap={32}
          interval="preserveStartEnd"
        />
        <YAxis
          stroke="#8b93a7"
          fontSize={11}
          width={64}
          tickFormatter={(v) => fmtTick(Number(v), yUnit)}
        />
        <Tooltip
          labelFormatter={(l) => fmtTime(Number(l))}
          formatter={(value: number | string) => [fmtTooltip(Number(value), yUnit), metric]}
          contentStyle={TOOLTIP_STYLE}
        />
        <Line type="monotone" dataKey="v" name={metric} stroke="#4f9cf9" dot={false} isAnimationActive={false} />
      </LineChart>
    </ResponsiveContainer>
  )
}

// Bar chart for discrete event counts. Replaces the previous misuse of
// LineChart for exceptionbeat_event, where integer counts collapsed
// onto the same y-pixel and were unreadable.
export function EventsChart({
  points,
  height = 160,
  from,
  to,
}: {
  points: Point[]
  height?: number
  from?: number
  to?: number
}) {
  if (points.length === 0) return <div className="chart-empty">no events</div>
  const data = points.map(([t, v]) => ({ t, v }))
  const inferredFrom = from ?? data[0].t
  const inferredTo = to ?? data[data.length - 1].t
  const fmtTime = pickTimeFormat(inferredFrom, inferredTo)

  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={data} margin={{ top: 8, right: 16, left: 8, bottom: 8 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="#2a2f3a" />
        <XAxis
          dataKey="t"
          tickFormatter={fmtTime}
          stroke="#8b93a7"
          fontSize={11}
          minTickGap={32}
          interval="preserveStartEnd"
        />
        <YAxis
          stroke="#8b93a7"
          fontSize={11}
          width={56}
          allowDecimals={false}
          tickFormatter={(v) => Math.round(Number(v)).toString()}
        />
        <Tooltip
          labelFormatter={(l) => fmtTime(Number(l))}
          formatter={(value: number | string) => [Math.round(Number(value)).toString(), 'count']}
          contentStyle={TOOLTIP_STYLE}
          cursor={{ fill: 'rgba(79,156,249,0.08)' }}
        />
        <Bar dataKey="v" name="count" fill="#f85149" isAnimationActive={false} />
      </BarChart>
    </ResponsiveContainer>
  )
}

// Backwards-compatible multi-series wrapper used by Probes (duration
// for a single probe kind). Keeps the old wide-table shape so callers
// don't need to change yet; new code should prefer MetricChart.
export function Chart({
  series,
  height = 280,
  from,
  to,
}: {
  series: MetricSeries[]
  height?: number
  from?: number
  to?: number
}) {
  const tsSet = new Set<number>()
  series.forEach((s) => s.points.forEach(([t]) => tsSet.add(t)))
  const tsList = Array.from(tsSet).sort((a, b) => a - b)
  const data = tsList.map((t) => {
    const row: Record<string, number> = { t }
    series.forEach((s) => {
      const hit = s.points.find(([pt]) => pt === t)
      if (hit) row[s.metric] = hit[1]
    })
    return row
  })
  if (data.length === 0) return <div className="chart-empty">no data</div>

  const inferredFrom = from ?? (data[0]?.t ?? 0)
  const inferredTo = to ?? (data[data.length - 1]?.t ?? 0)
  const fmtTime = pickTimeFormat(inferredFrom, inferredTo)
  const yUnit = series.find((s) => s.points.length > 0)?.unit ?? ''

  return (
    <ResponsiveContainer width="100%" height={height}>
      <LineChart data={data} margin={{ top: 8, right: 16, left: 8, bottom: 8 }}>
        <CartesianGrid strokeDasharray="3 3" stroke="#2a2f3a" />
        <XAxis
          dataKey="t"
          tickFormatter={fmtTime}
          stroke="#8b93a7"
          fontSize={11}
          minTickGap={32}
          interval="preserveStartEnd"
        />
        <YAxis
          stroke="#8b93a7"
          fontSize={11}
          width={64}
          tickFormatter={(v) => fmtTick(Number(v), yUnit)}
        />
        <Tooltip
          labelFormatter={(l) => fmtTime(Number(l))}
          formatter={(value: number | string, name: string) => [fmtTooltip(Number(value), yUnit), name]}
          contentStyle={TOOLTIP_STYLE}
        />
        {series.map((s) => (
          <Line key={s.metric} type="monotone" dataKey={s.metric} dot={false} isAnimationActive={false} />
        ))}
      </LineChart>
    </ResponsiveContainer>
  )
}
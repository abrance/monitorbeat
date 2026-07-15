import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  Legend,
} from 'recharts'
import type { MetricSeries } from '../types'

function fmtTime(t: number) {
  return new Date(t * 1000).toLocaleTimeString()
}

export function Chart({ series, height = 280 }: { series: MetricSeries[]; height?: number }) {
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
  return (
    <ResponsiveContainer width="100%" height={height}>
      <LineChart data={data}>
        <CartesianGrid strokeDasharray="3 3" stroke="#2a2f3a" />
        <XAxis dataKey="t" tickFormatter={fmtTime} stroke="#8b93a7" fontSize={11} />
        <YAxis stroke="#8b93a7" fontSize={11} />
        <Tooltip
          labelFormatter={(l) => fmtTime(Number(l))}
          contentStyle={{
            background: '#1b1f29',
            border: '1px solid #2a2f3a',
            color: '#e6e9f0',
          }}
        />
        <Legend />
        {series.map((s) => (
          <Line key={s.metric} type="monotone" dataKey={s.metric} dot={false} isAnimationActive={false} />
        ))}
      </LineChart>
    </ResponsiveContainer>
  )
}

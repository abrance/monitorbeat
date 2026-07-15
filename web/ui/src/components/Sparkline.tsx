import { Area, AreaChart, ResponsiveContainer } from 'recharts'
import type { Point } from '../types'

export function Sparkline({ points, color = '#4f9cf9' }: { points: Point[]; color?: string }) {
  const data = points.map(([t, v]) => ({ t, v }))
  if (data.length === 0) return <div className="spark-empty">—</div>
  return (
    <ResponsiveContainer width="100%" height={36}>
      <AreaChart data={data}>
        <Area
          type="monotone"
          dataKey="v"
          stroke={color}
          fill={color}
          fillOpacity={0.2}
          dot={false}
          isAnimationActive={false}
        />
      </AreaChart>
    </ResponsiveContainer>
  )
}

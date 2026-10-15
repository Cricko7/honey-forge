import { ResponsiveContainer, BarChart, Bar, XAxis, YAxis, Tooltip, CartesianGrid } from 'recharts'
import { useThemeColors } from '@/lib/theme'
import { formatDateTime } from '@/lib/utils'

export function Activity({ buckets }: { buckets: { at: number; until: number; count: number }[] }) {
  const colors = useThemeColors()
  return <section className="card p-4 sm:p-6 min-w-0" aria-labelledby="activity-title">
    <h2 id="activity-title" className="font-semibold">Активность за период выборки</h2>
    <p className="mt-1 text-xs text-muted">События по времени возникновения. Время местное; интервалы одинаковой длительности.</p>
    <div className="mt-5 h-56" role="img" aria-label={`Распределение активности: событий — ${buckets.reduce((sum, bucket) => sum + bucket.count, 0)}, интервалов — ${buckets.length}`}>
      <ResponsiveContainer width="100%" height="100%">
        <BarChart data={buckets} margin={{ left: -20, right: 8 }}>
          <CartesianGrid stroke={colors.line} vertical={false} />
          <XAxis dataKey="at" stroke={colors.muted} fontSize={10} tickLine={false} minTickGap={35} tickFormatter={at => formatDateTime(new Date(at).toISOString())} />
          <YAxis stroke={colors.muted} fontSize={11} tickLine={false} axisLine={false} allowDecimals={false} />
          <Tooltip labelFormatter={(_, rows) => {
            const bucket = rows[0]?.payload
            return bucket ? `${formatDateTime(new Date(bucket.at).toISOString())} — ${formatDateTime(new Date(bucket.until).toISOString())}` : ''
          }} contentStyle={{ background: colors.panel, border: `1px solid ${colors.line}`, borderRadius: 8, fontSize: 12 }} />
          <Bar name="События" dataKey="count" fill={colors.brand} radius={[3, 3, 0, 0]} isAnimationActive={false} />
        </BarChart>
      </ResponsiveContainer>
    </div>
    <details className="mt-3 text-xs text-muted"><summary className="cursor-pointer">Данные графика таблицей</summary>
      <div className="mt-3 overflow-x-auto"><table className="w-full text-left"><caption className="sr-only">Интервалы активности</caption><thead><tr><th scope="col">С</th><th scope="col">По</th><th scope="col">События</th></tr></thead>
        <tbody>{buckets.map(bucket => <tr key={bucket.at}><td className="py-1 pr-3">{formatDateTime(new Date(bucket.at).toISOString())}</td><td className="pr-3">{formatDateTime(new Date(bucket.until).toISOString())}</td><td>{bucket.count}</td></tr>)}</tbody></table></div>
    </details>
  </section>
}

import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
  BarChart,
  Bar,
  PieChart,
  Pie,
  Cell,
} from 'recharts'
import { Bug, Activity, ShieldAlert, Globe, TrendingUp, ArrowUpRight } from 'lucide-react'
import { Card, SectionTitle, EventTypeBadge } from '@/components/ui'
import { useEvents, useTraps, useLiveStream } from '@/api/hooks'
import type { EventSummary } from '@/types'
import { formatTime } from '@/lib/utils'
import { useThemeColors } from '@/lib/theme'

function StatCard({
  icon: Icon,
  label,
  value,
  delta,
  deltaTone,
}: {
  icon: typeof Bug
  label: string
  value: string
  delta?: string
  deltaTone?: 'up' | 'down'
}) {
  return (
    <Card>
      <div className="flex items-start justify-between">
        <div>
          <div className="text-xs text-muted mb-2">{label}</div>
          <div className="text-3xl font-bold text-fg">{value}</div>
          {delta && (
            <div
              className={`text-xs mt-2 flex items-center gap-1 ${
                deltaTone === 'down' ? 'text-danger' : 'text-ok'
              }`}
            >
              <TrendingUp size={12} /> {delta}
            </div>
          )}
        </div>
        <div className="h-10 w-10 rounded-xl bg-brand/15 grid place-items-center">
          <Icon size={18} className="text-brand-soft" />
        </div>
      </div>
    </Card>
  )
}

export function DashboardPage() {
  const c = useThemeColors()
  const { data: traps } = useTraps()
  const { data: eventsPage } = useEvents({ limit: 200 })
  const [liveFeed, setLiveFeed] = useState<EventSummary[]>([])

  useLiveStream((e) => setLiveFeed((f) => [e, ...f].slice(0, 8)))

  const trapsList = traps?.items ?? []
  const events = eventsPage?.items ?? []
  const onlineTraps = trapsList.filter((t) => t.connectivity === 'online').length

  // Активность атак по часам (последние 24ч)
  const activity = useMemo(() => {
    const buckets = new Array(24).fill(0)
    const dayAgo = Date.now() - 24 * 3600 * 1000
    for (const e of events) {
      const t = new Date(e.occurred_at).getTime()
      if (t < dayAgo) continue
      const h = new Date(e.occurred_at).getHours()
      buckets[h]++
    }
    return buckets.map((count, h) => ({ hour: `${String(h).padStart(2, '0')}:00`, count }))
  }, [events])

  // Распределение по типам событий
  const byType = useMemo(() => {
    const m = new Map<string, number>()
    for (const e of events) m.set(e.event_type, (m.get(e.event_type) ?? 0) + 1)
    return [...m.entries()].map(([type, count]) => ({ type: type.split('.')[1] ?? type, count }))
  }, [events])

  // Топ источников атак по IP (страна доступна в деталях события)
  const byCountry = useMemo(() => {
    const colors = [c.brand, c.brandSoft, '#38bdf8', '#34d399', '#fbbf24', '#f43f5e']
    const counts = new Map<string, number>()
    for (const e of events) counts.set(e.source.ip, (counts.get(e.source.ip) ?? 0) + 1)
    return [...counts.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 6)
      .map(([ip, count], i) => ({ ip, count, color: colors[i % colors.length] }))
  }, [events, c])

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Дашборд</h1>
          <p className="text-sm text-muted">Обзор ловушек и активности атак в реальном времени</p>
        </div>
        <Link to="/app/network" className="btn-primary">
          Открыть конструктор <ArrowUpRight size={16} />
        </Link>
      </div>

      {/* Статкарточки */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <StatCard
          icon={Bug}
          label="Активные ловушки"
          value={String(onlineTraps)}
          delta={`${trapsList.length} всего`}
        />
        <StatCard
          icon={Activity}
          label="События за 24 часа"
          value={String(events.length)}
          delta="+18% активности"
          deltaTone="down"
        />
        <StatCard
          icon={ShieldAlert}
          label="Попытки входа"
          value={String(events.filter((e) => e.event_type.includes('auth') || e.event_type.includes('payload') || e.event_type === 'honeytoken.triggered').length)}
          delta="перехвачено"
        />
        <StatCard
          icon={Globe}
          label="Уникальных источников"
          value={String(new Set(events.map((e) => e.source.ip)).size)}
          delta="IP-адресов"
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* Активность атак */}
        <Card className="lg:col-span-2">
          <SectionTitle>Активность атак · 24 часа</SectionTitle>
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={activity}>
                <CartesianGrid strokeDasharray="3 3" stroke={c.line} vertical={false} />
                <XAxis dataKey="hour" stroke={c.muted} fontSize={11} tickLine={false} interval={3} />
                <YAxis stroke={c.muted} fontSize={11} tickLine={false} axisLine={false} />
                <Tooltip
                  contentStyle={{
                    background: c.panel,
                    border: `1px solid ${c.line}`,
                    borderRadius: 12,
                    fontSize: 12,
                  }}
                />
                <Line
                  type="monotone"
                  dataKey="count"
                  stroke={c.brand}
                  strokeWidth={2.5}
                  dot={{ r: 3, fill: c.brand }}
                  activeDot={{ r: 5 }}
                />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </Card>

        {/* Live-лента */}
        <Card>
          <SectionTitle
            action={<span className="chip bg-danger/10 text-danger animate-pulseGlow">LIVE</span>}
          >
            Поток событий
          </SectionTitle>
          <div className="space-y-2 max-h-64 overflow-auto">
            {liveFeed.length === 0 && (
              <p className="text-xs text-muted py-8 text-center">Ожидание событий…</p>
            )}
            {liveFeed.map((e) => (
              <div
                key={e.event_id}
                className="flex items-center gap-2 text-xs p-2 rounded-lg bg-bg-elevated/50 animate-slideIn"
              >
                <span className="font-mono text-muted">{formatTime(e.occurred_at)}</span>
                <span className="font-mono text-fg">{e.source.ip}</span>
                <span className="ml-auto">
                  <EventTypeBadge type={e.event_type} />
                </span>
              </div>
            ))}
          </div>
        </Card>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* Типы событий */}
        <Card className="lg:col-span-2">
          <SectionTitle>Типы событий</SectionTitle>
          <div className="h-56">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={byType}>
                <CartesianGrid strokeDasharray="3 3" stroke={c.line} vertical={false} />
                <XAxis dataKey="type" stroke={c.muted} fontSize={11} tickLine={false} />
                <YAxis stroke={c.muted} fontSize={11} tickLine={false} axisLine={false} />
                <Tooltip
                  cursor={{ fill: 'rgba(112,69,160,0.1)' }}
                  contentStyle={{
                    background: c.panel,
                    border: `1px solid ${c.line}`,
                    borderRadius: 12,
                    fontSize: 12,
                  }}
                />
                <Bar dataKey="count" fill={c.brand} radius={[6, 6, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </Card>

        {/* Топ источников */}
        <Card>
          <SectionTitle>Топ источников атак</SectionTitle>
          <div className="flex items-center gap-4">
            <div className="h-36 w-36 shrink-0">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={byCountry}
                    dataKey="count"
                    nameKey="ip"
                    innerRadius={38}
                    outerRadius={60}
                    paddingAngle={3}
                    stroke="none"
                  >
                    {byCountry.map((d) => (
                      <Cell key={d.ip} fill={d.color} />
                    ))}
                  </Pie>
                </PieChart>
              </ResponsiveContainer>
            </div>
            <div className="flex-1 space-y-1.5 text-xs">
              {byCountry.map((d) => (
                <div key={d.ip} className="flex items-center gap-2">
                  <span className="h-2 w-2 rounded-full" style={{ background: d.color }} />
                  <span className="font-mono text-fg truncate">{d.ip}</span>
                  <span className="ml-auto text-muted">{d.count}</span>
                </div>
              ))}
            </div>
          </div>
        </Card>
      </div>
    </div>
  )
}

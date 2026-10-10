// HoneyForge стаб-бэкенд: REST + WSS /api/stream по контракту OpenAPI.
// Назначение — подключить фронт к реальному серверу без Go/Docker.
import http from 'node:http'
import express from 'express'
import cors from 'cors'
import { WebSocketServer } from 'ws'
import {
  CATALOG,
  profiles,
  traps,
  events,
  commandsByTrap,
  sessionView,
  createTrap,
  createProfile,
  createCommand,
  genEvent,
  summary,
  nextLiveEvent,
  uuid,
} from './data.js'

const PORT = process.env.PORT ? Number(process.env.PORT) : 8080
const HOST = process.env.HOST ?? '127.0.0.1' // в Docker задаём 0.0.0.0
const ORIGINS = (process.env.BROWSER_ORIGINS ?? 'http://127.0.0.1:3000,http://localhost:3000').split(',')

const app = express()
app.use(express.json({ limit: '256kb' }))
app.use(
  cors({
    origin: ORIGINS,
    credentials: true,
    allowedHeaders: ['Content-Type', 'X-CSRF-Token', 'X-Expected-Revision', 'If-Match'],
  }),
)

// X-Request-ID на каждый ответ (как требует контракт)
app.use((req, res, next) => {
  res.setHeader('X-Request-ID', uuid())
  res.setHeader('Cache-Control', 'no-store')
  next()
})

const page = (items, extra = {}) => ({ items, next_cursor: null, ...extra })
const fail = (res, status, code, message) =>
  res.status(status).json({ error: { code, message }, request_id: res.getHeader('X-Request-ID') })

// --- Auth / session ---------------------------------------------------------

app.post('/api/registrations', (req, res) => {
  const { email, organization } = req.body ?? {}
  if (!email || !organization?.name)
    return fail(res, 422, 'validation_failed', 'Request validation failed')
  const s = sessionView()
  s.user.email = email
  s.organization.name = organization.name
  res.setHeader('Location', '/api/session')
  res.status(201).json(s)
})
app.post('/api/sessions', (_req, res) => {
  res.setHeader('Location', '/api/session')
  res.status(201).json(sessionView())
})
app.get('/api/session', (_req, res) => res.json(sessionView()))
app.delete('/api/session', (_req, res) => res.status(204).end())

// --- Каталог ----------------------------------------------------------------

app.get('/api/trap-types', (_req, res) => res.json(page(CATALOG)))
app.get('/api/trap-types/:type_id/versions/:type_version', (req, res) => {
  const e = CATALOG.find(
    (c) => c.type_id === req.params.type_id && String(c.type_version) === req.params.type_version,
  )
  return e ? res.json(e) : fail(res, 404, 'resource_not_found', 'Resource not found')
})

// --- Профили ----------------------------------------------------------------

app.get('/api/profiles', (_req, res) => res.json(page(profiles)))
app.post('/api/profiles', (req, res) => {
  const { name, type_id, type_version, config, description } = req.body ?? {}
  if (!name || !type_id || !type_version || typeof config !== 'object')
    return fail(res, 422, 'validation_failed', 'Request validation failed')
  const profile = createProfile({ name, description, type_id, type_version, config })
  res.setHeader('Location', `/api/profiles/${profile.id}`)
  res.status(201).json(profile)
})
app.get('/api/profiles/:id', (req, res) => {
  const p = profiles.find((x) => x.id === req.params.id)
  return p ? res.json(p) : fail(res, 404, 'resource_not_found', 'Resource not found')
})

// --- Ловушки ----------------------------------------------------------------

app.get('/api/traps', (req, res) => {
  let items = traps.slice()
  if (req.query.connectivity) items = items.filter((t) => t.connectivity === req.query.connectivity)
  if (req.query.profile_id) items = items.filter((t) => t.profile_id === req.query.profile_id)
  res.json(page(items))
})
app.get('/api/traps/:id', (req, res) => {
  const t = traps.find((x) => x.id === req.params.id)
  return t ? res.json(t) : fail(res, 404, 'resource_not_found', 'Resource not found')
})
app.post('/api/traps', (req, res) => {
  const { name, profile_id, description } = req.body ?? {}
  if (!name || !profile_id) return fail(res, 422, 'validation_failed', 'Request validation failed')
  const trap = createTrap({ name, profile_id, description })
  if (!trap) return fail(res, 422, 'unknown_trap_type', 'Unknown trap type')
  broadcast({ kind: 'trap.changed', trap: { ...trap } })
  res.setHeader('Location', `/api/traps/${trap.id}`)
  res.status(201).json(trap)
})
app.delete('/api/traps/:id', (req, res) => {
  const idx = traps.findIndex((x) => x.id === req.params.id)
  if (idx < 0) return fail(res, 404, 'resource_not_found', 'Resource not found')
  if (!req.get('X-Expected-Revision')) return fail(res, 428, 'precondition_required', 'A precondition header is required')
  const [t] = traps.splice(idx, 1)
  broadcast({ kind: 'trap.deleted', trap_id: t.id })
  res.status(204).end()
})

// --- Команды ----------------------------------------------------------------

app.get('/api/traps/:id/commands', (req, res) => {
  res.json(page(commandsByTrap.get(req.params.id) ?? []))
})
app.post('/api/traps/:id/commands', (req, res) => {
  const trap = traps.find((x) => x.id === req.params.id)
  if (!trap) return fail(res, 404, 'resource_not_found', 'Resource not found')
  const { action } = req.body ?? {}
  if (!['start', 'stop', 'apply_config'].includes(action))
    return fail(res, 422, 'validation_failed', 'Request validation failed')
  const cmd = createCommand(trap, action, broadcast)
  res.setHeader('Location', `/api/traps/${trap.id}/commands/${cmd.id}`)
  res.status(201).json(cmd)
})
app.get('/api/traps/:id/commands/:command_id', (req, res) => {
  const list = commandsByTrap.get(req.params.id) ?? []
  const c = list.find((x) => x.id === req.params.command_id)
  return c ? res.json(c) : fail(res, 404, 'resource_not_found', 'Resource not found')
})

// --- События ----------------------------------------------------------------

app.get('/api/events', (req, res) => {
  let items = events
  if (req.query.trap_id) items = items.filter((e) => e.trap_id === req.query.trap_id)
  const limit = req.query.limit ? Number(req.query.limit) : 50
  res.json(
    page(
      items.slice(0, limit).map(summary),
      { stream_cursor: 'cursor-' + Date.now() },
    ),
  )
})
app.get('/api/events/:id', (req, res) => {
  const e = events.find((x) => x.event_id === req.params.id)
  return e ? res.json(e) : fail(res, 404, 'resource_not_found', 'Resource not found')
})

app.get('/healthz', (_req, res) => res.json({ status: 'ok' }))

// --- WSS /api/stream --------------------------------------------------------

const server = http.createServer(app)
const wss = new WebSocketServer({ server, path: '/api/stream' })

const envelope = (type, data) => JSON.stringify({ message_id: uuid(), type, reply_to: null, payload: data })

function broadcast(notification) {
  let type
  let data
  if (notification.kind === 'event.created') {
    type = 'event.created'
    data = { event: notification.event }
  } else if (notification.kind === 'trap.changed') {
    type = 'trap.changed'
    data = { trap: notification.trap }
  } else if (notification.kind === 'trap.deleted') {
    type = 'trap.deleted'
    data = { trap_id: notification.trap_id }
  } else if (notification.kind === 'command.changed') {
    type = 'command.changed'
    data = { trap_id: notification.trap_id, command: notification.command }
  } else return

  const msg = envelope(type, data)
  for (const client of wss.clients) {
    if (client.readyState === 1) client.send(msg)
  }
}

wss.on('connection', (ws) => {
  ws.on('message', (raw) => {
    try {
      const env = JSON.parse(raw.toString())
      // Клиент шлёт stream.subscribe {after}; отвечаем correlated stream.ready
      if (env.type === 'stream.subscribe') {
        ws.send(
          JSON.stringify({
            message_id: uuid(),
            type: 'stream.ready',
            reply_to: env.message_id,
            payload: { cursor: 'cursor-' + Date.now(), server_time: new Date().toISOString(), replayed: false },
          }),
        )
      }
    } catch {
      /* ignore */
    }
  })
})

// Живой поток атак: каждые 3.5с новое событие во все подписки
setInterval(() => {
  const ev = nextLiveEvent()
  if (ev) broadcast({ kind: 'event.created', event: summary(ev) })
}, 3500)

server.listen(PORT, HOST, () => {
  console.log(`HoneyForge stub backend → http://${HOST}:${PORT}`)
  console.log(`  REST:  /api/session, /api/traps, /api/events, /api/profiles, /api/trap-types, ...`)
  console.log(`  WSS:   ws://127.0.0.1:${PORT}/api/stream`)
  console.log(`  CORS origins: ${ORIGINS.join(', ')}`)
})

// genEvent импортирован для возможного расширения сценариев
void genEvent

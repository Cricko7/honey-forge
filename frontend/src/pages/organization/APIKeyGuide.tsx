import { useState } from 'react'
import { Modal } from '@/components/Modal'
import { CopyCommand } from '../servers/CopyCommand'

const topics = [
  { id: 'auth', title: 'Авторизация' },
  { id: 'events', title: 'События и данные' },
  { id: 'traps', title: 'Запуск ловушек' },
  { id: 'realtime', title: 'Realtime' },
  { id: 'limits', title: 'Лимиты и ошибки' },
] as const

type Topic = typeof topics[number]['id']

export function APIKeyGuide({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [topic, setTopic] = useState<Topic>('auth')

  return (
    <Modal open={open} onClose={onClose} title="Документация API" width="max-w-3xl">
      <div className="max-h-[75vh] overflow-y-auto space-y-5">
        <nav aria-label="Разделы документации API" className="flex flex-wrap gap-2">
          {topics.map((item) => (
            <button key={item.id} className={topic === item.id ? 'btn btn-primary' : 'btn-ghost'}
              aria-pressed={topic === item.id} onClick={() => setTopic(item.id)}>{item.title}</button>
          ))}
        </nav>
        <div className="text-sm space-y-4 leading-relaxed">
          {topic === 'auth' && <>
            <p>Выпустите ключ, сохраните его в надёжном месте и передавайте по HTTPS в заголовке Authorization: Bearer &lt;ключ&gt;. Повторно посмотреть секрет нельзя.</p>
            <p>Ключ действует до отзыва, с текущими правами владельца, только в его организации. Cookie, Origin и CSRF для серверной интеграции не нужны. Если отправляете Origin, он должен быть разрешён сервером.</p>
            <CopyCommand label="Скопировать пример" command={`export HONEYFORGE_URL="https://<адрес-сервера>"
export HONEYFORGE_API_KEY="<ваш-ключ>"
curl "$HONEYFORGE_URL/api/organization" \\
  -H "Authorization: Bearer $HONEYFORGE_API_KEY"`} />
            <p className="text-muted">Примеры команд рассчитаны на bash. Не передавайте ключ в URL. Выпуск, список и отзыв ключей, а также коды приглашения доступны через вход в приложение.</p>
          </>}
          {topic === 'events' && <>
            <p>Получайте события и другие данные через обычные REST-запросы с ключом.</p>
            <CopyCommand label="Скопировать запрос событий" command={`curl "$HONEYFORGE_URL/api/events?limit=100&trap_id=$TRAP_ID" \\
  -H "Authorization: Bearer $HONEYFORGE_API_KEY"`} />
            <p>Фильтры событий: trap_id, type_id, source_ip, event_type, session_id, from и to. Время — RFC 3339; from включительно, to не включительно.</p>
            <p>Ответ страницы: items, next_cursor, stream_cursor. Передавайте next_cursor в параметре cursor для следующей страницы до null. Максимальный limit — 100. stream_cursor подходит для подписки realtime без пропуска изменений после загрузки истории.</p>
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <thead><tr><th className="py-2 pr-4">Запрос</th><th>Данные</th></tr></thead>
                <tbody>{[
                  ['/api/events/{event_id}', 'Полное событие'],
                  ['/api/events/export?format=csv', 'Экспорт CSV; также format=stix'],
                  ['/api/events/recordings/{session_id}?trap_id={trap_id}', 'Запись SSH-сессии'],
                  ['/api/traps', 'Ловушки; детали /api/traps/{id}'],
                  ['/api/traps/{id}/commands', 'Команды и их статусы'],
                  ['/api/traps/deployment-nodes', 'Доступные серверы для размещения'],
                  ['/api/profiles', 'Профили конфигурации без секретов'],
                  ['/api/trap-types', 'Каталог; схема /api/trap-types/{type_id}/versions/{type_version}'],
                  ['/api/audit-entries', 'История действий'],
                  ['/api/trap-links', 'Связи ловушек'],
                  ['/api/organization', 'Ваша организация'],
                ].map(([path, label]) => <tr key={path} className="border-t border-line"><td className="py-2 pr-4 font-mono">GET {path}</td><td>{label}</td></tr>)}</tbody>
              </table>
            </div>
            <p className="text-muted">Пагинация есть у списков событий, ловушек, команд, профилей, каталога и аудита. Организация, серверы и связи возвращаются без курсора.</p>
          </>}
          {topic === 'traps' && <>
            <ol className="list-decimal pl-5 space-y-2">
              <li>Получите каталог и config_schema выбранной версии типа ловушки.</li>
              <li>POST /api/profiles: request_id (новый UUID), name, type_id, type_version, config по схеме типа.</li>
              <li>POST /api/traps: request_id (новый UUID), name, profile_id; deployment_node_id — при выборе сервера.</li>
              <li>Выполните apply_config с нужной profile_revision и дождитесь status: succeeded через GET /api/traps/{'{id}'}/commands/{'{command_id}'} либо realtime.</li>
              <li>Отправьте start. Результат выполнения проверяйте по статусу команды и runtime_state ловушки.</li>
            </ol>
            <CopyCommand label="Скопировать применение профиля" command={`curl "$HONEYFORGE_URL/api/traps/$TRAP_ID/commands" \\
  -H "Authorization: Bearer $HONEYFORGE_API_KEY" \\
  -H "Content-Type: application/json" \\
  --data '{"request_id":"<новый UUID>","action":"apply_config","params":{"profile_revision":1}}'`} />
            <CopyCommand label="Скопировать запуск" command={`curl "$HONEYFORGE_URL/api/traps/$TRAP_ID/commands" \\
  -H "Authorization: Bearer $HONEYFORGE_API_KEY" \\
  -H "Content-Type: application/json" \\
  --data '{"request_id":"<новый UUID>","action":"start","params":{}}'`} />
            <p className="text-muted">Для остановки используйте action: stop с params: {'{}'}. Команды асинхронные: 201 означает постановку в очередь. Агент ловушки должен быть подключён для выполнения. Один request_id можно повторить с тем же телом для безопасного повтора запроса.</p>
          </>}
          {topic === 'realtime' && <>
            <p>Подключитесь к wss://&lt;сервер&gt;/api/stream с Authorization: Bearer &lt;ключ&gt; и подпротоколом dashboard-stream.v1. В течение 5 секунд отправьте:</p>
            <CopyCommand label="Скопировать подписку" command={'{"message_id":"<новый UUID>","type":"stream.subscribe","payload":{"after":null}}'} />
            <p>after: null получает будущие изменения. Для восстановления передайте stream_cursor из REST или последний payload.cursor из потока в поле after.</p>
            <p>Типы уведомлений: event.created, trap.changed, trap.deleted, command.changed, profile.changed, profile.deleted и audit.created. После восстановления приходит stream.ready. Полное событие запрашивайте через GET /api/events/{'{event_id}'}; audit.created содержит audit_id.</p>
            <p>При cursor_expired или invalid_cursor загрузите REST-данные заново и подпишитесь с новым stream_cursor. Клиент должен отвечать pong на WebSocket ping. После отзыва ключа соединение закрывается с кодом 4401; проверка идёт перед уведомлениями и каждые 15 секунд.</p>
            <p className="text-muted">Используйте серверный WebSocket-клиент, поддерживающий заголовок Authorization. Браузерный WebSocket не позволяет задать этот заголовок; сайт работает через сессию. Ключ в URL не поддерживается.</p>
          </>}
          {topic === 'limits' && <>
            <p>Лимит запросов применяется отдельно к каждому API-ключу. REST-запросы и подключения realtime используют общую квоту. Уведомления в открытом соединении квоту не расходуют.</p>
            <p>При превышении лимита сервер возвращает HTTP 429 с кодом rate_limited. Подождите число секунд из заголовка Retry-After перед следующей попыткой.</p>
            <pre className="bg-bg-elevated rounded-xl p-3 text-xs">{'{"error":{"code":"rate_limited","message":"API key rate limit exceeded"}}'}</pre>
            <p>400 — некорректный запрос; 401 — ключ неверный или отозван; 403 — недостаточно прав; 404 — объект отсутствует или из другой организации; 409 — конфликт состояния; 422 — ошибка валидации; 429 — превышение лимита; 503 — временная недоступность.</p>
          </>}
        </div>
      </div>
    </Modal>
  )
}

import { useState } from 'react'
import { Link } from 'react-router-dom'
import { CheckCircle2 } from 'lucide-react'
import { CopyCommand } from './CopyCommand'
import { Modal } from '@/components/Modal'
import { assignServerCommand, workerJoinCommand } from '@/lib/server-connection'
import type { DeploymentNode } from '@/types'

export function ConnectServerModal({ organization, nodes, onClose, onRefresh }: {
  organization: string
  nodes: DeploymentNode[]
  onClose: () => void
  onRefresh: () => void
}) {
  const [manager, setManager] = useState('')
  const [token, setToken] = useState('')
  const [nodeId, setNodeId] = useState('')
  const join = workerJoinCommand(manager, token)
  const assign = assignServerCommand(nodeId, organization)
  const connected = nodes.find((node) => node.id === nodeId.trim() && node.ready)

  return <Modal open onClose={onClose} title="Подключить сервер компании" width="max-w-2xl">
    <div className="space-y-5 max-h-[70vh] overflow-y-auto pr-1">
      <p className="text-sm text-muted">Подключение выполняется один раз. После него сервер можно выбирать при создании любой ловушки.</p>
      <a href="#server-guide" onClick={onClose} className="inline-block text-sm text-brand underline">Открыть подробную инструкцию и устранение ошибок</a>
      <section className="space-y-3">
        <h3 className="font-medium">1. Подготовьте сервер</h3>
        <p className="text-sm text-muted">Нужны Linux, установленный Docker и частная сеть или VPN до платформы. Администратор платформы должен разрешить соединение и загрузить на сервер образы ловушек.</p>
        <p className="text-xs text-muted">Между узлами: TCP 2377 до управляющего сервера, TCP/UDP 7946 и UDP 4789. Проверяйте их внутри частной сети.</p>
      </section>
      <section className="space-y-3">
        <h3 className="font-medium">2. Подключите к кластеру</h3>
        <p className="text-sm text-muted">Получите у администратора платформы адрес управляющего сервера и worker token. Выполните полученную ниже команду на сервере компании.</p>
        <div className="grid sm:grid-cols-2 gap-3">
          <div><label htmlFor="swarm-manager" className="label">Адрес управляющего сервера</label>
            <input id="swarm-manager" className="input" placeholder="10.0.0.1:2377" value={manager} onChange={(e) => setManager(e.target.value)} autoComplete="off" /></div>
          <div><label htmlFor="swarm-token" className="label">Worker token</label>
            <input id="swarm-token" className="input" type="password" placeholder="SWMTKN-…" value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" /></div>
        </div>
        {join ? <CopyCommand key={join} command={join} label="Скопировать команду подключения" /> : <p className="text-xs text-muted">Введите адрес с портом и token, чтобы получить команду.</p>}
        <p className="text-xs text-muted">Token остаётся только в этой форме и не отправляется в API. После закрытия формы он очищается. Команда и буфер обмена содержат секрет.</p>
      </section>
      <section className="space-y-3">
        <h3 className="font-medium">3. Назначьте сервер организации</h3>
        <p className="text-sm text-muted">На сервере компании узнайте его ID:</p>
        <CopyCommand command={"docker info --format '{{.Swarm.NodeID}}'"} label="Скопировать команду проверки" />
        <label htmlFor="swarm-node" className="label">ID подключённого сервера</label>
        <input id="swarm-node" className="input font-mono" placeholder="Полный ID узла из 25 символов" value={nodeId} onChange={(e) => setNodeId(e.target.value)} autoComplete="off" />
        {assign && <>
          <p className="text-sm text-muted">Передайте эту команду администратору платформы. Он выполнит её на управляющем сервере и закрепит узел за вашей организацией.</p>
          <CopyCommand key={assign} command={assign} label="Скопировать команду назначения" />
        </>}
      </section>
      <div className="flex items-center gap-3">
        <button className="btn-ghost" onClick={onRefresh}>Проверить подключение</button>
        {connected ? <span role="status" className="text-sm text-success flex items-center gap-2"><CheckCircle2 size={16} /> {connected.hostname} готов</span>
          : <span role="status" className="text-xs text-muted">Ожидаем назначения и состояния Ready / Active. Список обновляется автоматически.</span>}
      </div>
      {connected && <Link className="btn-primary" to="/app/network">Создать ловушку на сервере</Link>}
    </div>
  </Modal>
}

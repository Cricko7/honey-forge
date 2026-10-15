import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Plus, Server } from 'lucide-react'
import { Card, EmptyState, Spinner } from '@/components/ui'
import { useCanManage, useDeploymentNodes, useSession } from '@/api/hooks'
import { ConnectServerModal } from './servers/ConnectServerModal'
import { ServerGuide } from './servers/ServerGuide'
import { SingleServerGuide } from './servers/SingleServerGuide'

export function ServersPage() {
  const canManage = useCanManage()
  const { data: session } = useSession()
  const { data, isLoading, error, refetch } = useDeploymentNodes()
  const [connecting, setConnecting] = useState(false)

  return <div className="page-content space-y-5">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div><h1 className="text-2xl font-semibold tracking-tight">Серверы</h1>
        <p className="text-sm text-muted">Машины вашей организации для развёртывания ловушек</p>
        <a href="#server-guide" className="text-sm text-brand underline">Подробная инструкция по подключению</a></div>
      {canManage && <button className="btn-primary" onClick={() => setConnecting(true)}><Plus size={16} /> Подключить сервер</button>}
    </div>
    <Card><div className="flex flex-wrap items-center gap-3"><Server size={20} className="text-brand shrink-0" />
      <div><h2 className="font-medium">Сервер платформы</h2><p className="text-xs text-muted">Доступен как стандартный вариант в форме создания ловушки</p></div>
      <Link className="btn-ghost ml-auto" to="/app/network">К ловушкам</Link></div></Card>
    {error && <div role="alert" className="text-sm text-danger">Не удалось загрузить серверы. <button className="btn-ghost" onClick={() => refetch()}>Повторить</button></div>}
    <SingleServerGuide />
    {isLoading ? <Spinner /> : !error && !data?.items.length ? <EmptyState title="Дополнительные серверы ещё не подключены" hint="Ловушки уже можно развернуть на сервере платформы. Для другой машины нажмите «Подключить сервер»." /> :
      <div>{data?.items.map((node) => <Card key={node.id}>
        <div className="flex items-center justify-between gap-3"><h2 className="font-medium">{node.hostname}</h2>
          <span className={`chip ${node.ready ? 'bg-brand/10 text-brand' : 'bg-warn/10 text-warn'}`}>{node.ready ? 'Готов' : 'Недоступен'}</span></div>
        <p className="font-mono text-sm mt-3">{node.address}</p>
        <p className="font-mono text-xs text-muted mt-1 break-all">{node.id}</p>
        <p className="text-xs text-muted mt-3">{node.ready ? 'Можно выбрать при создании ловушки.' : 'Проверьте сервер, связь с кластером и состояние Ready / Active.'}</p>
      </Card>)}</div>}
    <ServerGuide organization={session?.organization.id} />
    {connecting && session && <ConnectServerModal organization={session.organization.id} nodes={data?.items ?? []} onClose={() => setConnecting(false)} onRefresh={() => { void refetch() }} />}
  </div>
}

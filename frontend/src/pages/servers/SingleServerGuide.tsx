import { Card } from '@/components/ui'
import { CopyCommand } from './CopyCommand'

export function SingleServerGuide() {
  return <Card>
    <details>
      <summary className="min-h-11 cursor-pointer py-2 text-base font-semibold text-brand">Один сервер — два изолированных контура</summary>
    <article className="space-y-4 text-sm">
      <header className="space-y-2">
        <p>Ловушки работают на сервере платформы. Дополнительные машины и виртуальные машины для этого не нужны: при создании выберите «Сервер платформы».</p>
      </header>
      <div className="grid md:grid-cols-2 gap-4">
        <div className="space-y-2">
          <h3 className="font-semibold">Контур ловушки</h3>
          <p>TCP, Redis и HTTP отвечают на входящие подключения. Исходящие соединения и DNS закрыты. SSH-команды выполняются в отдельном sandbox без сети.</p>
        </div>
        <div className="space-y-2">
          <h3 className="font-semibold">Контур наблюдения</h3>
          <p>События выходят из worker через pipes. Запись SSH-сессии идёт на внешнем шлюзе. Адрес backend, его ключи и хранилище находятся за пределами sandbox.</p>
        </div>
      </div>
      <details className="space-y-3">
        <summary className="cursor-pointer font-medium">Запуск и демонстрация</summary>
        <p>Автоматический деплой собирает образы и проверяет изоляцию. Для отдельного запуска из корня проекта на Linux с Docker:</p>
        <CopyCommand command="docker compose -f docker-compose.real.yml up --build -d" label="Скопировать запуск" />
        <p>Панель в этом режиме доступна на https://localhost:8443. Для удалённого доступа администратор может использовать SSH-туннель; наружу открываются выбранные порты ловушек.</p>
        <ol className="list-decimal pl-5 space-y-2">
          <li>Запустите TCP, Redis или HTTP-ловушку; для проверки SSH откройте и держите сессию.</li>
          <li>Выполните проверку ниже. Она проверяет работающие контейнеры, права, сеть, DNS и отсутствие ресурсов управления.</li>
          <li>В SSH-сессии выполните hostname, id и cat /proc/net/route. Выполните обычную команду или загрузите файл и покажите соответствующее событие в панели.</li>
        </ol>
        <CopyCommand command={'docker exec "$(docker ps --filter label=com.docker.compose.service=orchestrator --format \'{{.ID}}\')" sh /opt/honeyforge/verify-isolation.sh'} label="Скопировать проверку изоляции" />
      </details>
      <details className="space-y-3">
        <summary className="cursor-pointer font-medium">Как представить судьям</summary>
        <p>«Мы вынесли наблюдение за периметр ловушки. Атакующий взаимодействует с изолированным сервисом, а события собираются в отдельном контуре. В SSH-sandbox нет ни сетевого канала телеметрии, ни процесса записи сессии. Для TCP, Redis и HTTP канал событий проходит через pipes, а сеть допускает только ответы клиентам. Всё работает на одном сервере — без парка виртуальных машин. Сейчас покажем проверку изоляции и запись реального действия в панели».</p>
      </details>
    </article>
    </details>
  </Card>
}

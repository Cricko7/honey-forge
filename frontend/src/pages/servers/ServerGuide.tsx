import { Card } from '@/components/ui'
import { CopyCommand } from './CopyCommand'

export function ServerGuide({ organization }: { organization?: string }) {
  return <Card>
    <details>
      <summary className="min-h-11 cursor-pointer py-2 text-base font-semibold text-brand">Подробная инструкция по подключению сервера компании</summary>
    <article id="server-guide" className="space-y-6 text-sm scroll-mt-6">
      <header className="space-y-2">
        <p className="text-muted">Всё необходимое для подключения и проверки собрано на этой странице. Пройдите шаги по порядку. Сервер подключается один раз; затем его можно выбирать при создании ловушек.</p>
        <p>Администратор компании выполняет команды на своём сервере. Администратор платформы выполняет команды на управляющем сервере Swarm (manager). Права администратора в панели сами по себе не дают доступ к manager.</p>
        <p className="text-muted">Сервер компании присоединяется как worker — исполнитель ловушек. Общий кластер предполагает доверие к администраторам подключённых машин. Согласуйте подключение с администратором платформы.</p>
      </header>

      <section className="space-y-3">
        <h3 className="font-semibold">1. Подготовьте сервер и доступы</h3>
        <ul className="list-disc pl-5 space-y-2">
          <li>Нужен Linux с Docker Engine и доступом к терминалу. Команды Docker выполняйте с правами root, через sudo или из учётной записи с доступом к Docker.</li>
          <li>Проверьте Docker командами ниже. Обе должны завершиться успешно. Если Docker не установлен, попросите администратора установить Docker Engine по инструкции для вашей версии Linux.</li>
          <li>Запросите у администратора платформы адрес manager в частной сети, worker token, версии образов ловушек и параметры доступа к платформе.</li>
          <li>Настройте частную сеть или VPN. Адреса узлов должны быть достижимы друг для друга; публичный адрес за NAT без этой связности не подходит.</li>
          <li>Согласуйте ресурсы под количество и тип ловушек. Проверьте свободное место для образов, память и CPU для контейнеров. Панель не проверяет достаточность ресурсов автоматически.</li>
        </ul>
        <CopyCommand command={'docker version\ndocker info'} label="Скопировать проверку Docker" />
        <p className="text-muted">Если машина уже состоит в другом Swarm, сначала согласуйте перенос с владельцем инфраструктуры или используйте отдельный сервер. Отключение от текущего кластера может нарушить работающие сервисы.</p>
      </section>

      <section className="space-y-3">
        <h3 className="font-semibold">2. Разрешите сетевые соединения</h3>
        <div className="overflow-x-auto"><table className="data-table w-full text-left">
          <caption className="text-left text-muted pb-2">Правила firewall внутри частной сети / VPN</caption>
          <thead><tr className="border-b border-border"><th className="p-2">Порт</th><th className="p-2">Направление</th><th className="p-2">Назначение</th></tr></thead>
          <tbody>
            <tr className="border-b border-border"><td className="p-2">TCP 2377</td><td className="p-2">С worker до manager</td><td className="p-2">Управление кластером</td></tr>
            <tr className="border-b border-border"><td className="p-2">TCP и UDP 7946</td><td className="p-2">Между узлами в обе стороны</td><td className="p-2">Обнаружение и связь узлов</td></tr>
            <tr><td className="p-2">UDP 4789</td><td className="p-2">Между узлами в обе стороны</td><td className="p-2">Сеть контейнеров (overlay)</td></tr>
          </tbody>
        </table></div>
        <p>Не открывайте UDP 4789 в публичный интернет. Инфраструктурные порты разрешайте только между доверенными адресами узлов.</p>
        <p className="text-muted">Контейнерам нужна связь с агентским адресом платформы (AGENT_WS_URL) и центральным Redis по TLS. Администратор платформы проверяет DNS, маршруты и сертификаты из management-сети нового worker. Порты ловушек открываются отдельно на шаге 6.</p>
      </section>

      <section className="space-y-3">
        <h3 className="font-semibold">3. Присоедините сервер к Swarm</h3>
        <p><strong>На manager, администратор платформы:</strong> получите команду присоединения worker.</p>
        <CopyCommand command="docker swarm join-token worker" label="Скопировать получение token" />
        <p><strong>На сервере компании:</strong> откройте «Подключить сервер» вверху страницы, введите выданные адрес с портом и worker token, скопируйте команду и выполните её в терминале. Успешный результат сообщает о присоединении к Swarm в роли worker.</p>
        <p className="text-muted">Если кнопка подключения недоступна, обратитесь к администратору вашей организации в панели. Эта инструкция доступна для чтения всем участникам организации.</p>
        <p className="text-muted">Worker token — секрет доступа к кластеру. Форма не отправляет его в API и очищает после закрытия. Команда, история терминала и буфер обмена могут содержать token: не публикуйте их и не прикладывайте к обращениям в поддержку.</p>
        <p>На сервере компании получите полный ID узла и вставьте его в поле «ID подключённого сервера» в форме:</p>
        <CopyCommand command={"docker info --format '{{.Swarm.NodeID}}'"} label="Скопировать получение ID узла" />
        <p className="text-muted">ID состоит из 25 символов. Пустой результат означает, что сервер ещё не присоединился к кластеру.</p>
      </section>

      <section className="space-y-3">
        <h3 className="font-semibold">4. Закрепите узел за организацией</h3>
        <p><strong>На manager, администратор платформы:</strong> найдите новый узел, сопоставьте hostname и полный ID с сервером компании.</p>
        <CopyCommand command="docker node ls --no-trunc" label="Скопировать список узлов" />
        <p>Форма подключения сформирует команду назначения с ID вашей организации. Передайте её администратору платформы для выполнения на manager. В шаблоне ниже замените NODE_ID на полученный ID:</p>
        <CopyCommand command={`docker node update --label-add honeyforge.organization=${organization ?? 'ORGANIZATION_UUID'} NODE_ID`} label="Скопировать шаблон назначения" />
        {organization && <p className="text-muted">ID вашей организации: <span className="font-mono break-all">{organization}</span>.</p>}
        <p className="text-muted">Узел без корректной метки организации или в роли manager не появится в списке серверов компании. Не назначайте его другой организации, пока на нём есть ловушки прежнего владельца.</p>
      </section>

      <section className="space-y-3">
        <h3 className="font-semibold">5. Загрузите образы и проверьте готовность</h3>
        <p><strong>Администратор платформы:</strong> передайте на worker те же версии образов, которые использует платформа. Для локальных тегов подготовьте архив на машине, где эти образы уже загружены:</p>
        <CopyCommand command="docker save -o honeyforge-decoys.tar honeyforge/tcp-decoy:local honeyforge/redis-decoy:local honeyforge/honeytoken-decoy:local honeyforge/ssh-decoy:local honeyforge/ssh-sandbox:local" label="Скопировать создание архива образов" />
        <p className="text-muted">Это пример тегов. Если версии отличаются, замените их на значения DECOY_TCP_IMAGE, DECOY_REDIS_IMAGE, DECOY_HONEYTOKEN_IMAGE, DECOY_SSH_IMAGE и SSH_SANDBOX_IMAGE из конфигурации платформы. Используйте выданные версии.</p>
        <p><strong>На сервере компании:</strong> получите архив защищённым способом и загрузите образы:</p>
        <CopyCommand command={'docker load -i honeyforge-decoys.tar\ndocker image ls'} label="Скопировать загрузку и проверку образов" />
        <p>Локальные worker и SSH sandbox могут запускаться без скачивания образа. Доступ к registry не заменяет наличие образов на самом сервере. При обновлении платформы загрузите новые версии на каждый используемый worker.</p>
        <p>На manager проверьте состояние узла: STATUS должен быть Ready, AVAILABILITY — Active. Если узел намеренно отключён от выполнения задач, администратор может вернуть его в работу:</p>
        <CopyCommand command="docker node update --availability active NODE_ID" label="Скопировать шаблон включения узла" />
        <p>Вернитесь к списку серверов или нажмите «Проверить подключение» в форме. Список обновляется автоматически. После очередного цикла оркестратора узел появится со статусом «Готов».</p>
        <p className="text-muted">Если оркестратор не подтверждал состояние более 30 секунд, узел считается недоступным. «Готов» подтверждает состояние в кластере; образы, порты и связь агента нужно проверить запуском ловушки.</p>
      </section>

      <section className="space-y-3">
        <h3 className="font-semibold">6. Создайте и проверьте ловушку</h3>
        <ol className="list-decimal pl-5 space-y-2">
          <li>Откройте конструктор сети и создание ловушки. Выберите профиль и укажите подключённый сервер в поле «Где развернуть ловушку». «Сервер платформы» остаётся стандартным вариантом.</li>
          <li>Выберите свободный порт профиля. Панель проверяет конфликты управляемых ловушек, но не знает о сторонних программах на сервере. Одинаковый порт допустим на разных явно выбранных серверах; ловушки без выбранного сервера резервируют порт для всех узлов.</li>
          <li>Разрешите входящие соединения на порт ловушки в firewall компании из нужной сети. На выбранном worker ловушка слушает 0.0.0.0; ограничение доступа задаётся сетевыми правилами компании.</li>
          <li>Создайте ловушку, дождитесь состояния online / running в карточке, проверьте hostname агента и выбранный сервер.</li>
          <li>Подключитесь с проверочной машины к адресу сервера компании и порту профиля по протоколу ловушки (TCP, Redis, HTTP или SSH). Адрес узла в списке может быть внутренним: используйте адрес, доступный из проверочной сети.</li>
          <li>Убедитесь, что событие появилось в панели. Проверьте команды «Остановить» и «Запустить» и смену состояния ловушки.</li>
        </ol>
        <p className="text-muted">Платформа выдаёт реквизиты и сертификат агента, применяет профиль и принимает события штатным способом. При отключении выбранного сервера ловушка ждёт восстановления этого узла. Автоматического переноса нет. Для смены сервера остановите и удалите регистрацию, затем создайте новую на нужном сервере.</p>
      </section>

      <section className="space-y-3">
        <h3 className="font-semibold">Если что-то не работает</h3>
        <dl className="space-y-4">
          <div><dt className="font-medium">Join не выполняется / timeout</dt><dd className="text-muted mt-1">Проверьте Docker, VPN, адрес и TCP 2377 до manager. Уточните актуальный worker token. Если сервер уже в другом Swarm, согласуйте перенос.</dd></div>
          <div><dt className="font-medium">Сервер не появился в панели</dt><dd className="text-muted mt-1">На manager проверьте полный ID, роль worker, метку honeyforge.organization с ID именно вашей организации и состояние Ready / Active. Проверьте работу оркестратора и обновите список. Присоединения без назначения организации недостаточно.</dd></div>
          <div><dt className="font-medium">Сервер «Недоступен»</dt><dd className="text-muted mt-1">Проверьте питание, Docker, VPN и статус узла. Drain / Pause не позволяют создавать новые ловушки. Если все серверы недоступны одновременно, проверьте оркестратор и обновление списка узлов на платформе.</dd></div>
          <div><dt className="font-medium">Ловушка не запускается / No such image</dt><dd className="text-muted mt-1">Проверьте точные теги образов на worker, свободные ресурсы и ошибки задачи на manager. После загрузки образов повторите штатный запуск.</dd></div>
          <div><dt className="font-medium">Порт занят / address already in use</dt><dd className="text-muted mt-1">Проверьте сторонние сервисы и контейнеры. Выберите другой порт профиля или согласуйте освобождение порта. Не останавливайте неизвестные контейнеры. Учитывайте регистрации на сервере платформы.</dd></div>
          <div><dt className="font-medium">Ловушка работает, но подключение не проходит</dt><dd className="text-muted mt-1">Проверьте доступный адрес сервера компании, порт профиля, firewall, маршруты и NAT. Подключайтесь к серверу, на котором создана ловушка.</dd></div>
          <div><dt className="font-medium">Агент offline или события не приходят</dt><dd className="text-muted mt-1">Проверьте доступ из management-сети worker к AGENT_WS_URL и Redis, DNS и TLS-сертификаты, UDP 4789 и TCP/UDP 7946 между узлами. Это выполняет администратор платформы.</dd></div>
        </dl>
        <p><strong>Для диагностики на manager:</strong> замените TRAP_UUID на ID ловушки из её карточки.</p>
        <CopyCommand command={"docker service ps node-metrics-TRAP_UUID --no-trunc\ndocker service inspect node-metrics-TRAP_UUID --format '{{json .Spec.TaskTemplate.Placement.Constraints}}'"} label="Скопировать диагностику размещения" />
        <p className="text-muted">В обращении администратору укажите ID ловушки, ID сервера, время ошибки и её текст. Не передавайте worker token, пароли, приватные ключи и конфигурации с секретами.</p>
      </section>
    </article>
    </details>
  </Card>
}

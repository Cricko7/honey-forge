import { useEffect, useRef } from 'react'
import { Modal } from '@/components/Modal'
import { CopyCommand } from '../servers/CopyCommand'

const testCommand = `$body = @{ action='login'; user='Alice' } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri 'http://localhost:32080/login' -ContentType 'application/json' -Body $body`

export function ConstructorGuide({ open, onClose }: { open: boolean; onClose: () => void }) {
  const content = useRef<HTMLElement>(null)

  useEffect(() => {
    if (!open) return

    const previous = document.activeElement
    content.current?.focus()
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }

    document.addEventListener('keydown', escape)

    return () => {
      document.removeEventListener('keydown', escape)
      if (previous instanceof HTMLElement) previous.focus()
    }
  }, [open, onClose])

  return <Modal open={open} onClose={onClose} title="Как собрать ловушку" width="max-w-4xl">
    <section ref={content} aria-label="Гайд по созданию ловушек" tabIndex={-1}
      className="max-h-[70vh] overflow-y-auto space-y-3 text-sm leading-relaxed outline-none pr-1">
      <p className="text-muted">Доска задаёт поведение ловушки: что она принимает, какие данные проверяет и как отвечает. Начните с короткой рабочей цепочки, затем добавляйте условия и связи.</p>

      <details open className="rounded-xl border border-line p-4 space-y-3">
        <summary className="cursor-pointer font-semibold">1. Первая HTTP-ловушка — по шагам</summary>
        <ol className="list-decimal pl-5 space-y-2">
          <li>Нажмите название текущей доски сверху слева → <strong>«+ Новая доска»</strong>. Назовите её, например, «Тестовый вход».</li>
          <li>Откройте <strong>«Ловушки»</strong> слева или в нижней панели. Добавьте три HTTP-блока: <strong>«HTTP-вход» → «Разобрать запрос» → «HTTP-ответ»</strong>.</li>
          <li>Нажимайте на блок: его настройки открываются справа. Заполните поля по таблице ниже.</li>
          <li>Соедините выход <code>out</code> первого блока со входом <code>in</code> второго, затем выход второго со входом ответа. Ориентируйтесь на подписи портов при соединении.</li>
          <li>Нажмите <strong>«Развернуть»</strong> снизу. Дождитесь работающей ловушки и статуса <code>online</code>, затем отправьте пробный запрос.</li>
        </ol>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead><tr><th className="py-2 pr-4">Блок</th><th className="py-2">Настройки</th></tr></thead>
            <tbody className="divide-y divide-line">
              <tr><td className="py-2 pr-4 align-top">HTTP-вход</td><td className="py-2">Имя сервиса: <code>web</code>; свободный порт: <code>32080</code>; метод: <code>POST</code>; путь: <code>/login</code>.</td></tr>
              <tr><td className="py-2 pr-4 align-top">Разобрать запрос</td><td className="py-2">Формат: <code>json</code>. После этого поля запроса доступны через <code>json</code>.</td></tr>
              <tr><td className="py-2 pr-4 align-top">HTTP-ответ</td><td className="py-2">Статус: <code>200</code>; формат: <code>json</code>; заголовки: <code>{'{}'}</code>; тело: <code>{'{"status":"ok","user":"${json.user}"}'}</code>.</td></tr>
            </tbody>
          </table>
        </div>
        <p>Для локального запуска откройте <strong>PowerShell</strong> и выполните команду. Если ловушка размещена на другом сервере, замените <code>localhost</code> его адресом. Если выбрали другой порт, замените <code>32080</code>.</p>
        <CopyCommand label="Скопировать команду проверки" command={testCommand} />
        <p>Ожидаемый результат: <code>{'{"status":"ok","user":"Alice"}'}</code>. В разделе «События» появятся запрос и ответ, а на открытой доске кратко подсветятся выполненные блоки.</p>
      </details>

      <details className="rounded-xl border border-line p-4 space-y-3">
        <summary className="cursor-pointer font-semibold">2. Соединения и данные — что куда идёт</summary>
        <ul className="list-disc pl-5 space-y-2">
          <li><code>out</code> — обычное продолжение; <code>yes</code> / <code>no</code> — результат условия; <code>error</code> — обработка ошибки. Незадействованная ветка не выполняется.</li>
          <li>Каждый путь запроса должен закончиться <strong>HTTP-ответом</strong> или перенаправлением. Для неправильного JSON добавьте второй ответ со статусом <code>422</code> и подключите к нему <code>error</code> блока разбора.</li>
          <li>В «Условии» укажите источник <code>json.action</code>, оператор <code>equals</code>, значение <code>login</code>. Подключите <code>yes</code> к успешному ответу, а <code>no</code> — к ответу отказа.</li>
          <li>В «Преобразовать данные» задайте переменную <code>user</code>, операцию <code>upper</code>, значение <code>{'"${json.user}"'}</code>. Результат доступен как <code>vars.user</code>; исходный <code>json.user</code> сохраняется.</li>
          <li>«Отправить событие» отмечает выбранный этап. Например, имя <code>login.accepted</code>, важность <code>high</code>, данные <code>{'{"user":"${vars.user}"}'}</code>. Событие появится только при прохождении этой ветки.</li>
        </ul>
        <div className="rounded-lg bg-bg-elevated p-3 space-y-1 text-xs">
          <p><code>json.user</code> — поле входящего JSON. В поле «Источник» пишите его без дополнительных символов.</p>
          <p><code>{'${json.user}'}</code> — подстановка в шаблоне. Например: <code>{'{"user":"${json.user}"}'}</code>.</p>
          <p><code>{'"${json}"'}</code> — передать весь JSON, сохранив его тип. Подстановка числа остаётся числом, объекта — объектом.</p>
        </div>
      </details>

      <details className="rounded-xl border border-line p-4 space-y-3">
        <summary className="cursor-pointer font-semibold">3. Отдельные ловушки — цепочка A → B → C</summary>
        <p>HTTP-блоки одной доски собираются в одну HTTP-ловушку, даже если входов несколько. Для отдельных ловушек создайте отдельные доски.</p>
        <ol className="list-decimal pl-5 space-y-2">
          <li><strong>Получатель B:</strong> создайте HTTP-вход с именем <code>web</code>, своим свободным портом, методом <code>POST</code> и путём <code>/ingest</code>. Соедините его с разбором JSON и ответом. Сначала разверните B.</li>
          <li><strong>Отправитель A:</strong> после разбора запроса добавьте «Передать в отдельную ловушку». Выберите B, имя входа <code>web</code>, метод <code>POST</code>, путь <code>/ingest</code> и JSON-шаблон <code>{'{"user":"${json.user}"}'}</code>.</li>
          <li>Подключите <code>out</code> отправки к ответу <code>202</code>, а <code>error</code> — к ответу <code>503</code>. Разверните A. Связь появится в «Конструкторе сети» как «передача JSON».</li>
          <li>На B используйте <code>json.user</code> для условий и преобразований. Для A → B → C сначала разверните C, затем настройте отправку B → C, после этого A → B.</li>
        </ol>
        <p className="rounded-lg bg-warn/10 p-3">Отправка не ждёт ответа получателя. Выход <code>out</code> означает принятие передачи, а результат обработки проверяйте в событиях B. Получатель может временно быть offline: принятые передачи ждут восстановления связи в пределах срока цепочки. После сбоя обработка может повториться.</p>
        <p>Имя входа, метод и путь должны совпадать с настройками B. Выбирайте ловушку своей организации. Пересылаемый JSON — до 8 КБ; собственное событие — до 4 КБ; срок всей цепочки — 5 минут.</p>
      </details>

      <details className="rounded-xl border border-line p-4 space-y-3">
        <summary className="cursor-pointer font-semibold">4. Параллельные ветки и внутренний вызов</summary>
        <p>«Параллельные ветки» запускают до трёх веток с независимыми копиями данных. Каждую завершите HTTP-ответом. Выход <code>out</code> разветвления подключите к «Собрать результаты», задайте переменную <code>results</code>, затем верните <code>{'"${vars.results}"'}</code> в общем ответе.</p>
        <p>«Вызвать другую ловушку» выбирает HTTP-вход <strong>на этой же доске</strong> и ждёт его ответ. Если переменная результата названа <code>result</code>, тело доступно в <code>vars.result</code>, статус — в <code>vars.result_status</code>.</p>
        <p>Объединение собирает локальные ветки. Оно не ожидает результатов отдельно развёрнутых ловушек, которым отправлен JSON.</p>
      </details>

      <details className="rounded-xl border border-line p-4 space-y-3">
        <summary className="cursor-pointer font-semibold">5. Проверка и ошибки — если не работает</summary>
        <ul className="list-disc pl-5 space-y-2">
          <li><strong>Нет ответа:</strong> проверьте адрес сервера, порт, метод и путь запроса. Открытие адреса в браузере отправляет GET; для входа POST используйте команду проверки.</li>
          <li><strong>Развёртывание отклонено:</strong> прочитайте сообщение, проверьте соединения и JSON в настройках. Порт должен быть свободен на выбранном сервере.</li>
          <li><strong>Получатель не реагирует:</strong> проверьте его статус, имя входа, метод, путь и наличие связи в «Конструкторе сети». Смотрите события обеих ловушек.</li>
          <li><strong>Как проверить условие:</strong> отправьте один подходящий запрос и один неподходящий. Убедитесь, что ответы и собственные события отличаются согласно вашим веткам.</li>
          <li><strong>Красная подсветка и цифры:</strong> подсветка показывает новые события, цифры считают события узла за текущий просмотр. Начало и завершение блока считаются отдельно. Это не число атак.</li>
          <li><strong>Настройки изменены:</strong> дождитесь надписи «сохранено» и снова нажмите «Развернуть», чтобы применить изменения к ловушке.</li>
        </ul>
        <p>«Вписать» показывает всю доску; «Авто-раскладка» размещает блоки; «Экспорт» сохраняет схему в JSON. Для остановки используйте «Остановить».</p>
      </details>

      <details className="rounded-xl border border-line p-4 space-y-3">
        <summary className="cursor-pointer font-semibold">6. TCP-ловушка — когда JSON не нужен</summary>
        <p>Для простого текстового сервиса соедините «TCP-вход» → «Приветствие» → «Запрос → ответ». Во входе задайте свободный порт, например <code>32000</code>. В приветствии укажите текст, который клиент получит сразу после подключения.</p>
        <p>В блоке «Запрос → ответ» выберите точное совпадение, запрос <code>PING</code> и ответ <code>{'PONG\\r\\n'}</code>. Клиент должен завершать запрос переводом строки. Дополнительные правила соединяйте в порядке проверки. Чтобы завершить соединение после ответа, включите «Закрыть» в соответствующем блоке.</p>
        <p>Для запросов с методом, путём и JSON выбирайте HTTP. Передача в отдельную ловушку из этого гайда работает между HTTP-ловушками.</p>
      </details>

      <button type="button" onClick={onClose} className="btn-primary">Понятно, вернуться к доске</button>
    </section>
  </Modal>
}

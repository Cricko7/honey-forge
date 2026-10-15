const updates = [
  {
    date: '2026-10-15', label: '15 октября 2026', title: 'Навигация и помощь',
    items: ['Боковую панель можно свернуть до иконок и развернуть обратно. Выбор сохраняется в этом браузере.', 'Добавлены разделы «История обновлений» и «Техподдержка» с контактами для обращений.'],
  },
  {
    date: '2026-10-15', label: '15 октября 2026', title: 'Управление доступом и удобство работы',
    items: ['Создатель организации может удалять участников с отзывом их сеансов и API-ключей.', 'Обновлены обзор, редактор профилей и отображение важности событий.', 'Улучшены изоляция пользовательских сессий и интерфейс на небольших экранах.'],
  },
  {
    date: '2026-10-14', label: '14 октября 2026', title: 'HTTP-конструктор и права доступа',
    items: ['Добавлены HTTP-ловушки с настраиваемыми цепочками обработки и улучшено сохранение досок конструктора.', 'Добавлена админ-панель с подтверждением пароля и делегированием права управлять ловушками.'],
  },
]

export function UpdatesPage() {
  return <div className="page-content max-w-3xl space-y-6">
    <div>
      <h1 className="text-2xl font-semibold tracking-tight">История обновлений</h1>
      <p className="text-sm text-muted">Что нового в HoneyForge: возможности, удобство и безопасность.</p>
    </div>
    <div className="space-y-4">
      {updates.map(update => <article key={update.title} className="card p-5 space-y-3">
        <time dateTime={update.date} className="text-xs text-muted">{update.label}</time>
        <h2 className="text-lg font-semibold">{update.title}</h2>
        <ul className="list-disc pl-5 space-y-2 text-sm text-muted">{update.items.map(item => <li key={item}>{item}</li>)}</ul>
      </article>)}
    </div>
  </div>
}

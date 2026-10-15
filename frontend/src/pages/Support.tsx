import { ExternalLink, Mail, MessageCircle, ShieldCheck } from 'lucide-react'

export function SupportPage() {
  return <div className="page-content max-w-3xl space-y-6">
    <div>
      <h1 className="text-2xl font-semibold tracking-tight">Техподдержка</h1>
      <p className="text-sm text-muted">Поможем с настройкой HoneyForge и работой конструктора.</p>
    </div>
    <div className="grid sm:grid-cols-2 gap-4">
      <a href="mailto:romanjigg@gmail.com" className="card card-hover p-5 flex items-start gap-3 min-w-0">
        <Mail size={22} aria-hidden="true" className="shrink-0 text-brand mt-1" />
        <div className="min-w-0"><h2 className="font-semibold">Написать на почту</h2><p className="text-sm text-brand break-all mt-2">romanjigg@gmail.com</p><p className="text-xs text-muted mt-2">Откроется почтовое приложение</p></div>
      </a>
      <a href="https://t.me/giraffick" target="_blank" rel="noopener noreferrer" className="card card-hover p-5 flex items-start gap-3 min-w-0">
        <MessageCircle size={22} aria-hidden="true" className="shrink-0 text-brand mt-1" />
        <div className="min-w-0"><h2 className="font-semibold flex items-center gap-2">Написать в Telegram <ExternalLink size={14} aria-hidden="true" /></h2><p className="text-sm text-brand mt-2">@giraffick</p><p className="text-xs text-muted mt-2">Откроется в новой вкладке</p></div>
      </a>
    </div>
    <section className="card p-5 space-y-3" aria-labelledby="support-details">
      <h2 id="support-details" className="font-semibold">Что указать в обращении</h2>
      <p className="text-sm text-muted">Опишите, что вы хотели сделать, какие шаги выполнили и что произошло. Укажите примерное время ошибки и браузер. Если прикладываете скриншот, скройте конфиденциальные данные.</p>
      <div className="flex items-start gap-2 text-sm"><ShieldCheck size={20} aria-hidden="true" className="shrink-0 text-brand" /><p>Не отправляйте пароли, токены, API-ключи, коды приглашения и данные, перехваченные ловушками. Диагностика и данные аккаунта автоматически не отправляются.</p></div>
    </section>
  </div>
}

import { useRef, useState, type FormEvent } from 'react'
import {
  ArrowUpRight,
  Check,
  CheckCheck,
  Mail,
  MessageCircle,
  Send,
  ShieldCheck,
} from 'lucide-react'
import { api, ApiError } from '@/api/client'
import './Support.css'

export function SupportPage() {
  const [sending, setSending] = useState(false)
  const [sent, setSent] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const pending = useRef(false)
  const emailRef = useRef<HTMLInputElement>(null)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()

    if (pending.current) {
      return
    }

    const fields = new FormData(event.currentTarget)
    pending.current = true
    setSending(true)
    setError('')

    try {
      await api.sendSupportMessage({
        email: String(fields.get('email') ?? '').trim(),
        subject: String(fields.get('subject') ?? '').trim(),
        message: String(fields.get('message') ?? '').trim(),
      })

      setSent(true)
    } catch (cause) {
      const code = cause instanceof ApiError ? cause.code : ''
      const errors: Record<string, string> = {
        support_unavailable:
          'Отправка писем пока недоступна. Текст сохранён в форме — свяжитесь с нами в Telegram.',
        demo_unavailable: 'В деморежиме отправка писем недоступна. Напишите нам в Telegram.',
        validation_failed: 'Проверьте адрес почты и заполните тему и описание.',
        unauthenticated: 'Сессия завершена. Войдите в аккаунт и попробуйте снова.',
      }

      setError(
        errors[code] ??
          'Не удалось подтвердить отправку. Текст сохранён. Попробуйте ещё раз или напишите в Telegram.',
      )
    } finally {
      pending.current = false
      setSending(false)
    }
  }

  function startAgain() {
    setSent(false)
    setMessage('')
    requestAnimationFrame(() => emailRef.current?.focus())
  }

  return (
    <div className="page-content support-page">
      <header className="support-heading">
        <h1>Техподдержка</h1>
        <p>
          Разберёмся вместе. Поможем с настройкой HoneyForge
          <br className="hidden sm:block" /> и работой конструктора.
        </p>
      </header>
      <div className="support-grid">
        <section className="support-panel support-compose" aria-labelledby="support-compose-title">
          <div className="support-section-heading">
            <div>
              <h2 id="support-compose-title">Напишите нам</h2>
              <p>Обращение уйдёт на почту поддержки прямо отсюда.</p>
            </div>
            <Mail size={22} strokeWidth={1.5} aria-hidden="true" />
          </div>
          {sent ? (
            <div className="support-success" role="status">
              <span className="support-success-icon">
                <CheckCheck size={30} strokeWidth={1.5} aria-hidden="true" />
              </span>
              <h3>Обращение отправлено</h3>
              <p>
                Письмо передано почтовому серверу.
                <br />
                Ответим на адрес, который вы указали.
              </p>
              <button className="btn-ghost" type="button" onClick={startAgain}>
                Новое обращение <ArrowUpRight size={16} aria-hidden="true" />
              </button>
            </div>
          ) : (
            <form onSubmit={submit} aria-busy={sending}>
              <fieldset disabled={sending} className="support-fields">
                <div>
                  <label htmlFor="support-email">Ваша почта</label>
                  <input
                    ref={emailRef}
                    id="support-email"
                    className="input"
                    name="email"
                    type="email"
                    autoComplete="email"
                    placeholder="you@company.ru"
                    required
                    maxLength={254}
                    aria-describedby="support-email-hint"
                  />
                  <p className="support-field-hint" id="support-email-hint">
                    На этот адрес придёт ответ.
                  </p>
                </div>
                <div>
                  <label htmlFor="support-subject">Тема обращения</label>
                  <input
                    id="support-subject"
                    className="input"
                    name="subject"
                    placeholder="Например, не запускается ловушка"
                    required
                    maxLength={160}
                    pattern=".*\S.*"
                  />
                </div>
                <div>
                  <label htmlFor="support-message">Что случилось?</label>
                  <textarea
                    id="support-message"
                    className="input"
                    name="message"
                    placeholder="Что вы хотели сделать, какие шаги выполнили и что произошло?"
                    rows={6}
                    required
                    maxLength={5000}
                    value={message}
                    onChange={(event) => setMessage(event.target.value)}
                    aria-describedby="support-message-count"
                  />
                  <p id="support-message-count" className="support-counter">
                    {message.length.toLocaleString('ru-RU')} / 5 000
                  </p>
                </div>
              </fieldset>
              {error && (
                <p className="support-error" role="alert">
                  {error}
                </p>
              )}
              <div className="support-submit-row">
                <p>
                  Получатель
                  <br />
                  <span>romanjigg@gmail.com</span>
                </p>
                <button type="submit" className="btn-primary" disabled={sending || !message.trim()}>
                  {sending ? 'Отправляем…' : 'Отправить обращение'}
                  <Send size={16} aria-hidden="true" />
                </button>
              </div>
            </form>
          )}
        </section>
        <section
          className="support-panel support-telegram"
          aria-labelledby="support-telegram-title"
        >
          <div className="support-section-heading">
            <MessageCircle size={25} strokeWidth={1.5} aria-hidden="true" />
            <span className="support-channel">Telegram</span>
          </div>
          <div>
            <h2 id="support-telegram-title">Удобнее в чате?</h2>
            <p>Напишите напрямую. Здесь можно приложить скриншот и обсудить детали.</p>
          </div>
          <a
            href="https://t.me/giraffick"
            target="_blank"
            rel="noopener noreferrer"
            className="support-telegram-link"
            aria-label="Написать @giraffick в Telegram, откроется в новой вкладке"
          >
            @giraffick <ArrowUpRight size={20} aria-hidden="true" />
          </a>
        </section>
        <section className="support-panel support-guide" aria-labelledby="support-details">
          <h2 id="support-details">Чтобы быстрее разобраться</h2>
          <ul>
            <li>
              <Check size={16} aria-hidden="true" />
              <span>Опишите шаги до ошибки</span>
            </li>
            <li>
              <Check size={16} aria-hidden="true" />
              <span>Укажите время и браузер</span>
            </li>
            <li>
              <Check size={16} aria-hidden="true" />
              <span>Расскажите, чего ожидали</span>
            </li>
          </ul>
        </section>
        <section className="support-panel support-privacy" aria-labelledby="support-privacy-title">
          <ShieldCheck size={21} strokeWidth={1.5} aria-hidden="true" />
          <div>
            <h2 id="support-privacy-title">Только нужные детали</h2>
            <p>
              Не отправляйте пароли, токены, API-ключи, коды приглашения и данные ловушек. Данные
              аккаунта и диагностика автоматически не прикладываются.
            </p>
          </div>
        </section>
      </div>
    </div>
  )
}

# HoneyForge

Документ описывает фактически работающую демо-платформу по адресу:
```
https://honey-forge-seg-fault.ru
```
##### *по состоянию на 10 октября 2026.

Без предположений — только то, что подтверждено скриншотами, логами и проверками.


## 1. Что такое HoneyForge (в одном абзаце)
HoneyForge — веб-платформа для развёртывания и управления сетью honeypot-ловушек («декой»). Оператор через веб-интерфейс создаёт профили конфигурации, разворачивает ловушки разных типов (`tcp-banner`, `redis-emulator`), наблюдает за атаками в реальном времени, ставит команды (`start/stop/apply_config`), читает журнал событий с пойманными кредами и payload'ами. Backend — `Go + PostgreSQL + Kafka`, frontend — `React SPA`, всё за `nginx` с `TLS`. Есть отдельный `TCP-runtime` агента (`cmd/agent`), который запускается на хосте с ловушкой и подключается к `API` по `WSS`.

## 2. Скриншот «как есть» — что видит пользователь
URL: `https://honey-forge-seg-fault.ru/login`
![login_screen](screenshots/hf_login.png)

→ после логина попадаем на главную страницу → `/app`
![network_screen](screenshots/hf_app.png)

→ переходим во вкладку "профиль" → `/app/profiles`
![network_prof](screenshots/hf_prof.png)

→ создаём "карточку" профиля → `/app/profiles`
![network_prof](screenshots/hf_prof_card.png)

→ проверяем наличие "карточки" профиля → `/app/profiles`
![network_prof](screenshots/hf_prof_result.png)

→ переходим в раздел "сеть" → `/app/network`
![network_screen](screenshots/hf_network.png)

→ переходим в раздел "добавить ловушку" и добавляем нашу ловушку → `/app/network`
![network_screen](screenshots/hf_network_new-trap.png)

→ проверяем раздел "сеть" ещё раз → `/app/network`
![network_screen](screenshots/hf_network_result.png)

→ кликаем по ловушке и смотрим информацию → `/app/traps/<uuid>`
![traps_screen](screenshots/hf_trap_info.png)

../
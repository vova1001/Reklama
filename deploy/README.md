# Выкладка и переезд

Всё настраивается переменными окружения — один список для всех мест: `.env.example` (локально и свой сервер),
`render.yaml` (Render). Адрес сайта — только `PUBLIC_URL`: из него строятся redirect Google, ссылки, флаг Secure у cookie.
Секреты (`DATABASE_URL`, `RESEND_API_KEY`, `SMTP_PASSWORD`, `TELEGRAM_BOT_TOKEN`, `GOOGLE_CLIENT_SECRET`) — только
в настройках хостинга или в `.env` на сервере, не в git и не в переписке.

| Где | Файлы | Как запускается |
|---|---|---|
| Локально | `docker-compose.yml`, `deploy/nginx.conf` | `make dev` → http://localhost:8000 |
| Render | `render.yaml`, `deploy/render/*` | пуш в ветку сервиса → автовыкладка |
| Свой сервер | `docker-compose.prod.yml`, `deploy/Caddyfile` | `make prod` → https://DOMAIN |

Сейчас: Render, бесплатный тариф, база Neon — подробности в [`render.md`](render.md).

## 1. Бесплатный Render → платный Render

1. `render.yaml`: `plan: free` → `plan: starter` (или `standard`), закоммитить, запушить.
   Или Render → сервис → Settings → Instance Type → Starter.
2. Переменные не меняются. Сайт перестаёт засыпать — пинг `.github/workflows/keepalive.yml` можно удалить
   (при смене домена — поменять в нём адрес).
3. База остаётся в Neon (или Render → New → PostgreSQL → строку подключения — в `DATABASE_URL`, данные перенести:
   `pg_dump "$OLD" | psql "$NEW"`).

## 2. Render → свой сервер (VPS)

1. Сервер с Docker (Ubuntu 24.04, от 1 ГБ памяти), открыты только 22, 80, 443. Вход по ключу.
2. У регистратора домена — запись `A` на IP сервера (и `AAAA`, если есть IPv6).
3. На сервере: `git clone` репозитория → `cp .env.example .env` → заполнить:
   `APP_ENV=production`, `DOMAIN=ohvat.ru`, `PUBLIC_URL=https://ohvat.ru`, `POSTGRES_PASSWORD`, `AUTH_SECRET`
   (`openssl rand -base64 48`; можно взять тот же, что на Render — тогда коды, отправленные до переезда, сработают),
   почта, Telegram, Google — те же значения, что в Render → Environment.
4. Данные: `pg_dump "<DATABASE_URL из Render>" > ohvat.sql` → `make prod` → `docker compose -f docker-compose.prod.yml exec -T db psql -U ohvat ohvat < ohvat.sql`
   (или оставить базу в Neon: `DATABASE_URL=<строка Neon>` в `.env`).
5. `make prod` — Caddy сам получит сертификат. Проверка: `https://DOMAIN/healthz` → `{"status":"ok"}`.
6. Сменился адрес — сделать шаги из раздела 3.

## 3. Смена домена (на любом хостинге)

1. **Адрес**: `PUBLIC_URL=https://новый-домен` — в Render → Environment (и строка `PUBLIC_URL` в `render.yaml`)
   или в `.env` на сервере (+ `DOMAIN`). Render: Settings → Custom Domains → добавить домен, у регистратора — `CNAME`
   на `ohvat-…onrender.com` (для корня домена — `A` на адрес из подсказки Render).
2. **Telegram**: @BotFather → `/setdomain` → бот `reklama_auth_bot` → новый домен (без https://). Без этого окно входа
   Telegram пишет «Bot domain invalid».
3. **Google**: console.cloud.google.com → APIs & Services → Credentials → OAuth client → Authorized redirect URIs —
   добавить `https://новый-домен/api/auth/google/callback` (старый удалить после переезда); OAuth consent screen →
   Authorized domains — новый домен.
4. **Почта (Resend)**: resend.com → Domains → Add → новый домен → DNS-записи (SPF, DKIM) у регистратора → Verify;
   затем `MAIL_FROM=Охват <hello@новый-домен>`. До подтверждения домена письма с `onboarding@resend.dev` доходят только
   на почту владельца аккаунта Resend.
5. Выкладка (пуш или `make prod`) → проверить: вход по почте, Telegram, Google.

## Почта без Resend (временно)

`MAIL_PROVIDER=smtp`, `SMTP_HOST=smtp.yandex.ru` (или `smtp.mail.ru`), `SMTP_PORT=465`, `SMTP_USER=ящик@yandex.ru`,
`SMTP_PASSWORD=<пароль приложения>` (Яндекс ID → Безопасность → Пароли приложений; не пароль от почты).
На бесплатном Render исходящий SMTP может быть закрыт — тогда только Resend.

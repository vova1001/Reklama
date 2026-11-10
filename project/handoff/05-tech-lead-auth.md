# 05. tech-lead: регистрация и вход — почта, Telegram, Google

## Задача (менеджер)
- Цель: регистрация и вход для двух ролей — **бизнес** и **автор**. Способы: почта (код из письма, без пароля), Telegram, Google (OAuth).
- Дизайн: ориентир — стиль goesti.ru (формы, расстановка, крупная типографика, пилюли, много воздуха), **ничего не копировать**;
  цвета, шрифт и компоненты — наши, как на главной (Inter 4, тёмный/светлый с лестницей уровней, изумруд-акцент, без мелкого шрифта).
- Экраны (375/768/1440):
  1. `/login` — одна страница входа и регистрации: заголовок, переключатель роли «Я бизнес / Я автор» (при регистрации), кнопки «Войти через Google», «Войти через Telegram», разделитель «или», поле почты → «Получить код».
  2. Ввод кода из письма (6 цифр, автопереход, «Отправить ещё раз» через 60 с, ошибки понятным языком).
  3. Первый вход — короткая анкета: роль (если не выбрана), имя/название, город; для бизнеса — сфера (теги), для автора — ссылки на каналы. Минимум полей.
  4. После входа — заглушка кабинета `/account` (имя, роль, «Выйти»). Шапка: вместо «Войти» — аватар/имя.
- Бэкенд (Go, три слоя, как в проекте): таблицы users, identities (provider: email/telegram/google), login_codes, sessions; миграции.
  - Почта: одноразовый код 6 цифр, хранить только хеш, срок 10 мин, 5 попыток, ограничение частоты отправки (на почту и IP). Отправка писем — через интерфейс; реализация Resend (ключ `RESEND_API_KEY`); если ключа нет — код пишется в журнал сервера (режим разработки).
  - Telegram: Telegram Login Widget, проверка подписи HMAC по токену бота (`TELEGRAM_BOT_TOKEN`), проверка `auth_date`. Без токена — кнопка скрыта.
  - Google: OAuth 2.0 Authorization Code + PKCE + state, `GOOGLE_CLIENT_ID`/`GOOGLE_CLIENT_SECRET`, redirect `${PUBLIC_URL}/api/auth/google/callback`. Без ключей — кнопка скрыта.
  - Связывание: один пользователь может войти разными способами; совпадение подтверждённой почты Google и почты пользователя — тот же аккаунт.
  - Сессия: httpOnly, Secure, SameSite=Lax cookie, хранится в БД (хеш), выход удаляет; CSRF-защита для POST.
  - Контракт — дополнить `backend/api/openapi.yaml`.
- Тесты: модульные (сервис: коды, лимиты, подпись Telegram), интеграционные (repository), e2e: вход по почте с кодом из журнала/тестового режима, анкета, выход; скриншоты экранов в `project/screens/auth-*.png`.
- Ключи — только в переменных окружения, `.env.example` дополнить; в git ничего секретного.
- Сдать: коммиты в ветку (пушит менеджер), отчёт сюда + список, какие ключи нужны от заказчика и как их получить (коротко).

## Отчёт (техлид)
- **Сделано, полностью рабочее (без «скоро»).** Вход и регистрация для ролей «бизнес» и «автор»: почта (код 6 цифр без пароля), Telegram, Google;
  анкета первого входа; кабинет `/account` («Выйти»); в шапке вместо «Войти» — аватар и имя. Стиль — как на главной:
  Inter, изумруд, пилюли, светлая лестница. Все состояния экранов: загрузка (спиннер в кнопке), нет сети, пустые и неверные
  поля, нет согласия, неверный код (сколько попыток осталось), код устарел или попытки кончились (кнопка «Отправить ещё раз»),
  лимит 429 с обратным отсчётом, успех. Управление с клавиатуры (роль стрелками, свой чекбокс пробелом), 375/768/1440.
  Специалистов звать нельзя — работу backend, frontend и проверку безопасности сделал сам по их инструкциям.
- **Бэкенд строго в три слоя:** transport/http → service → repository, плюс адаптеры внешних сервисов (`gateway/`). Слои
  связаны только через интерфейсы, сборка через конструкторы в `main.go`. В обработчиках нет логики, SQL есть только
  в `queries/` (sqlc) → repository.
- **Безопасность:** код хранится только как HMAC (ключ `AUTH_SECRET`), токен сессии — как SHA-256; сравнения за постоянное время;
  CSRF (заголовок = cookie + проверка Origin); cookie httpOnly, Secure (на https), SameSite=Lax; Google — state + PKCE S256;
  Telegram — HMAC по токену бота, `auth_date` не старше часа. Лимиты: на почту — 1 письмо в 60 с и 5 в час, с адреса — 20
  в час (считаются в БД); 5 попыток на код (атомарно в БД); токен-бакет по адресу на все `/api/auth/*` (429 + Retry-After).
  В журнале почта маскируется (a***@mail.ru), код пишется в журнал только при `APP_ENV=development`. Согласие на обработку
  данных обязательно (152-ФЗ), его проверяет сервер. govulncheck: уязвимый код не вызывается (pgx → 5.9.2, x/text → 0.41,
  поэтому Go 1.25); npm audit — 0.
- **Горутины:** письма уходят через очередь с пулом воркеров (повторы с растущей паузой, дедлайн на попытку, при остановке
  дожидается отправки), есть фоновая очистка кодов и сессий и очистка лимитов. Всё под одним `errgroup` и контекстом
  сигнала. Таймауты: у HTTP-сервера (заголовки, чтение, запись, простой); у Google и Resend — 10 с; у SMTP — по дедлайну.
- **Проверено:** `go test -race` и `make test` (Docker, PostgreSQL) зелёные, покрытие service+repository+transport —
  94,2%; e2e Playwright 49 пройдено, 2 пропущены по замыслу (вход по почте с кодом из журнала, ошибки, анкета, кабинет,
  выход, 429, CSRF); `astro check` — 0 ошибок; горизонтальной прокрутки нет на 320/360. Образ Render собран и запущен
  с `APP_ENV=production`: вход по почте без ключа скрыт, Telegram виден, cookie с Secure.
  Не проверено: настоящие окна Google и Telegram (нужен человек и ключи на Render), golangci-lint (только в CI), Lighthouse
  для новых страниц.

### Изменения по слоям (backend/internal)
```
transport/http/            (перенесён из internal/httpapi)
  server.go       изм.  маршруты; интерфейсы HomeService/HealthService/AuthService; Options (Origin, Secure, лимитер)
  middleware.go   нов.  request_id → журнал, логи без строки запроса, паника, no-store, адрес клиента (X-Real-IP от прокси), CSRF, requireUser, cookie
  ratelimit.go    нов.  токен-бакет по адресу, очистка ключей по тикеру, 429 + Retry-After
  auth.go         нов.  обработчики /api/auth/*, /api/me*; перевод ошибок сервиса в коды ответа
  auth_dto.go     нов.  тела запросов и ответы по openapi
  dto.go          изм.  ошибка: fields, retryAfter, attemptsLeft
  *_test.go       нов./изм.  CSRF, cookie, 429, карта ошибок, Google-callback, Telegram, me/profile/logout
service/
  auth.go         нов.  интерфейсы User/Code/SessionRepository, Mailer, GoogleProvider, Clock; ошибки; Auth: вход, связывание, сессии, очистка
  auth_email.go   нов.  код: нормализация почты, лимиты, HMAC, попытки, одноразовость, маска почты
  auth_telegram.go нов. проверка подписи Telegram Login, вход
  auth_google.go  нов.  state + PKCE, вход по профилю Google
  auth_profile.go нов.  анкета: роль, имя, город, сферы (из справочника) / каналы (https или @)
  health.go, random.go  нов.  проверка живости; коды и токены из crypto/rand
  auth_test.go, fakes_test.go  нов.  табличные тесты на фейках
repository/
  auth.go         нов.  пользователи, способы входа, коды, сессии; транзакции; pgx → ErrNotFound/ErrConflict
  health.go       нов.  ping базы
  auth_test.go    нов.  интеграционные на PostgreSQL
gateway/  (внешние сервисы за интерфейсами сервиса)
  mail/queue.go     нов.  очередь: воркеры, повторы, плавная остановка
  mail/senders.go   нов.  Resend (HTTPS), SMTP (465 TLS / 587 STARTTLS), журнал (только development)
  mail/templates.go нов.  письмо с кодом в цветах сайта
  google/google.go  нов.  OAuth: адрес входа, обмен кода с verifier, профиль
domain/   user.go, errors.go  нов.  User, Identity, LoginCode, Profile; ErrNotFound/ErrConflict
config/   config.go изм., config_test.go нов.  все переменные, APP_ENV, MAIL_PROVIDER, лимиты, проверки
logx/     logx.go нов.  request_id в каждой записи журнала
store/    auth.sql.go нов., models.go изм.  код sqlc
cmd/server/main.go изм.  сборка слоёв, errgroup: HTTP, очередь писем, очистки, остановка по сигналу
```
- **Миграции:** `backend/migrations/00003_auth.sql` — users, identities, login_codes, sessions (up/down); запросы — `backend/queries/auth.sql`.
- **Контракт:** `backend/api/openapi.yaml` 0.2.0 — 9 новых путей, схемы, CSRF, ошибки 400/401/403/409/429/503.
- **Фронтенд:** `src/pages/login.astro` и `src/pages/account.astro` (новые), `src/scripts/auth.ts`, `src/styles/auth.css`,
  `src/middleware.ts`, `src/lib/auth.ts`, `src/components/Avatar.astro`, `src/env.d.ts`, иконки (новые);
  `Header.astro`, `global.css`, `index.astro` (кэш private), `routes.ts`, `api.gen.ts` (изменены).
- **Тесты:** `frontend/tests/e2e/auth.spec.ts`, `scripts/screens-auth.mjs` → `project/screens/auth-*.png` (вход, ошибки, код,
  ошибка кода, анкета бизнеса и автора, кабинет, шапка; 375/768/1440). CI: `-race`, Go 1.25.
- **Выкладка и переезд:** `render.yaml` — полный Blueprint (все переменные с комментариями, секреты — `sync: false`,
  `AUTH_SECRET` создаётся автоматически, строки `[ПЕРЕЕЗД]`); `.env.example` разбит по группам; `docker-compose.prod.yml` +
  `deploy/Caddyfile` (https по DOMAIN); `deploy/README.md` — три сценария переезда; nginx Render берёт адрес посетителя
  из True-Client-IP. Переменные Telegram и Google — ровно `TELEGRAM_BOT_TOKEN/NAME` и `GOOGLE_CLIENT_ID/SECRET`; redirect —
  `${PUBLIC_URL}/api/auth/google/callback` = `https://ohvat-jif6.onrender.com/api/auth/google/callback`.

### Ключи от заказчика
1. **RESEND_API_KEY** — без него на Render вход по почте скрыт: в боевом режиме коды не пишутся в журнал. resend.com →
   API Keys → Create. Пока домен не подтверждён, письма доходят только на почту владельца аккаунта Resend; для всех
   адресов нужен свой домен (Domains → DNS-записи) и `MAIL_FROM`. Временно можно отправлять через SMTP Яндекса или Mail.ru
   (пароль приложения), но на бесплатном Render SMTP может быть закрыт.
2. **Telegram** — ключи уже в Render. Нужно: в @BotFather → `/setdomain` → `ohvat-jif6.onrender.com`, иначе окно входа
   покажет «Bot domain invalid».
3. **Google** — ключи уже в Render. Проверить: на экране согласия OAuth приложение опубликовано (или почта добавлена
   в тестовые пользователи).

### Вопросы к заказчику
1. Тексты условий и согласия на обработку данных: ссылки ведут на `/docs/terms` и `/docs/consent`, где пока заглушки —
   нужны тексты (юрист / legal).
2. Роль после анкеты не меняется: для второй роли нужен второй аккаунт. Подходит ли так?

### Сверх задачи (на одобрение)
1. Обязательное согласие на обработку данных перед любым способом входа (правило студии, 152-ФЗ).
2. Способы входа на странице кабинета (Почта / Telegram / Google).
3. Go 1.25 вместо 1.24 — из-за исправлений уязвимостей в pgx и x/text.
4. Проверить после выкладки: адрес посетителя берётся из True-Client-IP (Cloudflare перед Render); если этого заголовка
   нет — из X-Forwarded-For. Признак сбоя — у разных людей одновременно срабатывает лимит «20 писем в час с адреса».

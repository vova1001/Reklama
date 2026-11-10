# 06. tech-lead: вход — минималистичный дизайн, Telegram-бот, полная проверка

## Задача (менеджер, по словам заказчика)
1. **Дизайн входа и кабинета — минималистичный, как у goesti.ru и Reddit** (стиль, не копия; палитра наша):
   по центру одна узкая карточка (~400px), логотип-знак сверху, короткий заголовок («Вход в Охват»), минимум текста;
   крупные кнопки-пилюли способов входа одна под другой (Telegram, Google, почта), переключатель роли — компактный сегмент;
   согласие — одна строка мелко-нейтрально (≥ 14px) под кнопками; без левой колонки с преимуществами, без лишних плашек.
   Ввод кода — 6 ячеек, автопереход. Анкета — шагами по одному вопросу или один короткий экран. Кабинет — чисто и просто.
2. **Telegram-бот @reklama_auth_bot** (Go, в том же сервисе, те же три слоя; transport/telegram как отдельный вход):
   - вход через бота: на /login кнопка «Войти через Telegram» открывает `t.me/reklama_auth_bot?start=<одноразовый токен>`;
     бот по /start с токеном подтверждает вход («Подтвердить вход» inline-кнопкой), сайт ждёт подтверждения (опрос/long-poll,
     токен живёт 5 мин, одноразовый, хеш в БД) и создаёт сессию. Это надёжнее виджета (не нужен /setdomain); виджет оставить запасным, если работает.
   - команды: /start (приветствие, кнопки «Открыть сайт», «Войти»), /help, /profile (кто я, роль), /stop (отключить уведомления);
   - уведомления (каркас + готовые типы): вход в аккаунт с нового устройства; позже — новые задания по теме, отклики, выплаты (интерфейс Notifier в service, реализация — бот);
   - webhook `POST /api/telegram/webhook` с секретом `X-Telegram-Bot-Api-Secret-Token` (TELEGRAM_WEBHOOK_SECRET), установка вебхука при старте на `${PUBLIC_URL}`; лимиты, таймауты, очередь отправки сообщений (лимиты Telegram ~30 msg/s), повторы.
   - связывание: вход через бота = identity provider telegram (тот же, что у виджета).
3. **Проверка, что всё работает:** e2e на все сценарии (почта с кодом, бот — эмуляция webhook, Google — эмуляция callback, ошибки, лимиты, выход),
   модульные и интеграционные тесты, -race. После выкладки — проверка на живом сайте (менеджер сделает выкладку): /api/auth/config, страницы, вебхук бота установлен (getWebhookInfo).
- Отчёт по слоям (папка → файл → что сделано), как в задаче 05. Коммит в ветку, не пушить. Новые переменные — в render.yaml/.env.example.

## Отчёт (техлид)
- **Сделано.** Вход и кабинет в минималистичном стиле: узкая карточка по центру (≤420px), знак сверху, «Вход в Охват»,
  роль — компактный сегмент, способы входа — пилюли одна под другой (Telegram, Google, почта), согласие — одна строка
  14px под кнопками; код — 6 ячеек с автопереходом; анкета — один короткий экран; кабинет — одна карточка.
  Бот @reklama_auth_bot в том же сервисе (те же три слоя): вход по ссылке `t.me/<бот>?start=login_<токен>` →
  «Подтвердить вход» / «Это не я» → страница сама открывает кабинет (long-poll); /start, /help, /profile, /stop;
  уведомление «вход с нового устройства» с кнопкой «Это не я — выйти везде»; задания, отклики и выплаты — готовые типы
  за интерфейсом `Notifier`. Окно-виджет оставлен запасным («Не открывается? Войти через окно Telegram»).
  Почта: Resend 4xx (кроме 429) больше не повторяется (сразу ошибка в журнал), 429 — по Retry-After, 5xx и сеть — повторы.
- **Безопасность и лимиты.** В базе только SHA-256 токена из ссылки и секрета браузера (httpOnly-cookie `ohvat_tglogin`):
  чужой браузер по ссылке вход не заберёт; токен одноразовый (атомарный UPDATE), живёт 5 мин, start-параметр 38 из 64
  символов; лимит — 30 запусков в час с адреса + общий токен-бакет `/api/auth/*`. Вебхук: секрет в
  `X-Telegram-Bot-Api-Secret-Token` (сравнение за постоянное время), вне CSRF, тело ≤1 МБ, ≤32 одновременных (503 —
  Telegram повторит), таймаут 8 с, дедупликация `update_id`, флуд одного человека отсекается. Отправка — очередь:
  25 сообщений/с на бота, ≥1 с между сообщениями в чат, порядок в чате сохраняется, 429 — пауза по `retry_after`,
  403 — чат помечается «бот остановлен». Токен бота не попадает в ошибки и журнал (адрес запроса вырезается).
- **Горутины.** Под общим `errgroup` и контекстом сигнала: HTTP, очередь писем, очередь бота (досылает при остановке ≤10 с),
  очистка ограничителя бота, настройка бота при старте (getMe → setMyCommands → setWebhook на
  `${PUBLIC_URL}/api/telegram/webhook` с secret_token → getWebhookInfo в журнал; повтор 5 с → 5 мин, сайт не ждёт),
  при `TELEGRAM_MODE=polling` — getUpdates.
- **Проверено.** `go vet`, gofmt, `go test -race` — зелёные; с PostgreSQL покрытие service+repository+transport+gateway/telegram —
  93,9%. e2e Playwright (стек `make e2e-up`: бот против подставного Bot API, реальный Telegram не вызывается) — 64 пройдено,
  2 пропущены по замыслу: вход через бота (эмуляция вебхука /start и нажатий), «Это не я», команды, чужой секрет, повтор
  update_id, почта с кодом, ошибки, 429, анкета, выход, Google (state/PKCE, эмуляция callback с чужим state и отменой),
  CSRF, без прокрутки вбок на 320–1440. `astro check` — 0 ошибок. Скриншоты не делались (по решению заказчика).
- **Проверить после выкладки (менеджер):** `/api/auth/config` → `telegram.bot: true`; в журнале Render строка
  «бот: настроен» с `webhook=https://ohvat-jif6.onrender.com/api/telegram/webhook` и пустым `last_error`; getWebhookInfo
  (из журнала или вручную); «Войти через Telegram» → бот → «Подтвердить вход» — финальный вход делает человек.

### Изменения по слоям (backend)
```
transport/telegram/          (новый вход — бот)
  webhook.go      нов.  POST /api/telegram/webhook: секрет, лимит одновременных, размер, таймаут, дедупликация, флуд
  dto.go          нов.  Update Telegram → service.BotUpdate (только нужные поля, обрезка длинных)
  poller.go       нов.  запасной приём getUpdates (TELEGRAM_MODE=polling), повтор необработанного обновления
  webhook_test.go нов.
transport/http/
  server.go       изм.  маршрут вебхука вне CSRF; AuthService + StartTelegramBot/WaitTelegramBot; BotWait 25 с
  auth.go         изм.  POST /api/auth/telegram/bot/start, /status (long-poll, сессия); ошибки 410 login_expired/cancelled
  middleware.go   изм.  client: адрес, браузер, cookie устройства → контекст сервиса
  auth_dto.go     изм.  telegram.bot, BotStart/BotLink/BotStatus
  bot_test.go     нов.
service/
  auth_telegram_bot.go нов. вход через бота: токен, лимит, подтверждение/отмена, ожидание с сигналом, LogoutEverywhere
  bot.go          нов.  бот: команды, кнопки, «выйти везде», «бот остановлен»; интерфейсы BotMessenger/BotChatRepository/BotAuth
  bot_texts.go    нов.  тексты бота и уведомлений (HTML, экранирование, МСК)
  bot_notify.go   нов.  BotNotifier — реализация Notifier через бота
  notify.go       нов.  Notifier, типы уведомлений, ClientInfo, DescribeUA, MaskIP
  auth_device.go  нов.  устройство при входе → уведомление «вход с нового устройства»
  auth.go         изм.  зависимости BotLogins/Devices/Notifier, Methods.TelegramBot, очистка входов через бота
  *_test.go       нов./изм.  фейки и табличные тесты
repository/
  telegram.go     нов.  входы через бота, чаты, устройства; auth.go изм. — DeleteUserSessions
  telegram_test.go нов. интеграционные на PostgreSQL (в т.ч. гонка «забрать вход»)
gateway/telegram/
  client.go       нов.  Bot API: таймауты через контекст, разбор ошибок, токен вырезается из ошибок
  outbox.go       нов.  очередь: шарды по чату, общий темп, пауза в чате, повторы, 429/403
  setup.go        нов.  getMe → команды → setWebhook/deleteWebhook → getWebhookInfo, повторы
gateway/mail/     senders.go, queue.go изм.  SendError: 4xx не повторяется, 429 — Retry-After; mail_test.go изм.
domain/telegram.go нов.  TelegramProfile, TelegramLogin, TelegramChat
config/           изм.  TELEGRAM_WEBHOOK_SECRET, TELEGRAM_MODE, TELEGRAM_API_URL, TELEGRAM_LOGIN_TTL_MIN, TELEGRAM_LOGIN_PER_IP_HOUR, TELEGRAM_SEND_RPS
migrations/00004_telegram_bot.sql нов.  telegram_logins, telegram_chats, user_devices (up/down); queries/telegram.sql → store/telegram.sql.go
cmd/server/main.go изм.  сборка бота; cmd/faketelegram нов. — подставной Bot API только для e2e
```
- **Фронтенд:** `pages/login.astro`, `pages/account.astro`, `styles/auth.css` — новый дизайн; `scripts/auth.ts` — панели,
  вход через бота (вкладка открывается сразу на компьютере, на телефоне — кнопка «Открыть Telegram»), ожидание, отмена,
  новая ссылка, запасной виджет; `lib/api.gen.ts`. Тесты: `tests/e2e/telegram.spec.ts` (нов.), `auth.spec.ts` (изм.).
- **Окружение:** `render.yaml`, `.env.example`, `docker-compose.yml` (+ `-race` в `make test`), `docker-compose.e2e.yml`
  (нов.), `Makefile` (`make e2e-up`), `backend/Dockerfile`, `api/openapi.yaml` 0.3.0.

### Вопросы к заказчику
1. Согласие теперь — строка «Продолжая, вы принимаете…» без галочки (как просили). Тексты `/docs/terms` и `/docs/consent`
   по-прежнему заглушки — нужны тексты (legal).

### Сверх задачи (на одобрение)
1. Кнопка «Это не я — выйти везде» в уведомлении о входе (завершает все сеансы этого аккаунта).
2. Строка «Уведомления» в кабинете (в Telegram-боте / как подключить).
3. `TELEGRAM_MODE=polling` — приём сообщений без https (локально); подставной Bot API `cmd/faketelegram` для e2e.

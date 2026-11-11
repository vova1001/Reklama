# Архитектура «Охват»

Этап: главная, вход, приложение с режимами, задания и анкеты с модерацией, фото (задачи `handoff/01`–`08`).

## Схема

```
браузер → nginx (web, :8000) ─┬─ /            → frontend (Astro SSR, Node, :4321) ──→ GET /api/home, /api/me (cookie)
                              └─ /api, /healthz → backend (Go, :8080) → PostgreSQL 16 (db)
                                                     └→ Resend / SMTP (письма), Google OAuth (исходящие, таймаут 10 с)
```
Render — один контейнер (nginx + Astro + Go), свой сервер — `docker-compose.prod.yml` с Caddy (https). Настройки —
только переменные окружения (`.env.example`, `render.yaml`), адрес — только `PUBLIC_URL`; переезд — `deploy/README.md`.

Всё в Docker Compose: `db`, `backend`, `frontend`, `web`; для тестов — профиль `test` (`db-test` в памяти, `backend-test`).
Один адрес для сайта и API — cookie входа без CORS.

## Бэкенд (Go 1.25, chi, slog, pgx, sqlc, goose)

Три слоя, зависимости только сверху вниз:

| Слой | Пакет | Что делает |
|---|---|---|
| транспорт | `internal/transport/http` | маршруты chi, обработчики по `api/openapi.yaml` (только разбор запроса → вызов сервиса → ответ), DTO, единый формат ошибки `{error:{code,message,fields?,retryAfter?,attemptsLeft?}}`, middleware: request_id, журнал, паника, CSRF, лимиты через интерфейс `Limiter` (весь `/api` — по адресу, вход — строже по адресу, `/api/me*` — по пользователю; 429 + Retry-After), сессия, `no-store` |
| сервис | `internal/service` | бизнес-логика без HTTP и SQL: главная; вход — коды (HMAC, срок, попытки, лимиты почта/адрес), подпись Telegram, Google + PKCE, связывание аккаунтов по подтверждённой почте, сессии (SHA-256), фоновая очистка; `Profiles` — режим аккаунта «Бизнес / Автор», общие данные, профили режимов, прогресс заполнения |
| репозиторий | `internal/repository` (+ `internal/store` — код sqlc) | PostgreSQL, транзакции, ошибки pgx → `domain.ErrNotFound/ErrConflict`; SQL — только в `queries/*.sql` |
| лимиты | `internal/ratelimit` | токен-бакет за интерфейсом `Limiter`: `Memory` (один сервер), `Redis` (атомарный Lua-скрипт, время Redis, `REDIS_URL`), `Fallback` (Redis → память при сбое) |
| адаптеры | `internal/gateway/{mail,google}` | внешние сервисы за интерфейсами сервиса: очередь писем (воркеры, повторы, плавная остановка) поверх Resend / SMTP / журнала; OAuth-клиент Google |

Сервис зависит только от интерфейсов (`UserRepository`, `CodeRepository`, `SessionRepository`, `Mailer`, `GoogleProvider`,
`Clock`, источник случайности), транспорт — от интерфейсов `HomeService`, `HealthService`, `AuthService`; сборка — в
`cmd/server/main.go` (конструкторы), фоновые задачи (HTTP, очередь писем, очистка кодов и сессий, очистка лимитов) —
под одним `errgroup` и контекстом сигнала, остановка с ожиданием.
Миграции встроены в бинарник и применяются при старте (`migrations/`, goose, up и down).

### База

| Таблица | Поля | Индекс |
|---|---|---|
| `categories` | slug (уник.), title, icon (Lucide), position, is_active | position, где is_active |
| `plans` | audience (`business`/`creator`), slug, name, subtitle, price_kopecks, period (`month`), recommended, position, is_active; уник. (audience, slug) | (audience, position), где is_active |
| `plan_features` | plan_id → plans (каскад), position, text | PK (plan_id, position) |
| `faq` | question, answer, position, is_published | position, где is_published |
| `users` | name, email (подтверждённая, NULL), avatar_url, city, active_mode (`business`/`creator`, по умолчанию business), onboarded_at (общие данные сохранялись), last_login_at | уник. email, где не NULL |
| `tasks` | owner_id → users, status (`draft`/`pending_review`/`published`/`rejected`), reject_reason, title, city, nationwide, categories[] (slug, до 5; задача 11), formats[] (до 3, свои — названием), access (`open`/`approval`), min_views, min_likes, reward_kopecks, publish_by (date), blocks (jsonb), cover → media, photos[], submitted_at, published_at | (owner_id, updated_at); лента (published_at, id) где published; очередь проверки где pending |
| `creator_cards` | user_id → users (PK), status, reject_reason, name, city, topics[], platforms (jsonb: kind, url, followers), blocks (jsonb), avatar → media, works[] | опубликованные; очередь проверки |
| `moderation_log` | subject (`task`/`creator_card`/`category`), subject_id, from/to status, reason, actor_id, created_at — каждая смена статуса | (subject, subject_id); отправки за сутки по actor_id |
| `media` | id (128 случайных бит), owner_id, purpose, width, height, bytes, subject/subject_id (к чему прикреплено; NULL — сирота), touched_at | владелец; сироты; прикреплённые |
| `media_blobs` | key, content_type, data (bytea) — хранилище файлов при `MEDIA_STORE=postgres` | PK |
| `business_profiles` | user_id → users (PK, каскад), name, city, spheres[] — 1:1, строки нет, пока профиль не заполнен | PK |
| `creator_profiles` | user_id → users (PK, каскад), name, city, spheres[], channels[] — 1:1, nullable | PK |
| `identities` | user_id → users (каскад), provider (`email`/`telegram`/`google`), subject, email | уник. (provider, subject); user_id |
| `login_codes` | email, code_hash (HMAC), ip, attempts, expires_at, consumed_at; строки живут сутки — по ним лимиты | (email, created_at), (ip, created_at), created_at |
| `sessions` | user_id → users (каскад), token_hash (SHA-256, уник.), expires_at | user_id, expires_at |

`categories` (задача 08): + status (`approved` — общая, `pending_review` — своя на проверке, `rejected`), author_id,
norm (нижний регистр, ё → е, одиночные пробелы); уникальность norm среди общих и среди своих одного автора.

Деньги — целые копейки; время — `timestamptz` (UTC). Объём данных главной — десятки строк, нагрузка минимальна.

### Задания, анкеты, модерация, фото (задача 08)

- **Статусы:** `draft → pending_review → published | rejected`; владелец: отправить (из черновика и отклонённого),
  вернуть с проверки в черновик; модератор: опубликовать или отклонить с причиной. Таблица переходов — в `domain`,
  смена — `UPDATE … WHERE status = <ожидаемый>` + запись в `moderation_log` в одной транзакции (без гонок).
- **Проверка на сервере в два уровня:** «форма» при каждом сохранении (длины, числа, перечни, ссылки https, до 8 блоков
  × 15 пунктов, 6000 знаков, свои фото нужного назначения) и «полнота» при отправке. Ошибки — по полям (`blocks.N`).
- **Лимиты:** заданий на пользователя (`TASKS_MAX_PER_USER`), отправок на проверку в сутки (`MODERATION_SUBMITS_PER_DAY`,
  по журналу), загрузок фото по пользователю (Limiter), квота фото (число и байты), своих категорий на проверке — 10,
  в анкете — до 3.
- **Модерация без админки:** `service.Moderation` за интерфейсом `service.Moderator` (`Decide`, `History`) — админка
  подключит его своим транспортом. Свои категории после одобрения становятся общими (на главной — в конце списка).
- **Фото:** `POST /api/me/media` (multipart, до 10 МБ) → тип по содержимому (JPEG, PNG, WebP, HEIC; GIF, SVG,
  анимация — нет) → лимит пикселей до декодирования → уменьшение, поворот по EXIF, обрезка под назначение → WebP
  320/800/1600 без метаданных. Обработка — `gateway/imaging.Pool` (воркеры, очередь, контекст; переполнено — 503 busy).
  Хранилище — интерфейс `service.BlobStore`: `gateway/storage.Postgres` или `S3` (AWS Signature V4, R2/Яндекс/MinIO) —
  выбор переменной `MEDIA_STORE`. Отдача — `GET /media/{id}/{size}.webp`: аватар и опубликованное — всем с долгим
  кэшем, остальное — только владельцу (чужое закрытое — 404); ETag. Неприкреплённые удаляются через сутки (фоново).

### Автопроверка, лента у всех, редактор на весь экран (задача 09)

- **Проверка:** `MODERATION_MODE=auto` (по умолчанию) — при отправке цепочка `→ pending_review` (владелец) `→ published`
  (система, `moderation_log.auto = true`, «одобрено автоматически») одной транзакцией (`domain.SubmitChain`,
  `SetTaskStatus/SetCardStatus(ctx, chain...)`); `manual` — ждёт модератора. Опубликованное владелец снимает в черновик,
  чтобы изменить (`…/withdraw`).
- **Полнота:** анкете — только имя, город, темы, площадки с подписчиками (блоки и фото — по желанию); заголовок блока не
  обязателен. Тексты ошибок фронта — те же, что у сервера (`frontend/src/content/app.ts`).
- **Лента:** авторам — опубликованные задания (`GET /api/tasks`, отбор `category`, `city` — «Вся Россия» подходит
  любому, `format`), бизнесу — анкеты (`GET /api/cards`, после входа; `topic`, `city`); курсор `(published_at, id)`.
  Страницы: `GET /api/tasks/{id}`, `GET /api/cards/{id}` (только опубликованное). Фронт: `/app` — сетка карточек
  (`TaskTile`, `CreatorTile`; наведение — «полотно» с главным), догрузка `/app/more` (готовая разметка),
  `/app/task/{id}`, `/app/creator/{id}` — страница целиком, «Откликнуться» / «Предложить задание» — заглушка.
- **Редактор:** `layouts/Editor.astro` без навигации приложения: панель `EditorBar` (✕, «Правка / Как увидят»,
  сохранение, «Опубликовать», «Осталось: …»), колонка до 720px; блоки — `scripts/editor/blocks.ts`.

### Черновики форм, только просмотры, срок публикации (задача 10)

- **Черновик формы** (`/api/me/drafts/{task-new|task-<id>|card}`): состояние формы как есть — поля, блоки, порядок,
  выбранное, фото-ссылки. `service.Drafts` за интерфейсом `DraftStore`: `repository.DraftsRedis` (ключ
  `ohvat:draft:<user>:<form>`, TTL 7 дней) → при сбое и без `REDIS_URL` — `repository.DraftsPG` (`form_drafts`,
  очистка раз в час); `service.FallbackDrafts`. Предел — `DRAFT_MAX_KB` (64), частота — `RATE_LIMIT_DRAFT_*`.
  Фронт (`scripts/editor/drafts.ts`): копия в localStorage сразу, на сервер — через 800 мс; при открытии — самое
  свежее; нет связи — копия ждёт и уходит при возврате связи; после отправки черновик удаляется.
- **Условие** — только просмотры (`min_likes` удалён). **Срок** обязателен: дата (+ время по желанию, по Москве),
  от завтра до 90 дней; `deadline_at` (UTC) считает сервис. Лента и страница — только `deadline_at > now()`;
  `Tasks.RunExpiry` раз в 5 минут переводит просроченные в `expired` (журнал, «срок публикации прошёл»).

### Мультивыбор, свои форматы и срок, просмотр фото, самопинг (задача 11)

- **Сферы задания** — до 5 (своих — до 3): `tasks.category` → `categories text[]` (миграция 00010, GIN-индекс по
  опубликованным); проверка — `checkSlugs` + `checkUsable` (общая с анкетой). **Форматы** — до 3: slug из списка или
  своё название (`normFormat`: название из списка → slug, известное из `domain.FormatHints` — его написание, своё —
  2–30 символов, буквы, без ссылок и мата, с заглавной; повторы без учёта регистра и ё отбрасываются). Подсказки —
  `GET /api/me/formats/similar` (`service.Formats` за `FormatRepository`: список, известные, свои из заданий бизнеса).
- **Лента** — отбор массивом: `category`, `format`, `topic` повторяются, подходит любое (`&&`); список отбора на
  фронте — с несколькими отметками, лента открывается при закрытии списка.
- **Срок** — пилюли «3 дня / Неделя / 2 недели / Месяц / + Свой»; «Свой» — свой календарь (завтра…90 дней) и время
  (`scripts/editor/due.ts`); контракт тот же (`publishBy`, `publishTime`).
- **Фото** на страницах задания и анкеты — `scripts/lightbox.ts`: `<dialog>` поверх страницы, счётчик, стрелки,
  ←/→/Home/End, свайп, миниатюры, Esc/✕/фон — закрыть, фокус — на открывшее фото.
- **Формы** — разделы `.grp` (заголовок 21px + подсказка 15px одной строкой; тексты — `content/app.ts FORM_GROUPS`),
  подписи 17px / 600, поля 54px / 17px — задание, анкета, профиль.
- **Самопинг** — `service.KeepAlive` (интерфейс `Prober`) + `gateway/keepalive` (GET `${KEEPALIVE_URL}/healthz`,
  таймаут 30 с): раз в 10 минут под errgroup; пусто — выключен; в журнал — только ошибки. На Render `KEEPALIVE_URL` =
  `PUBLIC_URL`; workflow GitHub `keepalive.yml` — запасной.

### Подтверждение соцсетей автора (задача 12)

- **Только официальные API, цифры — не со слов автора.** `service.Social` за интерфейсом `PlatformVerifier`
  (`Start` → адрес разрешения / ссылка на бота, `Check` → владелец и цифры, `Refresh` → свежие цифры) и
  `TokenKeeper` (`Renew` по правилам площадки, `Revoke` при отвязке). Реализации — `gateway/platforms/<площадка>`:
  `telegram` (Bot API: `getChat`, `getChatAdministrators`, `getChatMemberCount`), `youtube` (Google OAuth + PKCE,
  только `youtube.readonly`, YouTube Data API `channels.list`). VK, TikTok, Instagram, Threads — интерфейс готов,
  адаптеров нет: в анкете «Скоро» (`available: false`).
- **Telegram:** «Подключить» → `t.me/<бот>?start=ch_<токен>` (30 мин, одноразовый, хеш в `social_oauth_states`) →
  в боте «Привязать / Это не я» (защита от чужой ссылки) → Telegram привязывается к аккаунту как способ входа →
  бота добавляют в канал или группу (`my_chat_member`): добавивший — создатель или админ, подписчики канала или
  участники группы. Каналы, куда бота добавили до привязки, — `telegram_channels`, подтверждаются после привязки
  сами. Бота убрали — отметка снимается (`failed` с причиной). Telegram не ответил — `pending`, повтор через 5 мин.
- **OAuth (YouTube):** state (32 байта, 10 мин, одноразовый, связан с пользователем) + httpOnly-cookie
  `ohvat_social` на `/api/social`; PKCE verifier — зашифрован; возврат `GET /api/social/{p}/callback` → анкета
  `?social=&result=`. Токены — один шифротекст AES-256-GCM (`internal/secretbox`, ключ `TOKEN_ENC_KEY`, AAD —
  пользователь+площадка+subject; смена ключа — `TOKEN_ENC_KEY_OLD`). Без ключа OAuth-площадки выключены.
- **Безопасность исходящих запросов:** `gateway/platforms/httpx` — только https и только хосты площадки (белый
  список в транспорте), редиректы не выполняются, ответ до 1 МБ, таймауты, честный User-Agent с адресом сайта;
  ошибки без адреса и тела (токены не попадают в журнал). Страницы профилей не загружаются вовсе.
- **Обновление:** фоновая задача раз в 10 мин забирает аккаунты со сроком (`FOR UPDATE SKIP LOCKED` + аренда —
  несколько серверов не дублируют), пауза 1 с между запросами к одной площадке; следующее — через
  `SOCIAL_REFRESH_HOURS` ±1/12 (разброс); временный сбой — 15 мин × 2ⁿ до 6 ч; доступ отозван — `failed`.
  Токен обновляется под арендой строки доступа (`lock_until`). История цифр — `social_stats_history`.
- **Анкета:** `Cards` берёт соцсети у `PlatformSource` (= `Social.CardPlatforms`, только `verified`); ручного ввода
  площадок и подписчиков нет (колонка `creator_cards.platforms` больше не пишется); к публикации — хотя бы одна
  подтверждённая соцсеть. Лимиты: 20 подключений в час, 20 аккаунтов, «Проверить снова» — раз в 10 мин.
- **Таблицы** (миграция 00012): `social_credentials`, `social_accounts` (статус `unverified|pending|verified|failed`,
  метод `bot|oauth`, вид `profile|channel|group`), `social_stats_history`, `social_oauth_states`, `telegram_channels`.

### Контракт

`backend/api/openapi.yaml`: `GET /api/home`, `GET /healthz`; вход — `GET /api/auth/config`, `POST /api/auth/email/start`,
`POST /api/auth/email/verify`, `POST /api/auth/telegram`, `GET /api/auth/google/start`, `GET /api/auth/google/callback`,
`POST /api/auth/logout`; приложение — `GET /api/me`, `PATCH /api/me/mode`, `PUT /api/me/profile` (общие данные),
`GET/PUT /api/me/profile/business`, `GET/PUT /api/me/profile/creator`, `POST /api/me/logout-all`; задачи 08 —
`GET /api/tasks` (лента), `GET/POST /api/me/tasks`, `GET/PUT/DELETE /api/me/tasks/{id}`, `POST …/submit`, `POST …/withdraw`,
`GET/PUT /api/me/card`, `POST /api/me/card/submit|withdraw`, `GET/POST /api/me/categories`, `GET /api/me/categories/similar`,
`POST /api/me/media`, `DELETE /api/me/media/{id}`, `PUT /api/me/avatar`, `GET /media/{id}/{size}.webp`; задача 09 —
`GET /api/tasks/{id}`, `GET /api/cards`, `GET /api/cards/{id}`, отбор ленты; задача 11 — `GET /api/me/formats/similar`,
сферы и форматы массивами (0.8.0); задача 12 — `GET /api/me/social`, `POST /api/me/social/{platform}/start`,
`GET /api/social/{platform}/callback`, `POST /api/me/social/accounts/{id}/refresh`, `DELETE /api/me/social/accounts/{id}`,
`Platform` в анкете — подтверждённые соцсети (0.9.0).
Типы фронта генерируются из контракта (`npm run gen:api` → `frontend/src/lib/api.gen.ts`).

## Фронтенд (Astro 7 SSR + Tailwind 4 + TypeScript)

- Главная рендерится на сервере при запросе: данные из API с кэшем 30 с; если API временно недоступно — последние
  полученные данные, если их нет — честный 503.
- Токены макета (`project/design/README.md`) — `@theme` в `src/styles/global.css` (Tailwind 4). Компоненты перенесены
  из исходника макета `project/design/src/home.js` один к одному, поэтому 1440 совпадает с макетом попиксельно
  (проверка — разница 0,014% пикселей, только сглаживание текста).
- Главная v2 (задача 02) — порядок: первый экран → «Зачем это» → зона «Для бизнеса» → зона «Для авторов» → темы, вопросы,
  призыв. Компоненты: `Header` (сворачивается в «пилюлю»), `ZoneNav` (переключатель зон с прогрессом), `Hero`, `Why`
  (мысль по словам, схема с линией, тезисы), `ZoneHead`, `How` (4 шага бизнеса), `Promo` (инструменты продвижения с
  живыми превью «Топ ленты» и «Своё оформление»), `AutHow` (шаги автора), `AutFeatures` (возможности + превью каталога),
  `Plans`, `Topics`, `Faq`, `Footer` (призыв + подвал), `Head`, `Logo`/`Mark`/`BigMark`, `Icon` (Lucide из `src/icons`).
  Новые блоки v2 собраны по системе макета (токены, карточки, пилюли) — в макете `design/` их нет.
  Тексты — `src/content/home.ts`; меняющиеся данные — из API.
- Адаптив, мобильный первым: база — телефон, затем 640 / 768 / 1024 / 1200 (= макет). Мобильного макета нет —
  собран по системе макета: те же токены, карточки, пилюли; сетки складываются в 2 и 1 колонку, темы — пилюлями
  в строку, фото первого экрана 2 × 2 после текста и кнопок, меню — «бургер» со стеклом как у шапки.
- Анимации — только CSS, IntersectionObserver и один обработчик прокрутки на requestAnimationFrame (`src/scripts/site.ts`),
  без библиотек; расширение тёмных листов — `animation-timeline: view()` внутри `@supports`; при
  `prefers-reduced-motion` выключены и всё видно сразу; без JS — тоже всё видно.
- Вход (задачи 05–07): `/login` — без выбора роли; Telegram-бот, Google, почта → код (6 ячеек, автопереход, таймер,
  429 с отсчётом) → сразу `/app`. `src/middleware.ts` узнаёт пользователя по cookie (`GET /api/me`, адрес посетителя
  пробрасывается в `X-Real-IP`). Скрипт — `src/scripts/auth.ts`, общий клиент API — `src/lib/client.ts`.
- Приложение (задача 07): `src/layouts/App.astro` — шапка (знак, режим, поиск-заглушка, аватар-меню), центр, колонка
  «полезное», нижняя навигация на телефоне; `/app` (лента), `/app/profile` (общие данные + вкладки «Бизнес» / «Автор»),
  `/app/settings` (способы входа, уведомления, выход со всех устройств); `/account` → 301 на `/app`.
  Скрипт — `src/scripts/app.ts`, стили — `src/styles/app.css`, токены — `project/design/tokens.md`.
- Будущие страницы (`/tasks`, `/creators`, `/tasks/new`, `/creators/new`, `/pricing`, `/categories`, `/faq`,
  `/docs/*`) — заглушка «Скоро» (`src/pages/[...path].astro`, список — `src/lib/routes.ts`); прочие адреса — 404.
- Фото — WebP двух размеров (`public/img`), шрифт Inter 4 файлами (`public/fonts`, OFL), иконки Lucide (ISC).
- SEO: title, description, один H1, canonical, Open Graph (`public/og.png`), favicon, `robots.txt`, `sitemap.xml`.

## Тесты

| Уровень | Где | Что |
|---|---|---|
| модульные | `internal/service/*_test.go` (фейки в `fakes_test.go`), `internal/transport/http/*_test.go`, `internal/config`, `internal/gateway/*` | табличные: коды, лимиты, попытки, подпись Telegram, Google/PKCE, связывание, гонка двойного клика, анкета, очистка; CSRF, cookie, 429, карта ошибок; настройки; очередь писем (повторы, остановка), Resend, Google |
| интеграционные | `internal/repository/*_test.go` | настоящая PostgreSQL: миграции up → down → up, транзакции, уникальность, атомарные попытки (10 параллельных → 5), сессии |
| e2e | `frontend/tests/e2e/{home,auth,telegram,app}.spec.ts` | 375 / 768 / 1440; вход по почте и через бота → `/app`, режимы, профили, меню, настройки, выход и выход везде, адаптив |

`go test -race`; покрытие `service` + `repository` + `transport/http` — 94%.

## Развитие

- Новый модуль (задания, авторы, вход, отклики, оплата) — новый пакет в `internal/<модуль>` по тем же трём слоям,
  новые миграции, новые пути в `openapi.yaml`; существующее не переписывается.
- Оплата — `internal/payments` с общим интерфейсом и адаптером ЮKassa / Т-Банк (безопасная сделка), по правилам студии.
- Файлы (аватары, обложки) — S3-совместимое хранилище (локально MinIO), когда появятся.
- Redis — для нескольких серверов: лимиты уже готовы (`REDIS_URL`), очередь писем и бота — пока в памяти процесса; сессии и коды — в базе.
- Правка тем, тарифов и вопросов без разработчика — раздел админки поверх тех же таблиц.

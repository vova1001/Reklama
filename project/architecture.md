# Архитектура «Охват»

Этап: главная, регистрация и вход (задачи `handoff/01`–`05`).

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
| транспорт | `internal/transport/http` | маршруты chi, обработчики по `api/openapi.yaml` (только разбор запроса → вызов сервиса → ответ), DTO, единый формат ошибки `{error:{code,message,fields?,retryAfter?,attemptsLeft?}}`, middleware: request_id, журнал, паника, CSRF, токен-бакет по адресу (429 + Retry-After), сессия, `no-store` |
| сервис | `internal/service` | бизнес-логика без HTTP и SQL: главная; вход — коды (HMAC, срок, попытки, лимиты почта/адрес), подпись Telegram, Google + PKCE, связывание аккаунтов по подтверждённой почте, сессии (SHA-256), анкета, фоновая очистка |
| репозиторий | `internal/repository` (+ `internal/store` — код sqlc) | PostgreSQL, транзакции, ошибки pgx → `domain.ErrNotFound/ErrConflict`; SQL — только в `queries/*.sql` |
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
| `users` | role (`business`/`creator`/NULL), name, email (подтверждённая, NULL), avatar_url, city, spheres[], channels[], onboarded_at, last_login_at | уник. email, где не NULL |
| `identities` | user_id → users (каскад), provider (`email`/`telegram`/`google`), subject, email | уник. (provider, subject); user_id |
| `login_codes` | email, code_hash (HMAC), role, ip, attempts, expires_at, consumed_at; строки живут сутки — по ним лимиты | (email, created_at), (ip, created_at), created_at |
| `sessions` | user_id → users (каскад), token_hash (SHA-256, уник.), expires_at | user_id, expires_at |

Деньги — целые копейки; время — `timestamptz` (UTC). Объём данных главной — десятки строк, нагрузка минимальна.

### Контракт

`backend/api/openapi.yaml`: `GET /api/home`, `GET /healthz`; вход — `GET /api/auth/config`, `POST /api/auth/email/start`,
`POST /api/auth/email/verify`, `POST /api/auth/telegram`, `GET /api/auth/google/start`, `GET /api/auth/google/callback`,
`POST /api/auth/logout`; кабинет — `GET /api/me`, `PUT /api/me/profile`.
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
- Вход (задача 05): `/login` — роль, согласие, Google, Telegram (Telegram Login в окне, своя кнопка), почта → код
  (6 ячеек, вставка, автопереход, таймер «ещё раз», 429 с отсчётом) → анкета (`/login` для вошедшего без анкеты);
  `/account` — профиль и «Выйти». `src/middleware.ts` узнаёт пользователя по cookie (`GET /api/me`), шапка показывает
  аватар. Скрипт — `src/scripts/auth.ts` (CSRF-токен из `/api/auth/config`), стили — `src/styles/auth.css`.
- Будущие страницы (`/tasks`, `/creators`, `/tasks/new`, `/creators/new`, `/pricing`, `/categories`, `/faq`,
  `/docs/*`) — заглушка «Скоро» (`src/pages/[...path].astro`, список — `src/lib/routes.ts`); прочие адреса — 404.
- Фото — WebP двух размеров (`public/img`), шрифт Inter 4 файлами (`public/fonts`, OFL), иконки Lucide (ISC).
- SEO: title, description, один H1, canonical, Open Graph (`public/og.png`), favicon, `robots.txt`, `sitemap.xml`.

## Тесты

| Уровень | Где | Что |
|---|---|---|
| модульные | `internal/service/*_test.go` (фейки в `fakes_test.go`), `internal/transport/http/*_test.go`, `internal/config`, `internal/gateway/*` | табличные: коды, лимиты, попытки, подпись Telegram, Google/PKCE, связывание, гонка двойного клика, анкета, очистка; CSRF, cookie, 429, карта ошибок; настройки; очередь писем (повторы, остановка), Resend, Google |
| интеграционные | `internal/repository/*_test.go` | настоящая PostgreSQL: миграции up → down → up, транзакции, уникальность, атомарные попытки (10 параллельных → 5), сессии |
| e2e | `frontend/tests/e2e/{home,auth}.spec.ts` | 375 / 768 / 1440; вход по почте с кодом из журнала, ошибки, анкета, кабинет, выход, лимит 429 |

`go test -race`; покрытие `service` + `repository` + `transport/http` — 94%.

## Развитие

- Новый модуль (задания, авторы, вход, отклики, оплата) — новый пакет в `internal/<модуль>` по тем же трём слоям,
  новые миграции, новые пути в `openapi.yaml`; существующее не переписывается.
- Оплата — `internal/payments` с общим интерфейсом и адаптером ЮKassa / Т-Банк (безопасная сделка), по правилам студии.
- Файлы (аватары, обложки) — S3-совместимое хранилище (локально MinIO), когда появятся.
- Redis — для нескольких серверов: лимиты частоты и очередь писем сейчас в памяти процесса (один сервер); сессии и коды — уже в базе.
- Правка тем, тарифов и вопросов без разработчика — раздел админки поверх тех же таблиц.

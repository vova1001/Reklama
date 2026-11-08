# Архитектура «Охват»

Этап: главная страница + каркас всего проекта (задача `handoff/01-tech-lead-home.md`).

## Схема

```
браузер → nginx (web, :8000) ─┬─ /            → frontend (Astro SSR, Node, :4321) ──→ GET /api/home
                              └─ /api, /healthz → backend (Go, :8080) → PostgreSQL 16 (db)
```

Всё в Docker Compose: `db`, `backend`, `frontend`, `web`; для тестов — профиль `test` (`db-test` в памяти, `backend-test`).
Один адрес для сайта и API — в будущем cookie входа без CORS.

## Бэкенд (Go 1.24, chi, slog, pgx, sqlc, goose)

Три слоя, зависимости только сверху вниз:

| Слой | Пакет | Что делает |
|---|---|---|
| транспорт | `internal/httpapi` | маршруты chi, обработчики по `api/openapi.yaml`, DTO, единый формат ошибки `{error:{code,message}}`, логи запросов, восстановление после паники, `/healthz` (сервер + база) |
| сервис | `internal/service` | бизнес-логика без HTTP и SQL: сборка главной, тарифы по аудиториям, цена текстом («3 000 ₽», копейки → рубли), подпись кнопки тарифа, скрытие вопросов без ответа |
| репозиторий | `internal/repository` (+ `internal/store` — код sqlc) | чтение из PostgreSQL, сборка сущностей `internal/domain`; SQL — только в `queries/*.sql` |

Сервис зависит от интерфейса `HomeRepository`, транспорт — от интерфейса `HomeService`: каждый слой тестируется отдельно.
Миграции встроены в бинарник и применяются при старте (`migrations/`, goose, up и down).

### База

| Таблица | Поля | Индекс |
|---|---|---|
| `categories` | slug (уник.), title, icon (Lucide), position, is_active | position, где is_active |
| `plans` | audience (`business`/`creator`), slug, name, subtitle, price_kopecks, period (`month`), recommended, position, is_active; уник. (audience, slug) | (audience, position), где is_active |
| `plan_features` | plan_id → plans (каскад), position, text | PK (plan_id, position) |
| `faq` | question, answer, position, is_published | position, где is_published |

Деньги — целые копейки; время — `timestamptz` (UTC). Объём данных главной — десятки строк, нагрузка минимальна.

### Контракт

`backend/api/openapi.yaml`: `GET /api/home` → `{categories, businessPlans, creatorPlans, faq}`, `GET /healthz`.
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
- Будущие страницы (`/tasks`, `/creators`, `/tasks/new`, `/creators/new`, `/pricing`, `/login`, `/categories`, `/faq`,
  `/docs/*`) — заглушка «Скоро» (`src/pages/[...path].astro`, список — `src/lib/routes.ts`); прочие адреса — 404.
- Фото — WebP двух размеров (`public/img`), шрифт Inter 4 файлами (`public/fonts`, OFL), иконки Lucide (ISC).
- SEO: title, description, один H1, canonical, Open Graph (`public/og.png`), favicon, `robots.txt`, `sitemap.xml`.

## Тесты

| Уровень | Где | Что |
|---|---|---|
| модульные | `internal/service/home_test.go`, `internal/httpapi/server_test.go` | цена и подписи, сборка главной, ошибки; ответы по контракту, 404/405/500, паника, healthz |
| интеграционные | `internal/repository/home_test.go` | настоящая PostgreSQL в Docker: миграции up → down → up, сид, фильтры и порядок, ошибки соединения |
| e2e | `frontend/tests/e2e/home.spec.ts` | 375 / 768 / 1440: главная открывается, блоки на месте, данные из API, нет ошибок консоли, вопросы раскрываются, ссылки живые, нет горизонтальной прокрутки, меню, шапка, reduced-motion |

Покрытие `service` + `repository` + `httpapi` — 98,5%.

## Развитие

- Новый модуль (задания, авторы, вход, отклики, оплата) — новый пакет в `internal/<модуль>` по тем же трём слоям,
  новые миграции, новые пути в `openapi.yaml`; существующее не переписывается.
- Оплата — `internal/payments` с общим интерфейсом и адаптером ЮKassa / Т-Банк (безопасная сделка), по правилам студии.
- Файлы (аватары, обложки) — S3-совместимое хранилище (локально MinIO), когда появятся.
- Redis — когда понадобятся сессии, лимиты частоты и очередь проверки роликов (просмотры, лайки).
- Правка тем, тарифов и вопросов без разработчика — раздел админки поверх тех же таблиц.

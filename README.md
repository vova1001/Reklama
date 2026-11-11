# Охват

Площадка рекламы у авторов видео с оплатой за результат. Сейчас готова главная страница и каркас всего проекта.

## Запуск

Нужен Docker с Compose.

```sh
docker compose up --build        # или: make dev (создаст .env со случайным паролем базы)
```

Сайт откроется на http://localhost:8000. API: http://localhost:8000/api/home, проверка живости: http://localhost:8000/healthz.

Если Docker Hub недоступен, задайте зеркало: `REGISTRY=mirror.gcr.io/library docker compose up --build`.
Если вы за корпоративным прокси с собственным сертификатом, положите `*.crt` в `backend/certs/` и `frontend/certs/`
(в git эти файлы не попадают).

## Команды

| Команда | Что делает |
|---|---|
| `make dev` | поднимает всё в Docker: база, API, сайт, nginx на :8000 |
| `make test` | тесты бэка в Docker: модульные и интеграционные на настоящей PostgreSQL, покрытие |
| `make e2e` | e2e на Playwright против поднятого сайта (375 / 768 / 1440) |
| `make screens` | скриншоты главной 375 / 768 / 1440 в `project/screens/` |
| `make lint` | `gofmt`, `go vet`, `astro check` |
| `make gen` | код `store` из SQL (sqlc) и типы фронта из `backend/api/openapi.yaml` |
| `make down` | остановить |

Для e2e и скриншотов на машине нужны Node 22 и `cd frontend && npm ci && npx playwright install chromium`.

## Устройство

```
backend/    Go 1.25: api/openapi.yaml (контракт), cmd/server, internal/{transport/{http,telegram},service,repository,ratelimit,gateway,store,domain,config,logx}, migrations/, queries/
frontend/   Astro + Tailwind 4: src/{components,content,lib,pages,scripts,styles}, public/{fonts,img}, tests/e2e
deploy/     nginx.conf — единый вход: / → сайт, /api и /healthz → API
project/    бриф, ТЗ, макет, задачи (handoff/), architecture.md, screens/
```

Подробно — в [`project/architecture.md`](project/architecture.md).

## Данные главной

Темы, тарифы и вопросы хранятся в PostgreSQL (таблицы `categories`, `plans`, `plan_features`, `faq`), начальные данные —
миграция `backend/migrations/00002_seed_home.sql`. Сайт берёт их из `GET /api/home` при каждом запросе (кэш 30 секунд).
Пока нет кабинета, правка — новой миграцией или SQL в базе.

## Настройки

Все — переменные окружения, образец — `.env.example` (без значений). Паролей и ключей в репозитории нет.
Лимиты запросов (`RATE_LIMIT_*`, `TELEGRAM_WEBHOOK_*`) считаются в памяти процесса; для нескольких серверов задайте
`REDIS_URL` — лимиты станут общими (при сбое Redis временно считаются в памяти, сайт работает).

Фото из приложения (аватар, обложки, примеры работ) по умолчанию хранятся в PostgreSQL (`MEDIA_STORE=postgres` —
подходит для хостинга без диска). Переход на S3-совместимое хранилище (Cloudflare R2, Яндекс Object Storage, MinIO) —
`MEDIA_STORE=s3` и `STORAGE_ENDPOINT`, `STORAGE_BUCKET`, `STORAGE_REGION`, `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY`,
без правки кода; уже загруженные файлы — перенести копией ключей `media/…` из таблицы `media_blobs` в корзину.
Лимиты фото и заданий — `MEDIA_*`, `RATE_LIMIT_UPLOAD_*`, `TASKS_MAX_PER_USER`, `MODERATION_SUBMITS_PER_DAY`.
Проверка заданий и анкет — `MODERATION_MODE`: `auto` (по умолчанию, сразу публикуются) или `manual` (ждут модератора).

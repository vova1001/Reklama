# 07. tech-lead: один аккаунт — два режима, оболочка приложения после входа

## Задача (менеджер, по словам заказчика)
1. **Без выбора роли при регистрации.** Один аккаунт может быть и бизнесом, и автором. Внутри — переключатель режима
   «Бизнес / Автор» (в меню профиля и в шапке приложения). Нужен режим — включил; профиль режима заполняется, когда впервые нужен
   (мягкая подсказка «Заполните профиль автора, чтобы откликаться» — не блокирует).
   - Бэкенд: убрать role из регистрации; users.active_mode (business|creator), отдельные business_profiles и creator_profiles (1:1, nullable);
     `PATCH /api/me/mode`, `GET/PUT /api/me/profile/business`, `GET/PUT /api/me/profile/creator`; миграция с переносом существующих ролей. Три слоя, интерфейсы, тесты.
2. **После входа — сразу полноэкранное приложение (как в goesti: стиль, не копия):**
   - верхняя панель: знак, переключатель режима, поиск (заглушка), **кружок-аватар** справа → меню: Профиль, Режим, Настройки входа, **Выйти**;
   - центр — **лента** (пока пустое состояние/демо-карточки заданий в режиме автора, «Создать задание» в режиме бизнеса — без реальных данных, помечено как пример);
   - справа — колонка с полезным: прогресс заполнения профиля, подсказки, быстрые действия, тариф, «как это работает» — коротко;
   - мобильная версия: нижняя навигация, правая колонка уходит вниз.
   - Маршрут `/app` (лента), `/app/profile` (профиль: общие данные + вкладки «Бизнес» и «Автор»: имя/название, город, сферы; ссылки на каналы), `/app/settings` (способы входа, уведомления, выход со всех устройств).
   - **Кнопка «Выйти»** видна в меню аватара и на страницах профиля/настроек. После входа и после подтверждения в боте — сразу на `/app`; `/account` → редирект на `/app`.
3. **Дизайн и цвета — красиво.** Пересобери палитру для приложения (и согласуй вход/главную): спокойная, современная, без «белое на белом»; изумруд как фирменный акцент, чёткая лестница поверхностей, тонкие рамки, мягкие тени, хороший контраст (≥ 4.5:1). Минимализм, крупная типографика Inter, пилюли. Задокументируй токены в `project/design/tokens.md`.
4. **Лимиты везде:** общий токен-бакет на весь `/api` (по адресу) + для `/api/me*` по пользователю; вебхук бота — по секрету и с лимитом. Через интерфейс Limiter, чтобы позже подключить Redis (`REDIS_URL`, пока не обязателен).
- Скриншоты не нужны. Тесты: go test -race, e2e (вход → /app, переключение режима, заполнение профилей, выход). Коммит в ветку, не пушить. Отчёт по слоям в этот файл.

## Отчёт (техлид)
- **Сделано.** Роль при регистрации убрана: один аккаунт — два режима. `users.active_mode` (business|creator),
  профили `business_profiles` / `creator_profiles` (1:1, строки нет, пока профиль не заполнен); миграция 00005
  переносит прежние роли и анкеты (откат возвращает роль). После входа (почта, бот, Google, виджет) — сразу `/app`;
  `/account` → 301 на `/app`. Приложение: шапка (знак, «Бизнес / Автор», поиск-заглушка, аватар → Профиль, Режим,
  Настройки входа, Выйти), лента (автор — 3 примера заданий с меткой «Пример»; бизнес — «Создать задание» + пример
  своего задания), справа — прогресс профиля, быстрые действия, подсказка, тариф, «как это работает»; на телефоне —
  нижняя навигация, колонка под лентой. `/app/profile` — общие данные + вкладки «Бизнес» / «Автор»; `/app/settings` —
  способы входа, уведомления, «Выйти» и «Выйти со всех устройств» (свой диалог). Подсказка «Заполните профиль автора,
  чтобы откликаться» — мягкая, не блокирует. Палитра — `project/design/tokens.md` (лестница bg → card → panel → sunken,
  изумруд-акцент, все тексты ≥ 4.5:1 — проверяет e2e).
- **Лимиты.** Интерфейс `Limiter` (`internal/ratelimit`): `Memory`, `Redis` (атомарный Lua, время Redis), `Fallback`
  (Redis упал — считаем в памяти). Весь `/api` — по адресу (10/с, запас 60), вход — строже (1/с, 10), `/api/me*` — по
  пользователю (5/с, 30), вебхук — после проверки секрета общий бакет (50/с, 200, 429 → Telegram повторит).
  `REDIS_URL` необязателен. SSR пробрасывает адрес посетителя (`X-Real-IP`), иначе все делили бы лимит сервера сайта.
- **Проверено.** `make test` (Docker, PostgreSQL + Redis, `-race`): зелёные, покрытие service + repository + transport +
  gateway/telegram + ratelimit — 94,7%; `go vet`, gofmt. e2e Playwright (`make e2e-up`, 375/768/1440): 82 пройдено,
  2 пропущены по замыслу — вход → `/app`, режим (шапка, стрелки, меню, сохраняется), профили бизнеса и автора (ошибки
  рядом с полем), прогресс, меню с клавиатуры, выход, выход везде, адаптив 320–1440 без прокрутки вбок, контраст.
  `astro check` — 0 ошибок. Скриншоты не делались.

### Изменения по слоям (backend)
```
migrations/00005_modes.sql   нов.  active_mode, business_profiles, creator_profiles, перенос ролей; down — обратно
queries/profile.sql нов., auth.sql/telegram.sql изм. (без role) → store/*.sql.go (sqlc)
domain/user.go       изм.  ModeBusiness/ModeCreator, User.ActiveMode, Basics, BusinessProfile, CreatorProfile, Profiles
repository/
  profile.go         нов.  режим, общие данные, профили (upsert), справочник тем
  auth.go, telegram.go изм. без роли; profile_test.go нов. (в т.ч. перенос данных миграцией и откат)
service/
  profile.go         нов.  Profiles: Overview (чек-лист, %), SwitchMode, SaveBasics, Business/Creator, SaveBusiness/SaveCreator;
                           интерфейсы ProfileRepository, ProviderLister
  auth*.go           изм.  без роли; auth_profile.go удалён (анкета → профили режимов); bot_texts.go — «Режим», ссылки на /app
transport/http/
  profile.go, profile_dto.go нов. GET /api/me, PATCH /api/me/mode, PUT /api/me/profile, GET/PUT /api/me/profile/{business,creator},
                           POST /api/me/logout-all; интерфейс ProfileService
  ratelimit.go       изм.  интерфейс Limiter, middleware limit (по адресу / по пользователю), 429 + Retry-After
  server.go, auth.go изм.  APILimiter/AuthLimiter/UserLimiter; next = /app; Google-cookie без роли (старая — принимается)
transport/telegram/webhook.go изм. лимит вебхука после секрета
ratelimit/           нов.  limiter.go (интерфейс), memory.go, redis.go, fallback.go, ratelimit_test.go
config/              изм.  RATE_LIMIT_API_*, RATE_LIMIT_USER_*, TELEGRAM_WEBHOOK_*, REDIS_URL
cmd/server/main.go   изм.  сборка Profiles и лимитов (Redis или память), очистка бакетов
api/openapi.yaml     0.4.0 Mode, Me (profiles, checklist, completion), профили, logout-all; role убрана
Dockerfile           изм.  стадия test (Debian + cgo) — make test с -race снова работает
```
- **Фронтенд:** `layouts/App.astro`; `components/app/{AppBar,ModeSwitch,Search,BottomNav,Side,Nudge}.astro`;
  `pages/app/{index,profile,settings}.astro`; `account.astro` → редирект; `login.astro` без роли и анкеты;
  `scripts/app.ts` (нов.), `scripts/auth.ts` и `lib/client.ts` (общий клиент API); `lib/auth.ts` (профили, адрес посетителя);
  `content/app.ts`; `styles/app.css` (нов.), `global.css` (токены), `auth.css` (без анкеты и кабинета); иконки Lucide.
  Тесты: `tests/e2e/app.spec.ts` (нов.), `auth.spec.ts`, `telegram.spec.ts` (изм.).
- **Окружение:** `.env.example`, `render.yaml`, `docker-compose*.yml` (Redis для тестов, шире лимиты в e2e), CI (Redis),
  `README.md`, `project/architecture.md`, `project/design/tokens.md` (нов.).

### Вопросы к заказчику
1. Режим нового аккаунта по умолчанию — «Бизнес». Так, или спрашивать при первом входе одним экраном?
2. Привязать Telegram/почту к уже открытому аккаунту (кнопкой в настройках) — сделать следующей задачей? Сейчас способы
   связываются только по одной подтверждённой почте; вход через Telegram без почты создаёт отдельный аккаунт.

### Сверх задачи (на одобрение)
1. `POST /api/me/logout-all` и проверка контраста всех текстов приложения в e2e.
2. Стадия `test` в `backend/Dockerfile`: прежний `make test` с `-race` падал (cgo выключен в alpine).
3. Наблюдение: локальный nginx (`deploy/nginx.conf`) запоминает адрес контейнера фронта — после пересборки фронта нужен
   `docker compose restart web`. Исправить (resolver Docker) — отдельной мелкой задачей.

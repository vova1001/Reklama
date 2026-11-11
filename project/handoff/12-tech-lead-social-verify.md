# 12. tech-lead: подтверждение соцсетей автора (Telegram, YouTube, VK, TikTok, Instagram, Threads)

## Задача (менеджер)
Автор подтверждает, что аккаунт/канал его, и мы получаем число подписчиков не со слов автора. В анкете и ленте — значок «Подтверждено» и дата обновления.

**Архитектура:** в service — интерфейс `PlatformVerifier` (Start → инструкция/ссылка, Check → владелец+подписчики, Refresh → статистика); реализации в `gateway/platforms/<площадка>`; таблицы `social_accounts` (площадка, handle/id, статус: unverified|pending|verified|failed, метод, подписчики, verified_at, refreshed_at) и `social_stats_history`; фоновая горутина обновляет статистику раз в сутки с лимитами API и jitter; цифры в анкете берутся только отсюда (ручной ввод подписчиков убрать для подтверждённых).

**Методы по площадкам:**
1. **Telegram (канал)** — автор добавляет @reklama_auth_bot в канал админом (без прав); бот по `my_chat_member` получает канал, через `getChatAdministrators` проверяет, что Telegram-аккаунт автора (из входа через Telegram или привязки) — создатель/админ; подписчики — `getChatMemberCount`. Полностью автоматически.
2. **YouTube** — отдельная кнопка «Подключить YouTube»: Google OAuth с доп. областью `youtube.readonly` (инкрементально, только при подключении), `channels.list?mine=true` → id, название, подписчики (если скрыты — пометка), просмотры. Токен обновления хранить зашифрованно (AES-GCM, ключ `TOKEN_ENC_KEY`), только для обновления статистики.
3. **VK** — «Подключить VK»: VK ID OAuth (`VK_CLIENT_ID/SECRET`), профиль → подписчики; для сообщества — `groups.get filter=admin` и `members_count`. Без ключей — кнопка скрыта, доступен метод кода (п.4).
4. **TikTok, Instagram, Threads (и запасной для всех)** — «код в описании профиля»: выдаём код `ohvat-XXXX` (живёт 24 ч), автор ставит его в bio, нажимает «Проверить» — сервер загружает публичную страницу профиля (таймаут, лимит, честный User-Agent сервиса), ищет код и число подписчиков. Если страницу получить нельзя (закрыто/нужен вход) — статус `pending`: автор прикладывает скрин статистики, проверка вручную позже (метод модератора за интерфейсом, без админки). Официальные API TikTok/Meta — интерфейсы готовы, реализации позже (нужно зарубежное юрлицо и проверка приложений).
- Фронт (стиль приложения, крупно и понятно): в анкете раздел «Соцсети» — список площадок, у каждой статус и одна кнопка действия; пошаговая инструкция в 2–3 строки; повторная проверка; отвязка.
- Лимиты на проверки, защита от SSRF при загрузке страниц (только разрешённые домены площадок), тесты на фейках, e2e на Telegram (подставной API) и код в описании (подставной сервер). Переменные — в render.yaml/.env.example.
- Скриншоты не нужны. Коммит, не пушить. Отчёт 3 строки + дерево по слоям + что нужно от заказчика (ключи VK, область YouTube в Google).

## Отчёт (техлид)
По решению заказчика (через менеджера): только официальные API, без загрузки страниц, «кода в описании», скринов и
ручного ввода подписчиков; сейчас рабочие — **Telegram (каналы и группы)** и **YouTube**; VK, TikTok, Instagram,
Threads — интерфейс `PlatformVerifier` готов, в анкете неактивная кнопка «Скоро».

- **Сделано.** В анкете раздел «Соцсети»: 6 площадок, у каждой статус и одна кнопка; аккаунты со значком
  «Подтверждено», видом (канал / группа / профиль), числом подписчиков (участников) и датой обновления, «Проверить
  снова», «Отвязать». Telegram: «Подключить» → 3 шага → бот «Привязать / Это не я» → бота добавляют в канал или группу
  админом (права можно снять) → `getChatAdministrators` (автор — создатель/админ) + `getChatMemberCount` → отметка
  появляется сама; бота убрали — отметка снимается. YouTube: Google OAuth + PKCE, только `youtube.readonly`
  (отдельно от входа), `channels.list mine=true`, скрытые подписчики — пометка. Цифры обновляются раз в сутки (±2 ч,
  аренда строк — без дублей на нескольких серверах, пауза между запросами к площадке, растущая пауза при сбоях),
  токен Google обновляется сам, при отвязке — отзывается. В анкете и ленте — только подтверждённое; без
  подтверждённой соцсети анкету не опубликовать.
- **Безопасность.** Токены — только AES-256-GCM (`TOKEN_ENC_KEY`, привязка шифротекста к пользователю и площадке,
  смена ключа — `TOKEN_ENC_KEY_OLD`); state OAuth одноразовый, 10 мин, связан с пользователем и httpOnly-cookie;
  привязка Telegram — с подтверждением в боте; исходящие запросы — только https и только хосты площадки (белый
  список в транспорте), без редиректов, ответ ≤1 МБ, таймауты, ошибки без адресов и токенов; страницы профилей не
  загружаются (SSRF исключён по устройству). Лимиты: 20 подключений в час, 20 аккаунтов, «Проверить снова» — раз в 10 мин.
- **Проверено.** `make test` (Docker, PostgreSQL + Redis, `-race`) — зелёные, покрытие 90,7%; gofmt, go vet,
  `astro check` — 0 ошибок. e2e (1440 / 768 / 375, подставной Bot API, реальные площадки не вызываются) — 115 пройдено,
  2 пропущены по замыслу; новые: канал и группа через бота, чужая ссылка / одноразовость / Telegram другого аккаунта,
  анкета без соцсети не публикуется → с ней видна бизнесу со значком. YouTube — Go-тесты на подставном Google.
  Скриншоты не делались.

### Изменения по слоям
```
backend/
  migrations/00012_social_accounts.sql  нов.  social_credentials, social_accounts, social_stats_history,
                                              social_oauth_states, telegram_channels (up/down)
  queries/social.sql нов. → store/social.sql.go (sqlc)
  domain/   social.go нов. (площадки, статусы, методы, SocialAccount/Stats/Credential/State, TelegramChannel);
            listing.go — Platform: вид, название, подтверждено, дата обновления
  service/  social_verifier.go нов. (PlatformVerifier, TokenKeeper, Tokens, ошибки площадок)
            social.go нов. (Social: Overview, Start, Callback, RefreshNow, Unlink, RunRefresher, CardPlatforms;
                            шифрование токенов, аренда доступа, разброс и паузы)
            social_telegram.go нов. (привязка Telegram по ссылке, события каналов/групп, pending)
            bot_social.go нов. (бот: /start ch_, «Привязать / Это не я», ответы о каналах); bot.go — BotSocial, Channel
            cards.go — соцсети из PlatformSource, без ручных площадок; listing_check.go — убран разбор ссылок площадок
  repository/social.go нов.; listing.go — площадки анкеты больше не пишутся
  secretbox/ нов.   AES-256-GCM, id ключа, AAD, старые ключи для расшифровки
  gateway/platforms/
    httpx/    нов.  белый список хостов, только https, без редиректов, лимит ответа, User-Agent, ошибки без секретов
    telegram/ нов.  PlatformVerifier: getChat, getChatAdministrators, getChatMemberCount (канал / группа)
    youtube/  нов.  PlatformVerifier + TokenKeeper: OAuth + PKCE, channels.list, обновление и отзыв токена
  gateway/telegram/client.go — GetChat, GetChatAdministrators, GetChatMemberCount
  transport/http/social.go нов. — GET /api/me/social, POST …/{platform}/start, GET /api/social/{platform}/callback,
            POST …/accounts/{id}/refresh, DELETE …/accounts/{id}; server.go, auth.go (ошибки), listing_dto.go
  transport/telegram/dto.go — my_chat_member каналов и групп → ChannelEvent
  config — TOKEN_ENC_KEY, TOKEN_ENC_KEY_OLD, YOUTUBE_CONNECT, SOCIAL_REFRESH_HOURS, SOCIAL_STARTS_PER_HOUR
  cmd/server — сборка Social, фоновое обновление; cmd/faketelegram — каналы/группы (POST /__chats)
  api/openapi.yaml 0.9.0;  тесты: social_test, bot_social_test, secretbox, httpx, telegram, youtube, repository, transport
frontend/src/
  scripts/editor/social.ts нов. (раздел «Соцсети», ожидание подтверждения из бота, итог возврата с площадки);
  scripts/editor.ts (без ручных площадок, полнота — подтверждённая соцсеть, предпросмотр), editor/dom.ts (иконки)
  components/app/ CardForm, CreatorPage, CreatorTile, TaskForm; lib/format.ts; content/app.ts; styles/app.css;
  icons: youtube, instagram, at-sign, refresh-cw; lib/api.gen.ts
  tests/e2e/ social.ts, social.spec.ts нов.; listings.spec.ts (анкета — через подтверждение Telegram)
окружение: render.yaml, .env.example, docker-compose*.yml, CI (покрытие новых пакетов); README,
  project/architecture.md, project/design/tokens.md
```

### Что нужно от заказчика
1. **YouTube (Google Cloud, тот же проект, что вход через Google):** включить YouTube Data API v3; в OAuth client
   добавить Authorized redirect URI `${PUBLIC_URL}/api/social/youtube/callback`; на экране согласия добавить область
   `https://www.googleapis.com/auth/youtube.readonly` и пройти проверку приложения Google (область «чувствительная»;
   до проверки — до 100 тестовых пользователей, их почты внести в Test users). В Render — `TOKEN_ENC_KEY`
   (создаётся сам из render.yaml; при переезде перенести тот же ключ).
2. **Telegram:** ничего нового — тот же бот @reklama_auth_bot.
3. **На потом (кнопки «Скоро»):** VK — приложение VK ID (`VK_CLIENT_ID`, redirect `${PUBLIC_URL}/api/social/vk/callback`,
   доступ `groups`); TikTok — Login Kit (`user.info.basic`, `user.info.stats`, проверка приложения); Instagram и
   Threads — приложение Meta (Instagram API with Instagram Login — `instagram_business_basic`, только
   профессиональные аккаунты; Threads API — `threads_basic`, `threads_manage_insights`), проверка приложения Meta;
   для TikTok и Meta обычно нужно зарубежное юрлицо.

### Вопросы к заказчику
1. Один и тот же канал могут подтвердить несколько авторов, если все они — админы канала (соавторы). Так подходит
   или канал должен принадлежать только одному (тогда — только создатель)?
2. Анкеты, опубликованные до этой задачи с площадками «со слов», остались опубликованными, но цифр в них теперь нет,
   пока автор не подтвердит соцсеть. Снять такие анкеты в черновик или оставить?

### Сверх задачи (на одобрение)
1. Привязка Telegram из анкеты становится и способом входа в этот аккаунт (через Telegram входят в тот же аккаунт).
2. Каналы, куда бота добавили до привязки Telegram, подтверждаются сами после «Привязать».
3. История цифр (`social_stats_history`) — основа для графика роста и проверки накруток позже.

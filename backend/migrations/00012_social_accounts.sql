-- +goose Up
-- Подтверждённые соцсети автора. Цифры (подписчики, просмотры, видео, лайки) — только из официального API площадки
-- (Telegram — бот-админ канала; YouTube, VK, TikTok, Instagram, Threads — OAuth автора), не со слов автора.

-- Доступы OAuth: токены — одним шифротекстом AES-256-GCM (ключ TOKEN_ENC_KEY, связан с user_id, площадкой и
-- subject). Сроки — открыто, чтобы планировать обновление без расшифровки. lock_until — аренда на обновление
-- токена (площадки с одноразовым refresh-токеном: обновлять может только один процесс).
CREATE TABLE social_credentials (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id            bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    platform           text   NOT NULL CHECK (platform IN ('youtube', 'vk', 'tiktok', 'instagram', 'threads')),
    subject            text   NOT NULL,          -- id человека на площадке (open_id, user_id, sub)
    secret             bytea  NOT NULL,          -- зашифрованные токены
    access_expires_at  timestamptz,
    refresh_expires_at timestamptz,
    lock_until         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, platform, subject)
);

-- Аккаунты и каналы автора. Статус: unverified — не проверен; pending — ждёт проверки (Telegram: бот добавлен,
-- проверка админов не удалась временно); verified — подтверждён, цифры от площадки; failed — доступ потерян
-- (токен отозван, бот удалён из канала или группы) — нужна повторная проверка.
CREATE TABLE social_accounts (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id          bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    platform         text   NOT NULL CHECK (platform IN ('telegram', 'youtube', 'vk', 'tiktok', 'instagram', 'threads')),
    external_id      text   NOT NULL,            -- id канала / профиля / сообщества на площадке
    kind             text   NOT NULL CHECK (kind IN ('profile', 'channel', 'group')),
    handle           text   NOT NULL DEFAULT '', -- @имя
    title            text   NOT NULL DEFAULT '',
    url              text   NOT NULL DEFAULT '',
    status           text   NOT NULL DEFAULT 'unverified' CHECK (status IN ('unverified', 'pending', 'verified', 'failed')),
    method           text   NOT NULL CHECK (method IN ('bot', 'oauth')),
    credential_id    bigint REFERENCES social_credentials (id) ON DELETE SET NULL,
    followers        bigint,                     -- NULL — площадка не отдала (скрыто)
    followers_hidden boolean NOT NULL DEFAULT false,
    views            bigint,
    videos           bigint,
    likes            bigint,
    fail_reason      text   NOT NULL DEFAULT '',
    failures         int    NOT NULL DEFAULT 0,  -- временных сбоев подряд (пауза растёт)
    verified_at      timestamptz,
    refreshed_at     timestamptz,
    next_refresh_at  timestamptz,                -- NULL — не обновлять (failed)
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, platform, external_id)
);
CREATE INDEX social_accounts_due_idx ON social_accounts (next_refresh_at) WHERE next_refresh_at IS NOT NULL;
CREATE INDEX social_accounts_ext_idx ON social_accounts (platform, external_id);
CREATE INDEX social_accounts_cred_idx ON social_accounts (credential_id) WHERE credential_id IS NOT NULL;

-- История цифр (одна строка на обновление): рост аудитории, проверка накруток позже.
CREATE TABLE social_stats_history (
    account_id bigint NOT NULL REFERENCES social_accounts (id) ON DELETE CASCADE,
    at         timestamptz NOT NULL,
    followers  bigint,
    views      bigint,
    videos     bigint,
    likes      bigint,
    PRIMARY KEY (account_id, at)
);

-- Начатые подключения: state OAuth (только SHA-256) → пользователь и PKCE verifier (зашифрован); для Telegram —
-- токен ссылки на бота (t.me/<бот>?start=ch_<токен>). Одноразовые, живут 10 минут (Telegram — 30).
CREATE TABLE social_oauth_states (
    state_hash bytea PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    platform   text   NOT NULL,
    verifier   bytea,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX social_oauth_states_exp_idx ON social_oauth_states (expires_at);
CREATE INDEX social_oauth_states_user_idx ON social_oauth_states (user_id, created_at DESC);

-- Каналы и группы, куда добавили бота (по my_chat_member): кто добавил и в каком статусе бот. Нужны, если канал
-- добавили раньше, чем привязали Telegram к аккаунту: после привязки канал подтверждается сам.
CREATE TABLE telegram_channels (
    chat_id    bigint PRIMARY KEY,
    title      text   NOT NULL DEFAULT '',
    username   text   NOT NULL DEFAULT '',
    chat_type  text   NOT NULL DEFAULT 'channel', -- channel | group | supergroup
    added_by   bigint NOT NULL,                  -- tg id того, кто добавил бота
    bot_status text   NOT NULL,                  -- administrator | member | left | kicked
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX telegram_channels_added_by_idx ON telegram_channels (added_by);

-- +goose Down
DROP TABLE telegram_channels;
DROP TABLE social_oauth_states;
DROP TABLE social_stats_history;
DROP TABLE social_accounts;
DROP TABLE social_credentials;

-- +goose Up
-- Вход через бота: сайт создаёт одноразовый токен (в ссылке t.me/<бот>?start=login_<токен>), бот подтверждает,
-- браузер забирает вход. В базе только SHA-256 токена и секрета браузера (httpOnly-cookie): зная ссылку, чужой
-- браузер вход не заберёт. Строки живут сутки — по ним считается лимит запусков с адреса.
CREATE TABLE telegram_logins (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token_hash   bytea  NOT NULL UNIQUE,
    browser_hash bytea  NOT NULL UNIQUE,
    role         text CHECK (role IN ('business', 'creator')),
    ip           text   NOT NULL DEFAULT '',
    tg_user_id   bigint,                     -- кто подтвердил
    tg_name      text   NOT NULL DEFAULT '',
    tg_username  text   NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    confirmed_at timestamptz,
    consumed_at  timestamptz,                -- вход забран браузером или отменён
    cancelled    boolean NOT NULL DEFAULT false
);
CREATE INDEX telegram_logins_ip_idx ON telegram_logins (ip, created_at DESC);
CREATE INDEX telegram_logins_created_idx ON telegram_logins (created_at);

-- Чаты бота: кто писал боту (личный чат) и хочет ли уведомления. Пользователь сайта связан через
-- identities (provider = 'telegram', subject = tg_user_id).
CREATE TABLE telegram_chats (
    tg_user_id  bigint PRIMARY KEY,
    chat_id     bigint  NOT NULL,
    username    text    NOT NULL DEFAULT '',
    notify      boolean NOT NULL DEFAULT true,
    blocked_at  timestamptz,                 -- пользователь остановил бота — не пишем
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Устройства, с которых входил пользователь (SHA-256 id из httpOnly-cookie): вход с нового — уведомление.
CREATE TABLE user_devices (
    user_id       bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    device_hash   bytea  NOT NULL,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, device_hash)
);

-- +goose Down
DROP TABLE user_devices;
DROP TABLE telegram_chats;
DROP TABLE telegram_logins;

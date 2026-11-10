-- +goose Up
-- Пользователи площадки: бизнес или автор. Роль пустая, пока человек не выбрал её (вход через Google/Telegram
-- без выбора) — тогда её спрашивает анкета первого входа. email — подтверждённая почта (нижний регистр) или NULL.
CREATE TABLE users (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    role          text CHECK (role IN ('business', 'creator')),
    name          text   NOT NULL DEFAULT '',
    email         text,
    avatar_url    text   NOT NULL DEFAULT '',
    city          text   NOT NULL DEFAULT '',
    spheres       text[] NOT NULL DEFAULT '{}', -- бизнес: slug тем (categories.slug)
    channels      text[] NOT NULL DEFAULT '{}', -- автор: ссылки на каналы
    onboarded_at  timestamptz,                  -- анкета заполнена
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz
);
CREATE UNIQUE INDEX users_email_uniq ON users (email) WHERE email IS NOT NULL;

-- Способы входа: один пользователь — несколько способов. subject: почта / telegram id / Google sub.
CREATE TABLE identities (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider     text   NOT NULL CHECK (provider IN ('email', 'telegram', 'google')),
    subject      text   NOT NULL,
    email        text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, subject)
);
CREATE INDEX identities_user_idx ON identities (user_id);

-- Одноразовые коды из письма: хранится только HMAC кода. Строки живут сутки — по ним считаются лимиты отправки.
CREATE TABLE login_codes (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email       text     NOT NULL,
    code_hash   bytea    NOT NULL,
    role        text CHECK (role IN ('business', 'creator')),
    ip          text     NOT NULL DEFAULT '',
    attempts    smallint NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz
);
CREATE INDEX login_codes_email_idx ON login_codes (email, created_at DESC);
CREATE INDEX login_codes_ip_idx ON login_codes (ip, created_at DESC);
CREATE INDEX login_codes_created_idx ON login_codes (created_at);

-- Сессии: в базе только SHA-256 токена из cookie.
CREATE TABLE sessions (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   bytea  NOT NULL UNIQUE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;
DROP TABLE login_codes;
DROP TABLE identities;
DROP TABLE users;

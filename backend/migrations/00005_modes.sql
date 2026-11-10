-- +goose Up
-- Один аккаунт — два режима. Роль при регистрации больше не выбирается: у пользователя есть активный режим
-- (business | creator), а профили режимов — отдельные таблицы 1:1, строка появляется, когда профиль впервые
-- сохранён. Общие данные (как называть, город, почта, аватар) остаются в users.
ALTER TABLE users
    ADD COLUMN active_mode text NOT NULL DEFAULT 'business' CHECK (active_mode IN ('business', 'creator'));

CREATE TABLE business_profiles (
    user_id    bigint PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    name       text   NOT NULL,                -- название компании или бренда
    city       text   NOT NULL,
    spheres    text[] NOT NULL DEFAULT '{}',   -- slug тем (categories.slug)
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE creator_profiles (
    user_id    bigint PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    name       text   NOT NULL,                -- имя или псевдоним автора
    city       text   NOT NULL,
    spheres    text[] NOT NULL DEFAULT '{}',   -- темы, о которых пишет автор
    channels   text[] NOT NULL DEFAULT '{}',   -- ссылки на каналы (https://… или @имя)
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Перенос: заполненная анкета прежней роли становится профилем соответствующего режима, роль — активным режимом.
INSERT INTO business_profiles (user_id, name, city, spheres, created_at, updated_at)
SELECT id, name, city, spheres, COALESCE(onboarded_at, now()), updated_at
FROM users WHERE role = 'business' AND onboarded_at IS NOT NULL;

INSERT INTO creator_profiles (user_id, name, city, channels, created_at, updated_at)
SELECT id, name, city, channels, COALESCE(onboarded_at, now()), updated_at
FROM users WHERE role = 'creator' AND onboarded_at IS NOT NULL;

UPDATE users SET active_mode = role WHERE role IS NOT NULL;

ALTER TABLE users DROP COLUMN role, DROP COLUMN spheres, DROP COLUMN channels;
ALTER TABLE login_codes DROP COLUMN role;
ALTER TABLE telegram_logins DROP COLUMN role;

-- +goose Down
ALTER TABLE telegram_logins ADD COLUMN role text CHECK (role IN ('business', 'creator'));
ALTER TABLE login_codes ADD COLUMN role text CHECK (role IN ('business', 'creator'));
ALTER TABLE users
    ADD COLUMN role     text CHECK (role IN ('business', 'creator')),
    ADD COLUMN spheres  text[] NOT NULL DEFAULT '{}',
    ADD COLUMN channels text[] NOT NULL DEFAULT '{}';

-- Обратно — одна роль: профиль активного режима, а если его нет — тот, что есть.
UPDATE users u SET role = 'creator', channels = c.channels
FROM creator_profiles c WHERE c.user_id = u.id;

UPDATE users u SET role = 'business', spheres = b.spheres, channels = '{}'
FROM business_profiles b WHERE b.user_id = u.id AND (u.role IS NULL OR u.active_mode = 'business');

DROP TABLE creator_profiles;
DROP TABLE business_profiles;
ALTER TABLE users DROP COLUMN active_mode;

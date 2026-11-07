-- +goose Up
-- Темы заданий (категории). position — порядок показа.
CREATE TABLE categories (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug       text    NOT NULL UNIQUE,
    title      text    NOT NULL,
    icon       text    NOT NULL,
    position   integer NOT NULL DEFAULT 0,
    is_active  boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX categories_active_position_idx ON categories (position) WHERE is_active;

-- Тарифы: для бизнеса и для авторов. Цена — в копейках за период.
CREATE TABLE plans (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    audience      text    NOT NULL CHECK (audience IN ('business', 'creator')),
    slug          text    NOT NULL,
    name          text    NOT NULL,
    subtitle      text    NOT NULL DEFAULT '',
    price_kopecks bigint  NOT NULL CHECK (price_kopecks >= 0),
    period        text    NOT NULL DEFAULT 'month' CHECK (period IN ('month')),
    recommended   boolean NOT NULL DEFAULT false,
    position      integer NOT NULL DEFAULT 0,
    is_active     boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (audience, slug)
);
CREATE INDEX plans_audience_position_idx ON plans (audience, position) WHERE is_active;

-- Пункты тарифа по порядку.
CREATE TABLE plan_features (
    plan_id  bigint  NOT NULL REFERENCES plans (id) ON DELETE CASCADE,
    position integer NOT NULL,
    text     text    NOT NULL,
    PRIMARY KEY (plan_id, position)
);

-- Частые вопросы.
CREATE TABLE faq (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    question     text    NOT NULL,
    answer       text    NOT NULL,
    position     integer NOT NULL DEFAULT 0,
    is_published boolean NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX faq_published_position_idx ON faq (position) WHERE is_published;

-- +goose Down
DROP TABLE faq;
DROP TABLE plan_features;
DROP TABLE plans;
DROP TABLE categories;

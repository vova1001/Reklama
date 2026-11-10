-- +goose Up
-- Задания бизнеса, анкеты авторов, свои категории и фото — с модерацией:
-- черновик → на проверке → опубликовано | отклонено. Черновик хранит что угодно в пределах длин; полноту проверяет
-- сервис при отправке. Журнал moderation_log — каждая смена статуса: кто, откуда, куда, причина отказа.

-- ===== свои категории =====
-- Общий справочник — status = 'approved' (из него главная). Свою категорию создаёт пользователь: она «на проверке»,
-- видна ему в его задании или анкете, после одобрения становится общей. norm — название в нижнем регистре, ё → е,
-- одиночные пробелы: по нему нет дублей среди общих и среди своих у одного автора.
ALTER TABLE categories
    ADD COLUMN status    text NOT NULL DEFAULT 'approved' CHECK (status IN ('approved', 'pending_review', 'rejected')),
    ADD COLUMN author_id bigint REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN norm      text;
UPDATE categories SET norm = lower(replace(replace(title, 'Ё', 'Е'), 'ё', 'е'));
ALTER TABLE categories ALTER COLUMN norm SET NOT NULL;
CREATE UNIQUE INDEX categories_norm_common_uq ON categories (norm) WHERE status = 'approved';
CREATE UNIQUE INDEX categories_norm_author_uq ON categories (author_id, norm) WHERE status = 'pending_review';
CREATE INDEX categories_author_idx ON categories (author_id, created_at) WHERE author_id IS NOT NULL;

-- ===== фото =====
-- Метаданные; сами файлы — в хранилище за интерфейсом (media_blobs в PostgreSQL или S3). id — 128 случайных бит.
-- subject — к чему прикреплено (user — аватар профиля, task, creator_card); NULL — не прикреплено: через сутки
-- после touched_at файл удаляет фоновая очистка.
CREATE TABLE media (
    id         text   PRIMARY KEY,
    owner_id   bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    text   NOT NULL CHECK (purpose IN ('avatar', 'task_cover', 'task_photo', 'card_avatar', 'card_work')),
    width      integer NOT NULL,
    height     integer NOT NULL,
    bytes      bigint  NOT NULL,                       -- все размеры вместе — для квоты
    subject    text    CHECK (subject IN ('user', 'task', 'creator_card')),
    subject_id bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    touched_at timestamptz NOT NULL DEFAULT now()      -- загружено, прикреплено или откреплено
);
CREATE INDEX media_owner_idx ON media (owner_id, created_at);
CREATE INDEX media_orphans_idx ON media (touched_at) WHERE subject IS NULL;
CREATE INDEX media_subject_idx ON media (subject, subject_id) WHERE subject IS NOT NULL;

-- Хранилище файлов в PostgreSQL (MEDIA_STORE=postgres; на хостинге без диска).
CREATE TABLE media_blobs (
    key          text  PRIMARY KEY,
    content_type text  NOT NULL,
    data         bytea NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- ===== задания =====
CREATE TABLE tasks (
    id             bigserial PRIMARY KEY,
    owner_id       bigint  NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status         text    NOT NULL DEFAULT 'draft'
                   CHECK (status IN ('draft', 'pending_review', 'published', 'rejected')),
    reject_reason  text    NOT NULL DEFAULT '',
    title          text    NOT NULL DEFAULT '',
    city           text    NOT NULL DEFAULT '',
    nationwide     boolean NOT NULL DEFAULT false,              -- «Вся Россия»
    category       text    REFERENCES categories (slug),        -- NULL — сфера не выбрана (черновик)
    formats        text[]  NOT NULL DEFAULT '{}',               -- reels, shorts, youtube, vk_clips, tiktok, telegram
    access         text    NOT NULL DEFAULT 'open' CHECK (access IN ('open', 'approval')),
    min_views      bigint  NOT NULL DEFAULT 0 CHECK (min_views >= 0),
    min_likes      bigint  NOT NULL DEFAULT 0 CHECK (min_likes >= 0),
    reward_kopecks bigint  NOT NULL DEFAULT 0 CHECK (reward_kopecks >= 0),
    publish_by     date,                                        -- срок публикации
    blocks         jsonb   NOT NULL DEFAULT '[]',               -- [{kind, title, items[]}]
    cover          text    REFERENCES media (id) ON DELETE SET NULL,
    photos         text[]  NOT NULL DEFAULT '{}',               -- media.id по порядку, до 6
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    submitted_at   timestamptz,
    published_at   timestamptz
);
CREATE INDEX tasks_owner_idx ON tasks (owner_id, updated_at DESC);
CREATE INDEX tasks_feed_idx ON tasks (published_at DESC, id DESC) WHERE status = 'published';
CREATE INDEX tasks_review_idx ON tasks (submitted_at) WHERE status = 'pending_review';

-- ===== анкеты авторов =====
CREATE TABLE creator_cards (
    user_id       bigint PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    status        text   NOT NULL DEFAULT 'draft'
                  CHECK (status IN ('draft', 'pending_review', 'published', 'rejected')),
    reject_reason text   NOT NULL DEFAULT '',
    name          text   NOT NULL DEFAULT '',
    city          text   NOT NULL DEFAULT '',
    topics        text[] NOT NULL DEFAULT '{}',                 -- categories.slug
    platforms     jsonb  NOT NULL DEFAULT '[]',                 -- [{kind, url, followers}]
    blocks        jsonb  NOT NULL DEFAULT '[]',
    avatar        text   REFERENCES media (id) ON DELETE SET NULL,
    works         text[] NOT NULL DEFAULT '{}',                 -- media.id по порядку, до 9
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    submitted_at  timestamptz,
    published_at  timestamptz
);
CREATE INDEX creator_cards_published_idx ON creator_cards (published_at DESC) WHERE status = 'published';
CREATE INDEX creator_cards_review_idx ON creator_cards (submitted_at) WHERE status = 'pending_review';

-- ===== журнал модерации =====
CREATE TABLE moderation_log (
    id          bigserial PRIMARY KEY,
    subject     text   NOT NULL CHECK (subject IN ('task', 'creator_card', 'category')),
    subject_id  bigint NOT NULL,                                -- tasks.id, creator_cards.user_id, categories.id
    from_status text   NOT NULL,
    to_status   text   NOT NULL,
    reason      text   NOT NULL DEFAULT '',
    actor_id    bigint REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX moderation_log_subject_idx ON moderation_log (subject, subject_id, id);
-- лимит отправок на проверку за сутки
CREATE INDEX moderation_log_submits_idx ON moderation_log (actor_id, created_at) WHERE to_status = 'pending_review';

-- +goose Down
DROP TABLE moderation_log;
DROP TABLE creator_cards;
DROP TABLE tasks;
DROP TABLE media_blobs;
DROP TABLE media;
DELETE FROM categories WHERE status <> 'approved' OR author_id IS NOT NULL;
DROP INDEX categories_author_idx;
DROP INDEX categories_norm_author_uq;
DROP INDEX categories_norm_common_uq;
ALTER TABLE categories DROP COLUMN norm, DROP COLUMN author_id, DROP COLUMN status;

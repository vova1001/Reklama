-- +goose Up
-- Публичные страницы (задача 14): страница-витрина бизнеса /b/<slug> и профиль автора /c/<slug> — из блоков, с
-- проверкой как у заданий и анкет (правка опубликованного — новая версия ждёт, старая видна), адрес (slug) —
-- уникальный, прежние адреса ведут на новый. Видео — файлом (MP4/WebM, без перекодирования) или ссылкой
-- YouTube / VK (встраивание по белому списку).

-- ===== страницы =====
-- Одна страница каждого вида на пользователя. blocks — блоки по порядку (jsonb, вид — в поле type); media — все фото
-- и видео опубликованного содержимого (по нему решается, видно ли файл всем); revision — правка опубликованного.
CREATE TABLE profile_pages (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id               bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind                  text   NOT NULL CHECK (kind IN ('business', 'creator')),
    slug                  text   NOT NULL,
    status                text   NOT NULL DEFAULT 'draft'
                          CHECK (status IN ('draft', 'pending_review', 'published', 'rejected', 'changes_requested')),
    reject_reason         text   NOT NULL DEFAULT '',
    title                 text   NOT NULL DEFAULT '',
    city                  text   NOT NULL DEFAULT '',
    spheres               text[] NOT NULL DEFAULT '{}',
    logo                  text   REFERENCES media (id) ON DELETE SET NULL,
    cover                 text   REFERENCES media (id) ON DELETE SET NULL,
    blocks                jsonb  NOT NULL DEFAULT '[]',
    media                 text[] NOT NULL DEFAULT '{}',
    review_notes          jsonb  NOT NULL DEFAULT '{}',
    revision              jsonb,
    revision_status       text   CHECK (revision_status IN ('draft', 'pending_review', 'rejected', 'changes_requested')),
    revision_reason       text   NOT NULL DEFAULT '',
    revision_submitted_at timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    submitted_at          timestamptz,
    published_at          timestamptz,
    UNIQUE (user_id, kind)
);
CREATE UNIQUE INDEX profile_pages_slug_uq ON profile_pages (kind, slug);
CREATE INDEX profile_pages_review_idx ON profile_pages (submitted_at) WHERE status = 'pending_review';
CREATE INDEX profile_pages_revision_review_idx ON profile_pages (revision_submitted_at) WHERE revision_status = 'pending_review';

-- Все адреса страницы — нынешний и прежние: прежний ведёт на нынешний и не достаётся другому.
CREATE TABLE page_slugs (
    kind       text   NOT NULL CHECK (kind IN ('business', 'creator')),
    slug       text   NOT NULL,
    page_id    bigint NOT NULL REFERENCES profile_pages (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, slug)
);
CREATE INDEX page_slugs_page_idx ON page_slugs (page_id, created_at);

-- ===== файлы страниц =====
ALTER TABLE media DROP CONSTRAINT media_purpose_check;
ALTER TABLE media ADD CONSTRAINT media_purpose_check CHECK (purpose IN ('avatar', 'task_cover', 'task_photo', 'card_avatar',
    'card_work', 'page_logo', 'page_cover', 'page_photo', 'page_video', 'video_poster'));
ALTER TABLE media DROP CONSTRAINT media_subject_check;
ALTER TABLE media ADD CONSTRAINT media_subject_check CHECK (subject IN ('user', 'task', 'creator_card', 'page'));

-- ===== проверка =====
ALTER TABLE moderation_log DROP CONSTRAINT moderation_log_subject_check;
ALTER TABLE moderation_log ADD CONSTRAINT moderation_log_subject_check CHECK (subject IN ('task', 'creator_card', 'category', 'page'));
ALTER TABLE review_decisions DROP CONSTRAINT review_decisions_kind_check;
ALTER TABLE review_decisions ADD CONSTRAINT review_decisions_kind_check
    CHECK (kind IN ('task', 'task_revision', 'card', 'card_revision', 'page', 'page_revision'));

-- Очередь: + страницы и правки опубликованных страниц (сферы — для отбора, название — для поиска).
CREATE OR REPLACE VIEW review_queue AS
SELECT 'task'::text AS kind, t.id::text AS item_id, t.title AS title, t.owner_id AS owner_id,
       t.categories AS categories, (CASE WHEN t.nationwide THEN '' ELSE t.city END)::text AS city,
       COALESCE(t.submitted_at, t.updated_at) AS waiting_since
FROM tasks t JOIN users u ON u.id = t.owner_id
WHERE t.status = 'pending_review' AND u.blocked_at IS NULL
UNION ALL
SELECT 'task_revision', t.id::text, COALESCE(NULLIF(t.revision ->> 'title', ''), t.title), t.owner_id,
       t.categories, (CASE WHEN t.nationwide THEN '' ELSE t.city END)::text,
       COALESCE(t.revision_submitted_at, t.updated_at)
FROM tasks t JOIN users u ON u.id = t.owner_id
WHERE t.status = 'published' AND t.revision_status = 'pending_review' AND u.blocked_at IS NULL
UNION ALL
SELECT 'card', c.user_id::text, c.name, c.user_id, c.topics, c.city, COALESCE(c.submitted_at, c.updated_at)
FROM creator_cards c JOIN users u ON u.id = c.user_id
WHERE c.status = 'pending_review' AND u.blocked_at IS NULL
UNION ALL
SELECT 'card_revision', c.user_id::text, COALESCE(NULLIF(c.revision ->> 'name', ''), c.name), c.user_id, c.topics, c.city,
       COALESCE(c.revision_submitted_at, c.updated_at)
FROM creator_cards c JOIN users u ON u.id = c.user_id
WHERE c.status = 'published' AND c.revision_status = 'pending_review' AND u.blocked_at IS NULL
UNION ALL
SELECT 'category', c.id::text, c.title, COALESCE(c.author_id, 0), ARRAY[]::text[], ''::text, c.created_at
FROM categories c
WHERE c.status = 'pending_review'
UNION ALL
SELECT 'format', f.norm, min(f.title), min(f.owner_id), ARRAY[]::text[], ''::text, min(f.since)
FROM (
    SELECT replace(lower(x.f), 'ё', 'е') AS norm, x.f AS title, t.owner_id, COALESCE(t.submitted_at, t.updated_at) AS since
    FROM tasks t JOIN users u ON u.id = t.owner_id, unnest(t.formats) AS x(f)
    WHERE t.status IN ('pending_review', 'published') AND u.blocked_at IS NULL
      AND x.f NOT IN ('reels', 'shorts', 'youtube', 'vk_clips', 'tiktok', 'telegram')
) f
WHERE NOT EXISTS (SELECT 1 FROM format_decisions d WHERE d.norm = f.norm)
GROUP BY f.norm
UNION ALL
SELECT 'page', p.id::text, p.title, p.user_id, p.spheres, p.city, COALESCE(p.submitted_at, p.updated_at)
FROM profile_pages p JOIN users u ON u.id = p.user_id
WHERE p.status = 'pending_review' AND u.blocked_at IS NULL
UNION ALL
SELECT 'page_revision', p.id::text, COALESCE(NULLIF(p.revision ->> 'title', ''), p.title), p.user_id, p.spheres, p.city,
       COALESCE(p.revision_submitted_at, p.updated_at)
FROM profile_pages p JOIN users u ON u.id = p.user_id
WHERE p.status = 'published' AND p.revision_status = 'pending_review' AND u.blocked_at IS NULL;

-- +goose Down
CREATE OR REPLACE VIEW review_queue AS
SELECT 'task'::text AS kind, t.id::text AS item_id, t.title AS title, t.owner_id AS owner_id,
       t.categories AS categories, (CASE WHEN t.nationwide THEN '' ELSE t.city END)::text AS city,
       COALESCE(t.submitted_at, t.updated_at) AS waiting_since
FROM tasks t JOIN users u ON u.id = t.owner_id
WHERE t.status = 'pending_review' AND u.blocked_at IS NULL
UNION ALL
SELECT 'task_revision', t.id::text, COALESCE(NULLIF(t.revision ->> 'title', ''), t.title), t.owner_id,
       t.categories, (CASE WHEN t.nationwide THEN '' ELSE t.city END)::text,
       COALESCE(t.revision_submitted_at, t.updated_at)
FROM tasks t JOIN users u ON u.id = t.owner_id
WHERE t.status = 'published' AND t.revision_status = 'pending_review' AND u.blocked_at IS NULL
UNION ALL
SELECT 'card', c.user_id::text, c.name, c.user_id, c.topics, c.city, COALESCE(c.submitted_at, c.updated_at)
FROM creator_cards c JOIN users u ON u.id = c.user_id
WHERE c.status = 'pending_review' AND u.blocked_at IS NULL
UNION ALL
SELECT 'card_revision', c.user_id::text, COALESCE(NULLIF(c.revision ->> 'name', ''), c.name), c.user_id, c.topics, c.city,
       COALESCE(c.revision_submitted_at, c.updated_at)
FROM creator_cards c JOIN users u ON u.id = c.user_id
WHERE c.status = 'published' AND c.revision_status = 'pending_review' AND u.blocked_at IS NULL
UNION ALL
SELECT 'category', c.id::text, c.title, COALESCE(c.author_id, 0), ARRAY[]::text[], ''::text, c.created_at
FROM categories c
WHERE c.status = 'pending_review'
UNION ALL
SELECT 'format', f.norm, min(f.title), min(f.owner_id), ARRAY[]::text[], ''::text, min(f.since)
FROM (
    SELECT replace(lower(x.f), 'ё', 'е') AS norm, x.f AS title, t.owner_id, COALESCE(t.submitted_at, t.updated_at) AS since
    FROM tasks t JOIN users u ON u.id = t.owner_id, unnest(t.formats) AS x(f)
    WHERE t.status IN ('pending_review', 'published') AND u.blocked_at IS NULL
      AND x.f NOT IN ('reels', 'shorts', 'youtube', 'vk_clips', 'tiktok', 'telegram')
) f
WHERE NOT EXISTS (SELECT 1 FROM format_decisions d WHERE d.norm = f.norm)
GROUP BY f.norm;

DELETE FROM review_leases WHERE kind IN ('page', 'page_revision');
DELETE FROM review_decisions WHERE kind IN ('page', 'page_revision');
ALTER TABLE review_decisions DROP CONSTRAINT review_decisions_kind_check;
ALTER TABLE review_decisions ADD CONSTRAINT review_decisions_kind_check CHECK (kind IN ('task', 'task_revision', 'card', 'card_revision'));
DELETE FROM moderation_log WHERE subject = 'page';
ALTER TABLE moderation_log DROP CONSTRAINT moderation_log_subject_check;
ALTER TABLE moderation_log ADD CONSTRAINT moderation_log_subject_check CHECK (subject IN ('task', 'creator_card', 'category'));

DROP TABLE page_slugs;
DROP TABLE profile_pages;

-- файлы страниц откреплены и удаляются: их назначения в прежней схеме не бывает (файлы в хранилище — без записи,
-- не видны и не мешают)
DELETE FROM media WHERE purpose IN ('page_logo', 'page_cover', 'page_photo', 'page_video', 'video_poster');
UPDATE media SET subject = NULL, subject_id = NULL WHERE subject = 'page';
ALTER TABLE media DROP CONSTRAINT media_subject_check;
ALTER TABLE media ADD CONSTRAINT media_subject_check CHECK (subject IN ('user', 'task', 'creator_card'));
ALTER TABLE media DROP CONSTRAINT media_purpose_check;
ALTER TABLE media ADD CONSTRAINT media_purpose_check CHECK (purpose IN ('avatar', 'task_cover', 'task_photo', 'card_avatar', 'card_work'));

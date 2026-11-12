-- +goose Up
-- Админка (задача 13): роли сотрудников, 2FA (TOTP + резервные коды), неизменяемый журнал действий, очередь проверки
-- с арендой, отложенные решения (отмена 10 с), «на доработку» с замечаниями, правки опубликованного (новая версия
-- ждёт, старая видна), жалобы, блокировка пользователей, входы, настройки (режим проверки) в базе.

-- ===== пользователи: роль сотрудника и блокировка =====
-- staff_role: NULL — обычный пользователь; moderator — проверка и жалобы; admin — всё + управление модераторами.
ALTER TABLE users
    ADD COLUMN staff_role     text CHECK (staff_role IN ('moderator', 'admin')),
    ADD COLUMN blocked_at     timestamptz,
    ADD COLUMN blocked_reason text NOT NULL DEFAULT '';
CREATE INDEX users_staff_idx ON users (staff_role) WHERE staff_role IS NOT NULL;
CREATE INDEX users_created_idx ON users (created_at);
CREATE INDEX users_name_lower_idx ON users (lower(name));

-- Время подтверждения 2FA в этой сессии: доступ к админке — не старше 12 часов.
ALTER TABLE sessions ADD COLUMN mfa_at timestamptz;

-- ===== 2FA сотрудников =====
-- secret — секрет TOTP, зашифрованный AES-256-GCM (AAD — id пользователя); confirmed_at NULL — настройка не завершена.
-- last_step — последний принятый шаг 30 с (повтор того же кода не проходит); failures/locked_until — перебор.
CREATE TABLE staff_totp (
    user_id      bigint PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret       bytea  NOT NULL,
    confirmed_at timestamptz,
    last_step    bigint NOT NULL DEFAULT 0,
    failures     integer NOT NULL DEFAULT 0,
    locked_until timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- Резервные коды: только HMAC-SHA-256, каждый — один раз.
CREATE TABLE staff_backup_codes (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  bytea  NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, code_hash)
);

-- ===== журнал действий сотрудников — только добавление =====
-- actor_id без внешнего ключа: строки журнала не меняются, даже когда пользователя удаляют.
CREATE TABLE audit_log (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id    bigint,
    actor_label text   NOT NULL DEFAULT '',   -- почта или имя на момент действия
    actor_role  text   NOT NULL DEFAULT '',
    action      text   NOT NULL,
    subject     text   NOT NULL DEFAULT '',
    subject_id  text   NOT NULL DEFAULT '',
    before      jsonb,
    after       jsonb,
    reason      text   NOT NULL DEFAULT '',
    ip          text   NOT NULL DEFAULT '',
    user_agent  text   NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_created_idx ON audit_log (created_at DESC, id DESC);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_id, created_at DESC);
CREATE INDEX audit_log_subject_idx ON audit_log (subject, subject_id, created_at DESC);
CREATE INDEX audit_log_action_idx ON audit_log (action, created_at DESC);

-- +goose StatementBegin
CREATE FUNCTION audit_log_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log: журнал действий не изменяется и не удаляется';
END
$$;
-- +goose StatementEnd
CREATE TRIGGER audit_log_no_change BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_immutable();
CREATE TRIGGER audit_log_no_truncate BEFORE TRUNCATE ON audit_log
    FOR EACH STATEMENT EXECUTE FUNCTION audit_log_immutable();

-- ===== «на доработку» и правки опубликованного =====
-- changes_requested — модератор вернул на доработку с замечаниями по полям (review_notes: поле → замечание).
-- Правка опубликованного: новая версия — revision (jsonb), её статус — revision_status; опубликованная видна
-- всем, пока правку не одобрят.
ALTER TABLE tasks DROP CONSTRAINT tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check
    CHECK (status IN ('draft', 'pending_review', 'published', 'rejected', 'expired', 'changes_requested'));
ALTER TABLE tasks
    ADD COLUMN review_notes          jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN revision              jsonb,
    ADD COLUMN revision_status       text CHECK (revision_status IN ('draft', 'pending_review', 'rejected', 'changes_requested')),
    ADD COLUMN revision_reason       text NOT NULL DEFAULT '',
    ADD COLUMN revision_submitted_at timestamptz;
CREATE INDEX tasks_revision_review_idx ON tasks (revision_submitted_at) WHERE revision_status = 'pending_review';

ALTER TABLE creator_cards DROP CONSTRAINT creator_cards_status_check;
ALTER TABLE creator_cards ADD CONSTRAINT creator_cards_status_check
    CHECK (status IN ('draft', 'pending_review', 'published', 'rejected', 'changes_requested'));
ALTER TABLE creator_cards
    ADD COLUMN review_notes          jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN revision              jsonb,
    ADD COLUMN revision_status       text CHECK (revision_status IN ('draft', 'pending_review', 'rejected', 'changes_requested')),
    ADD COLUMN revision_reason       text NOT NULL DEFAULT '',
    ADD COLUMN revision_submitted_at timestamptz;
CREATE INDEX creator_cards_revision_review_idx ON creator_cards (revision_submitted_at) WHERE revision_status = 'pending_review';

-- ===== свои категории: объединение с существующей =====
ALTER TABLE categories DROP CONSTRAINT categories_status_check;
ALTER TABLE categories ADD CONSTRAINT categories_status_check
    CHECK (status IN ('approved', 'pending_review', 'rejected', 'merged'));
ALTER TABLE categories ADD COLUMN merged_into text;

-- ===== свои форматы: решения модератора =====
-- norm — нижний регистр, ё → е. approved — в подсказках у всех; merged — заменён в заданиях на merged_into;
-- rejected — больше не принимается.
CREATE TABLE format_decisions (
    norm        text PRIMARY KEY,
    title       text NOT NULL,
    status      text NOT NULL CHECK (status IN ('approved', 'rejected', 'merged')),
    merged_into text NOT NULL DEFAULT '',
    decided_by  bigint REFERENCES users (id) ON DELETE SET NULL,
    decided_at  timestamptz NOT NULL DEFAULT now()
);

-- ===== очередь проверки =====
-- Аренда: элемент в работе у модератора до expires_at; другим не виден.
CREATE TABLE review_leases (
    kind         text   NOT NULL,
    item_id      text   NOT NULL,
    moderator_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    taken_at     timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    PRIMARY KEY (kind, item_id)
);
CREATE INDEX review_leases_moderator_idx ON review_leases (moderator_id);

-- Решение применяется через apply_at (10 с на отмену) фоновым исполнителем; lock_until — аренда исполнителя
-- (несколько серверов не применяют одно решение дважды).
CREATE TABLE review_decisions (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind          text   NOT NULL CHECK (kind IN ('task', 'task_revision', 'card', 'card_revision')),
    item_id       bigint NOT NULL,
    owner_id      bigint NOT NULL,
    moderator_id  bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    action        text   NOT NULL CHECK (action IN ('approve', 'reject', 'rework')),
    reason        text   NOT NULL DEFAULT '',
    notes         jsonb  NOT NULL DEFAULT '{}',
    waiting_since timestamptz,
    state         text   NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'applied', 'undone', 'failed')),
    apply_at      timestamptz NOT NULL,
    lock_until    timestamptz,
    applied_at    timestamptz,
    error         text   NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX review_decisions_due_idx ON review_decisions (apply_at) WHERE state = 'pending';
CREATE UNIQUE INDEX review_decisions_one_pending_uq ON review_decisions (kind, item_id) WHERE state = 'pending';
CREATE INDEX review_decisions_applied_idx ON review_decisions (applied_at) WHERE state = 'applied';
CREATE INDEX review_decisions_owner_idx ON review_decisions (owner_id, created_at DESC);

-- ===== жалобы =====
CREATE TABLE complaints (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    reporter_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    subject        text   NOT NULL CHECK (subject IN ('task', 'creator_card', 'user')),
    subject_id     bigint NOT NULL,
    target_user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,  -- чей это контент или профиль
    reason         text   NOT NULL CHECK (reason IN ('spam', 'fraud', 'offensive', 'illegal', 'fake', 'other')),
    comment        text   NOT NULL DEFAULT '',
    status         text   NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'dismissed')),
    action         text   NOT NULL DEFAULT '',   -- что сделано: hide | block | ''
    resolution     text   NOT NULL DEFAULT '',
    resolved_by    bigint REFERENCES users (id) ON DELETE SET NULL,
    resolved_at    timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX complaints_open_uq ON complaints (reporter_id, subject, subject_id) WHERE status = 'open';
CREATE INDEX complaints_open_idx ON complaints (created_at) WHERE status = 'open';
CREATE INDEX complaints_target_idx ON complaints (target_user_id, created_at DESC);
CREATE INDEX complaints_reporter_idx ON complaints (reporter_id, created_at);

-- ===== входы пользователей (карточка пользователя в админке; хранятся 180 дней) =====
CREATE TABLE login_events (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    method     text   NOT NULL,
    ip         text   NOT NULL DEFAULT '',
    user_agent text   NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_events_user_idx ON login_events (user_id, created_at DESC);
CREATE INDEX login_events_created_idx ON login_events (created_at);

-- ===== настройки, которые меняет администратор (переменная окружения — значение по умолчанию) =====
CREATE TABLE app_settings (
    key        text PRIMARY KEY,
    value      text NOT NULL,
    updated_by bigint REFERENCES users (id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- ===== очередь проверки одним видом =====
-- kind/item_id — что проверять; waiting_since — с какого момента ждёт. Заблокированные пользователи — не в очереди.
CREATE VIEW review_queue AS
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

-- +goose Down
DROP VIEW review_queue;
DROP TABLE app_settings;
DROP TABLE login_events;
DROP TABLE complaints;
DROP TABLE review_decisions;
DROP TABLE review_leases;
DROP TABLE format_decisions;

UPDATE categories SET status = 'rejected' WHERE status = 'merged';
ALTER TABLE categories DROP COLUMN merged_into;
ALTER TABLE categories DROP CONSTRAINT categories_status_check;
ALTER TABLE categories ADD CONSTRAINT categories_status_check CHECK (status IN ('approved', 'pending_review', 'rejected'));

DROP INDEX creator_cards_revision_review_idx;
ALTER TABLE creator_cards DROP COLUMN revision_submitted_at, DROP COLUMN revision_reason, DROP COLUMN revision_status,
    DROP COLUMN revision, DROP COLUMN review_notes;
UPDATE creator_cards SET status = 'rejected' WHERE status = 'changes_requested';
ALTER TABLE creator_cards DROP CONSTRAINT creator_cards_status_check;
ALTER TABLE creator_cards ADD CONSTRAINT creator_cards_status_check CHECK (status IN ('draft', 'pending_review', 'published', 'rejected'));

DROP INDEX tasks_revision_review_idx;
ALTER TABLE tasks DROP COLUMN revision_submitted_at, DROP COLUMN revision_reason, DROP COLUMN revision_status,
    DROP COLUMN revision, DROP COLUMN review_notes;
UPDATE tasks SET status = 'rejected' WHERE status = 'changes_requested';
ALTER TABLE tasks DROP CONSTRAINT tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check CHECK (status IN ('draft', 'pending_review', 'published', 'rejected', 'expired'));

DROP TRIGGER audit_log_no_truncate ON audit_log;
DROP TRIGGER audit_log_no_change ON audit_log;
DROP TABLE audit_log;
DROP FUNCTION audit_log_immutable();
DROP TABLE staff_backup_codes;
DROP TABLE staff_totp;
ALTER TABLE sessions DROP COLUMN mfa_at;
DROP INDEX users_name_lower_idx;
DROP INDEX users_created_idx;
DROP INDEX users_staff_idx;
ALTER TABLE users DROP COLUMN blocked_reason, DROP COLUMN blocked_at, DROP COLUMN staff_role;

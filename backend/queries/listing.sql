-- Задания бизнеса, анкеты авторов и журнал модерации.

-- ===== задания =====

-- name: CreateTask :one
INSERT INTO tasks (owner_id, title, city, nationwide, categories, formats, access, min_views,
                   reward_kopecks, publish_by, publish_time, deadline_at, blocks, cover, photos)
VALUES (@owner_id, @title, @city, @nationwide, @categories, @formats, @access, @min_views,
        @reward_kopecks, sqlc.narg(publish_by), sqlc.narg(publish_time), sqlc.narg(deadline_at), @blocks, sqlc.narg(cover), @photos)
RETURNING *;

-- name: GetOwnTask :one
SELECT * FROM tasks WHERE id = @id AND owner_id = @owner_id;

-- name: ListOwnTasks :many
SELECT * FROM tasks WHERE owner_id = @owner_id ORDER BY updated_at DESC, id DESC LIMIT @lim;

-- name: CountOwnTasks :one
SELECT count(*)::int FROM tasks WHERE owner_id = @owner_id;

-- Правка — только черновика, отклонённого или возвращённого на доработку (статус проверяется в том же запросе:
-- гонки с модерацией нет).
-- name: UpdateOwnTask :one
UPDATE tasks
SET title = @title, city = @city, nationwide = @nationwide, categories = @categories, formats = @formats,
    access = @access, min_views = @min_views, reward_kopecks = @reward_kopecks,
    publish_by = sqlc.narg(publish_by), publish_time = sqlc.narg(publish_time), deadline_at = sqlc.narg(deadline_at), blocks = @blocks, cover = sqlc.narg(cover), photos = @photos, updated_at = now()
WHERE id = @id AND owner_id = @owner_id AND status IN ('draft', 'rejected', 'changes_requested')
RETURNING *;

-- name: DeleteOwnTask :execrows
DELETE FROM tasks WHERE id = @id AND owner_id = @owner_id AND status IN ('draft', 'rejected', 'expired', 'changes_requested');

-- Смена статуса — только из ожидаемого (from): параллельная смена даёт 0 строк. Замечания — только при
-- «на доработку»; правка опубликованного уходит вместе с публикацией (её содержимое переносит сервис).
-- name: SetTaskStatus :one
UPDATE tasks
SET status = @to_status::text, reject_reason = CASE WHEN @to_status::text = 'rejected' THEN @reason::text ELSE '' END, updated_at = now(),
    review_notes = CASE WHEN @to_status::text = 'changes_requested' THEN @notes::jsonb ELSE '{}'::jsonb END,
    revision = CASE WHEN @to_status::text = 'published' THEN revision ELSE NULL END,
    revision_status = CASE WHEN @to_status::text = 'published' THEN revision_status ELSE NULL END,
    revision_reason = CASE WHEN @to_status::text = 'published' THEN revision_reason ELSE '' END,
    submitted_at = CASE WHEN @to_status::text = 'pending_review' THEN now() ELSE submitted_at END,
    published_at = CASE WHEN @to_status::text = 'published' THEN now() ELSE published_at END
WHERE id = @id AND status = @from_status::text
RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks WHERE id = @id;

-- Лента: опубликованные, новые сверху; курсор — (published_at, id) последнего на прошлой странице.
-- Отбор (пусто — без отбора): любая из сфер, город (без регистра; «Вся Россия» подходит любому), любой из форматов.
-- name: ListPublishedTasks :many
SELECT t.*, COALESCE(NULLIF(b.name, ''), NULLIF(u.name, ''), '')::text AS brand, ARRAY(SELECT c.title FROM unnest(t.categories) WITH ORDINALITY AS x(slug, n)
                   JOIN categories c ON c.slug = x.slug ORDER BY x.n)::text[] AS category_titles
FROM tasks t
JOIN users u ON u.id = t.owner_id
LEFT JOIN business_profiles b ON b.user_id = t.owner_id
WHERE t.status = 'published' AND t.deadline_at > now() AND u.blocked_at IS NULL
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (t.published_at, t.id) < (sqlc.narg(before_at)::timestamptz, @before_id::bigint))
  AND (cardinality(@categories::text[]) = 0 OR t.categories && @categories::text[])
  AND (@city::text = '' OR t.nationwide OR lower(t.city) = lower(@city::text))
  AND (cardinality(@formats::text[]) = 0 OR t.formats && @formats::text[])
ORDER BY t.published_at DESC, t.id DESC
LIMIT @lim;

-- name: GetPublishedTask :one
SELECT t.*, COALESCE(NULLIF(b.name, ''), NULLIF(u.name, ''), '')::text AS brand, ARRAY(SELECT c.title FROM unnest(t.categories) WITH ORDINALITY AS x(slug, n)
                   JOIN categories c ON c.slug = x.slug ORDER BY x.n)::text[] AS category_titles
FROM tasks t
JOIN users u ON u.id = t.owner_id
LEFT JOIN business_profiles b ON b.user_id = t.owner_id
WHERE t.id = @id AND t.status = 'published' AND t.deadline_at > now() AND u.blocked_at IS NULL;

-- Срок публикации прошёл — «завершено» (система) с записью в журнал; одним запросом.
-- name: ExpireTasks :many
WITH done AS (
    UPDATE tasks SET status = 'expired', updated_at = now(), revision = NULL, revision_status = NULL, revision_reason = ''
    WHERE status = 'published' AND deadline_at <= @now
    RETURNING id
), logged AS (
    INSERT INTO moderation_log (subject, subject_id, from_status, to_status, reason, actor_id, auto)
    SELECT 'task', id, 'published', 'expired', 'срок публикации прошёл', NULL, true FROM done
)
SELECT id FROM done;

-- ===== анкеты авторов =====

-- name: GetCard :one
SELECT * FROM creator_cards WHERE user_id = @user_id;

-- Создать или обновить — только черновик, отклонённую или возвращённую на доработку.
-- name: UpsertCard :one
INSERT INTO creator_cards (user_id, name, city, topics, platforms, blocks, avatar, works)
VALUES (@user_id, @name, @city, @topics, @platforms, @blocks, sqlc.narg(avatar), @works)
ON CONFLICT (user_id) DO UPDATE
SET name = EXCLUDED.name, city = EXCLUDED.city, topics = EXCLUDED.topics, platforms = EXCLUDED.platforms,
    blocks = EXCLUDED.blocks, avatar = EXCLUDED.avatar, works = EXCLUDED.works, updated_at = now()
WHERE creator_cards.status IN ('draft', 'rejected', 'changes_requested')
RETURNING *;

-- name: SetCardStatus :one
UPDATE creator_cards
SET status = @to_status::text, reject_reason = CASE WHEN @to_status::text = 'rejected' THEN @reason::text ELSE '' END, updated_at = now(),
    review_notes = CASE WHEN @to_status::text = 'changes_requested' THEN @notes::jsonb ELSE '{}'::jsonb END,
    revision = CASE WHEN @to_status::text = 'published' THEN revision ELSE NULL END,
    revision_status = CASE WHEN @to_status::text = 'published' THEN revision_status ELSE NULL END,
    revision_reason = CASE WHEN @to_status::text = 'published' THEN revision_reason ELSE '' END,
    submitted_at = CASE WHEN @to_status::text = 'pending_review' THEN now() ELSE submitted_at END,
    published_at = CASE WHEN @to_status::text = 'published' THEN now() ELSE published_at END
WHERE user_id = @user_id AND status = @from_status::text
RETURNING *;

-- Лента анкет: опубликованные, новые сверху; курсор — (published_at, user_id); отбор — любая из тем, город.
-- Названия тем — по порядку.
-- name: ListPublishedCards :many
SELECT cc.*, ARRAY(SELECT c.title FROM unnest(cc.topics) WITH ORDINALITY AS x(slug, n)
                   JOIN categories c ON c.slug = x.slug ORDER BY x.n)::text[] AS topic_titles
FROM creator_cards cc
JOIN users u ON u.id = cc.user_id
WHERE cc.status = 'published' AND u.blocked_at IS NULL
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (cc.published_at, cc.user_id) < (sqlc.narg(before_at)::timestamptz, @before_id::bigint))
  AND (cardinality(@topics::text[]) = 0 OR cc.topics && @topics::text[])
  AND (@city::text = '' OR lower(cc.city) = lower(@city::text))
ORDER BY cc.published_at DESC, cc.user_id DESC
LIMIT @lim;

-- name: GetPublishedCard :one
SELECT cc.*, ARRAY(SELECT c.title FROM unnest(cc.topics) WITH ORDINALITY AS x(slug, n)
                   JOIN categories c ON c.slug = x.slug ORDER BY x.n)::text[] AS topic_titles
FROM creator_cards cc
JOIN users u ON u.id = cc.user_id
WHERE cc.user_id = @user_id AND cc.status = 'published' AND u.blocked_at IS NULL;

-- ===== журнал модерации =====

-- name: LogStatusChange :exec
INSERT INTO moderation_log (subject, subject_id, from_status, to_status, reason, actor_id, auto)
VALUES (@subject, @subject_id, @from_status, @to_status, @reason, sqlc.narg(actor_id), @auto);

-- name: SubmitStatsSince :one
SELECT count(*)::int AS sent, COALESCE(min(created_at), now())::timestamptz AS first_at
FROM moderation_log
WHERE actor_id = @actor_id AND to_status = 'pending_review' AND created_at > @since;

-- name: StatusHistory :many
SELECT * FROM moderation_log WHERE subject = @subject AND subject_id = @subject_id ORDER BY id;

-- ===== фото, прикреплённые к заданию или анкете =====

-- Прикрепить свои фото (не прикреплённые к другому) — в той же транзакции, что и сохранение.
-- name: AttachMedia :execrows
UPDATE media SET subject = @subject, subject_id = @subject_id, touched_at = now()
WHERE owner_id = @owner_id AND id = ANY(@ids::text[])
  AND (subject IS NULL OR (subject = @subject AND subject_id = @subject_id));

-- Открепить фото, которых больше нет в задании или анкете (через сутки их удалит очистка).
-- name: DetachMedia :exec
UPDATE media SET subject = NULL, subject_id = NULL, touched_at = now()
WHERE subject = @subject AND subject_id = @subject_id AND NOT (id = ANY(@keep::text[]));

-- Свои форматы бизнеса (не из списка) — подсказки при вводе своего; чаще используемые сверху.
-- name: OwnTaskFormats :many
SELECT f::text AS format FROM tasks t, unnest(t.formats) AS f
WHERE t.owner_id = @owner_id AND NOT (f = ANY(@builtin::text[]))
GROUP BY f ORDER BY count(*) DESC, f LIMIT 50;

-- ===== правка опубликованного: новая версия ждёт проверки, опубликованная видна всем =====

-- Сохранить правку опубликованного задания (новая или после отказа / доработки; на проверке — нельзя).
-- name: SaveTaskRevision :one
UPDATE tasks SET revision = @revision, revision_status = 'draft', revision_reason = '', updated_at = now()
WHERE id = @id AND owner_id = @owner_id AND status = 'published'
  AND (revision_status IS NULL OR revision_status IN ('draft', 'rejected', 'changes_requested'))
RETURNING *;

-- Статус правки — только из ожидаемого.
-- name: SetTaskRevisionStatus :one
UPDATE tasks
SET revision_status = @to_status::text,
    revision_reason = CASE WHEN @to_status::text = 'rejected' THEN @reason::text ELSE '' END,
    review_notes = CASE WHEN @to_status::text = 'changes_requested' THEN @notes::jsonb ELSE '{}'::jsonb END,
    revision_submitted_at = CASE WHEN @to_status::text = 'pending_review' THEN now() ELSE revision_submitted_at END,
    updated_at = now()
WHERE id = @id AND status = 'published' AND revision_status = @from_status::text
RETURNING *;

-- Удалить правку (владелец передумал).
-- name: DropTaskRevision :one
UPDATE tasks SET revision = NULL, revision_status = NULL, revision_reason = '', revision_submitted_at = NULL,
                 review_notes = '{}', updated_at = now()
WHERE id = @id AND owner_id = @owner_id AND status = 'published' AND revision IS NOT NULL
RETURNING *;

-- Заменить содержимое задания новой версией (одобрение правки или снятие с публикации с правкой) — только из
-- ожидаемого статуса и статуса правки.
-- name: ReplaceTaskContent :one
UPDATE tasks
SET title = @title, city = @city, nationwide = @nationwide, categories = @categories, formats = @formats,
    access = @access, min_views = @min_views, reward_kopecks = @reward_kopecks,
    publish_by = sqlc.narg(publish_by), publish_time = sqlc.narg(publish_time), deadline_at = sqlc.narg(deadline_at),
    blocks = @blocks, cover = sqlc.narg(cover), photos = @photos,
    revision = NULL, revision_status = NULL, revision_reason = '', revision_submitted_at = NULL, review_notes = '{}',
    updated_at = now()
WHERE id = @id AND status = @status::text AND revision_status = @revision_status::text
RETURNING *;

-- name: SaveCardRevision :one
UPDATE creator_cards SET revision = @revision, revision_status = 'draft', revision_reason = '', updated_at = now()
WHERE user_id = @user_id AND status = 'published'
  AND (revision_status IS NULL OR revision_status IN ('draft', 'rejected', 'changes_requested'))
RETURNING *;

-- name: SetCardRevisionStatus :one
UPDATE creator_cards
SET revision_status = @to_status::text,
    revision_reason = CASE WHEN @to_status::text = 'rejected' THEN @reason::text ELSE '' END,
    review_notes = CASE WHEN @to_status::text = 'changes_requested' THEN @notes::jsonb ELSE '{}'::jsonb END,
    revision_submitted_at = CASE WHEN @to_status::text = 'pending_review' THEN now() ELSE revision_submitted_at END,
    updated_at = now()
WHERE user_id = @user_id AND status = 'published' AND revision_status = @from_status::text
RETURNING *;

-- name: DropCardRevision :one
UPDATE creator_cards SET revision = NULL, revision_status = NULL, revision_reason = '', revision_submitted_at = NULL,
                         review_notes = '{}', updated_at = now()
WHERE user_id = @user_id AND status = 'published' AND revision IS NOT NULL
RETURNING *;

-- name: ReplaceCardContent :one
UPDATE creator_cards
SET name = @name, city = @city, topics = @topics, blocks = @blocks, avatar = sqlc.narg(avatar), works = @works,
    revision = NULL, revision_status = NULL, revision_reason = '', revision_submitted_at = NULL, review_notes = '{}',
    updated_at = now()
WHERE user_id = @user_id AND status = @status::text AND revision_status = @revision_status::text
RETURNING *;

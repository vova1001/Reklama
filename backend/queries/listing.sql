-- Задания бизнеса, анкеты авторов и журнал модерации.

-- ===== задания =====

-- name: CreateTask :one
INSERT INTO tasks (owner_id, title, city, nationwide, category, formats, access, min_views, min_likes,
                   reward_kopecks, publish_by, blocks, cover, photos)
VALUES (@owner_id, @title, @city, @nationwide, sqlc.narg(category), @formats, @access, @min_views, @min_likes,
        @reward_kopecks, sqlc.narg(publish_by), @blocks, sqlc.narg(cover), @photos)
RETURNING *;

-- name: GetOwnTask :one
SELECT * FROM tasks WHERE id = @id AND owner_id = @owner_id;

-- name: ListOwnTasks :many
SELECT * FROM tasks WHERE owner_id = @owner_id ORDER BY updated_at DESC, id DESC LIMIT @lim;

-- name: CountOwnTasks :one
SELECT count(*)::int FROM tasks WHERE owner_id = @owner_id;

-- Правка — только черновика или отклонённого (статус проверяется в том же запросе: гонки с модерацией нет).
-- name: UpdateOwnTask :one
UPDATE tasks
SET title = @title, city = @city, nationwide = @nationwide, category = sqlc.narg(category), formats = @formats,
    access = @access, min_views = @min_views, min_likes = @min_likes, reward_kopecks = @reward_kopecks,
    publish_by = sqlc.narg(publish_by), blocks = @blocks, cover = sqlc.narg(cover), photos = @photos, updated_at = now()
WHERE id = @id AND owner_id = @owner_id AND status IN ('draft', 'rejected')
RETURNING *;

-- name: DeleteOwnTask :execrows
DELETE FROM tasks WHERE id = @id AND owner_id = @owner_id AND status IN ('draft', 'rejected');

-- Смена статуса — только из ожидаемого (from): параллельная смена даёт 0 строк.
-- name: SetTaskStatus :one
UPDATE tasks
SET status = @to_status::text, reject_reason = @reason, updated_at = now(),
    submitted_at = CASE WHEN @to_status::text = 'pending_review' THEN now() ELSE submitted_at END,
    published_at = CASE WHEN @to_status::text = 'published' THEN now() ELSE published_at END
WHERE id = @id AND status = @from_status::text
RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks WHERE id = @id;

-- Лента: опубликованные, новые сверху; курсор — (published_at, id) последнего на прошлой странице.
-- name: ListPublishedTasks :many
SELECT t.*, COALESCE(NULLIF(b.name, ''), NULLIF(u.name, ''), '')::text AS brand, COALESCE(c.title, '')::text AS category_title
FROM tasks t
JOIN users u ON u.id = t.owner_id
LEFT JOIN business_profiles b ON b.user_id = t.owner_id
LEFT JOIN categories c ON c.slug = t.category
WHERE t.status = 'published'
  AND (sqlc.narg(before_at)::timestamptz IS NULL OR (t.published_at, t.id) < (sqlc.narg(before_at)::timestamptz, @before_id::bigint))
ORDER BY t.published_at DESC, t.id DESC
LIMIT @lim;

-- ===== анкеты авторов =====

-- name: GetCard :one
SELECT * FROM creator_cards WHERE user_id = @user_id;

-- Создать или обновить — только черновик или отклонённую.
-- name: UpsertCard :one
INSERT INTO creator_cards (user_id, name, city, topics, platforms, blocks, avatar, works)
VALUES (@user_id, @name, @city, @topics, @platforms, @blocks, sqlc.narg(avatar), @works)
ON CONFLICT (user_id) DO UPDATE
SET name = EXCLUDED.name, city = EXCLUDED.city, topics = EXCLUDED.topics, platforms = EXCLUDED.platforms,
    blocks = EXCLUDED.blocks, avatar = EXCLUDED.avatar, works = EXCLUDED.works, updated_at = now()
WHERE creator_cards.status IN ('draft', 'rejected')
RETURNING *;

-- name: SetCardStatus :one
UPDATE creator_cards
SET status = @to_status::text, reject_reason = @reason, updated_at = now(),
    submitted_at = CASE WHEN @to_status::text = 'pending_review' THEN now() ELSE submitted_at END,
    published_at = CASE WHEN @to_status::text = 'published' THEN now() ELSE published_at END
WHERE user_id = @user_id AND status = @from_status::text
RETURNING *;

-- ===== журнал модерации =====

-- name: LogStatusChange :exec
INSERT INTO moderation_log (subject, subject_id, from_status, to_status, reason, actor_id)
VALUES (@subject, @subject_id, @from_status, @to_status, @reason, sqlc.narg(actor_id));

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

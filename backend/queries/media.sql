-- Фото: метаданные, права на просмотр, квоты, очистка неприкреплённых.

-- name: CreateMedia :one
INSERT INTO media (id, owner_id, purpose, width, height, bytes)
VALUES (@id, @owner_id, @purpose, @width, @height, @bytes)
RETURNING *;

-- name: GetOwnMedia :many
SELECT * FROM media WHERE owner_id = @owner_id AND id = ANY(@ids::text[]);

-- name: MediaUsage :one
SELECT count(*)::int AS files, COALESCE(sum(bytes), 0)::bigint AS bytes FROM media WHERE owner_id = @owner_id;

-- Кому видно: аватар профиля — всем; фото задания или анкеты — всем, когда они опубликованы; иначе — владельцу.
-- name: MediaAccess :one
SELECT m.id, m.owner_id,
       (CASE
            WHEN m.subject = 'user' THEN true
            WHEN m.subject = 'task' THEN EXISTS (SELECT 1 FROM tasks t WHERE t.id = m.subject_id AND t.status = 'published')
            WHEN m.subject = 'creator_card' THEN EXISTS (SELECT 1 FROM creator_cards c WHERE c.user_id = m.subject_id AND c.status = 'published')
            ELSE false
        END)::boolean AS public
FROM media m WHERE m.id = @id;

-- Удалить своё неприкреплённое фото (прикреплённое открепляется сохранением задания или анкеты).
-- name: DeleteOwnLooseMedia :execrows
DELETE FROM media WHERE id = @id AND owner_id = @owner_id AND subject IS NULL;

-- Аватар профиля: новое фото прикрепляется к пользователю, прежнее — открепляется.
-- name: AttachAvatar :execrows
UPDATE media SET subject = 'user', subject_id = @owner_id, touched_at = now()
WHERE id = @id AND owner_id = @owner_id AND purpose = 'avatar' AND (subject IS NULL OR subject = 'user');

-- name: DetachAvatars :exec
UPDATE media SET subject = NULL, subject_id = NULL, touched_at = now()
WHERE subject = 'user' AND subject_id = @owner_id AND id <> @keep;

-- name: SetUserAvatar :exec
UPDATE users SET avatar_url = @avatar_url, updated_at = now() WHERE id = @id;

-- Неприкреплённые дольше срока — к удалению (файлы удаляются из хранилища до строк).
-- name: ListOrphanMedia :many
SELECT id FROM media WHERE subject IS NULL AND touched_at < @before ORDER BY touched_at LIMIT @lim;

-- name: DeleteMedia :exec
DELETE FROM media WHERE id = ANY(@ids::text[]) AND subject IS NULL;

-- ===== хранилище файлов в PostgreSQL =====

-- name: PutBlob :exec
INSERT INTO media_blobs (key, content_type, data) VALUES (@key, @content_type, @data)
ON CONFLICT (key) DO UPDATE SET content_type = EXCLUDED.content_type, data = EXCLUDED.data;

-- name: GetBlob :one
SELECT content_type, data, created_at FROM media_blobs WHERE key = @key;

-- name: DeleteBlobs :exec
DELETE FROM media_blobs WHERE key = ANY(@keys::text[]);

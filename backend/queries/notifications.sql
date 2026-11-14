-- Уведомления в приложении: вставка (в транзакции итога решения), список, счётчик непрочитанных, прочтение,
-- подметание старых прочитанных.

-- name: InsertNotification :exec
-- ON CONFLICT — повтор исполнителя не создаст дубль уведомления.
INSERT INTO notifications (user_id, decision_id, kind, item_id, action, title, reason, path)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (decision_id) DO NOTHING;

-- name: Notifications :many
SELECT id, kind, item_id, action, title, reason, path, read_at, created_at
FROM notifications
WHERE user_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: UnreadNotifications :one
SELECT count(*)
FROM notifications
WHERE user_id = $1 AND read_at IS NULL;

-- name: ReadNotification :exec
UPDATE notifications
SET read_at = now()
WHERE id = $1 AND user_id = $2 AND read_at IS NULL;

-- name: ReadAllNotifications :exec
UPDATE notifications
SET read_at = now()
WHERE user_id = $1 AND read_at IS NULL;

-- name: DeleteOldNotifications :exec
DELETE FROM notifications
WHERE read_at IS NOT NULL AND read_at < $1;

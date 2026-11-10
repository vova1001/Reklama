-- name: CreateTelegramLogin :exec
INSERT INTO telegram_logins (token_hash, browser_hash, role, ip, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: CountTelegramLoginsByIP :one
SELECT count(*)::int AS started, COALESCE(min(created_at), 'epoch'::timestamptz)::timestamptz AS first_at
FROM telegram_logins WHERE ip = $1 AND created_at > $2;

-- Ожидающий вход по токену из ссылки (для бота).
-- name: GetPendingTelegramLogin :one
SELECT * FROM telegram_logins
WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > $2;

-- Подтверждение: только ожидающий и неистёкший; повторное подтверждение тем же человеком — без изменений.
-- После подтверждения у браузера есть grace-период забрать вход.
-- name: ConfirmTelegramLogin :one
UPDATE telegram_logins
SET tg_user_id = $3, tg_name = $4, tg_username = $5, confirmed_at = COALESCE(confirmed_at, $2), expires_at = GREATEST(expires_at, $6)
WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > $2
  AND (tg_user_id IS NULL OR tg_user_id = $3)
RETURNING *;

-- name: CancelTelegramLogin :one
UPDATE telegram_logins SET consumed_at = $2, cancelled = true
WHERE token_hash = $1 AND consumed_at IS NULL AND confirmed_at IS NULL
RETURNING *;

-- Состояние входа для браузера (по секрету из cookie).
-- name: GetTelegramLoginByBrowser :one
SELECT * FROM telegram_logins WHERE browser_hash = $1;

-- Забрать подтверждённый вход может только один запрос.
-- name: ConsumeTelegramLogin :one
UPDATE telegram_logins SET consumed_at = $2
WHERE browser_hash = $1 AND confirmed_at IS NOT NULL AND consumed_at IS NULL AND expires_at > $2
RETURNING *;

-- name: DeleteOldTelegramLogins :execrows
DELETE FROM telegram_logins WHERE created_at < $1;

-- name: UpsertTelegramChat :one
INSERT INTO telegram_chats (tg_user_id, chat_id, username)
VALUES ($1, $2, $3)
ON CONFLICT (tg_user_id) DO UPDATE
SET chat_id = EXCLUDED.chat_id, username = EXCLUDED.username, blocked_at = NULL, updated_at = now()
RETURNING *;

-- name: SetTelegramChatNotify :execrows
UPDATE telegram_chats SET notify = $2, updated_at = now() WHERE tg_user_id = $1;

-- name: BlockTelegramChat :execrows
UPDATE telegram_chats SET blocked_at = now(), updated_at = now() WHERE chat_id = $1 AND blocked_at IS NULL;

-- name: GetTelegramChat :one
SELECT * FROM telegram_chats WHERE tg_user_id = $1;

-- Куда писать пользователю сайта: его Telegram, если он писал боту, не остановил его и не выключил уведомления.
-- name: GetUserNotifyChat :one
SELECT c.*
FROM identities i
JOIN telegram_chats c ON c.tg_user_id::text = i.subject
WHERE i.user_id = $1 AND i.provider = 'telegram' AND c.notify AND c.blocked_at IS NULL
LIMIT 1;

-- Устройство: true — увидели впервые.
-- name: TouchUserDevice :one
INSERT INTO user_devices (user_id, device_hash) VALUES ($1, $2)
ON CONFLICT (user_id, device_hash) DO UPDATE SET last_seen_at = now()
RETURNING (xmax = 0)::boolean AS inserted;

-- name: DeleteUserSessions :execrows
DELETE FROM sessions WHERE user_id = $1;

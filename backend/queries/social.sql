-- ===== подтверждённые соцсети =====

-- name: ListSocialAccounts :many
SELECT * FROM social_accounts WHERE user_id = $1 ORDER BY platform, created_at, id;

-- Подтверждённые аккаунты нескольких авторов (анкеты в ленте): по площадкам, крупные сверху.
-- name: ListVerifiedSocialAccounts :many
SELECT * FROM social_accounts
WHERE user_id = ANY(@user_ids::bigint[]) AND status = 'verified'
ORDER BY user_id, followers DESC NULLS LAST, id;

-- name: GetSocialAccount :one
SELECT * FROM social_accounts WHERE id = $1 AND user_id = $2;

-- name: CountSocialAccounts :one
SELECT count(*)::int FROM social_accounts WHERE user_id = $1;

-- Подтверждение (новый аккаунт или повторное): цифры, статус verified, следующее обновление.
-- name: UpsertSocialAccount :one
INSERT INTO social_accounts (user_id, platform, external_id, kind, handle, title, url, status, method, credential_id,
                             followers, followers_hidden, views, videos, likes, fail_reason, failures,
                             verified_at, refreshed_at, next_refresh_at)
VALUES (@user_id, @platform, @external_id, @kind, @handle, @title, @url, @status, @method, sqlc.narg(credential_id),
        sqlc.narg(followers), @followers_hidden, sqlc.narg(views), sqlc.narg(videos), sqlc.narg(likes), @fail_reason, 0,
        sqlc.narg(verified_at), sqlc.narg(refreshed_at), sqlc.narg(next_refresh_at))
ON CONFLICT (user_id, platform, external_id) DO UPDATE
SET kind = EXCLUDED.kind, handle = EXCLUDED.handle, title = EXCLUDED.title, url = EXCLUDED.url,
    status = EXCLUDED.status, method = EXCLUDED.method, credential_id = EXCLUDED.credential_id,
    followers = EXCLUDED.followers, followers_hidden = EXCLUDED.followers_hidden, views = EXCLUDED.views,
    videos = EXCLUDED.videos, likes = EXCLUDED.likes, fail_reason = EXCLUDED.fail_reason, failures = 0,
    verified_at = COALESCE(EXCLUDED.verified_at, social_accounts.verified_at),
    refreshed_at = COALESCE(EXCLUDED.refreshed_at, social_accounts.refreshed_at),
    next_refresh_at = EXCLUDED.next_refresh_at, updated_at = now()
RETURNING *;

-- Свежие цифры после обновления (имя и ссылка могли поменяться).
-- name: SetSocialStats :one
UPDATE social_accounts
SET status = 'verified', handle = @handle, title = @title, url = @url,
    followers = sqlc.narg(followers), followers_hidden = @followers_hidden, views = sqlc.narg(views),
    videos = sqlc.narg(videos), likes = sqlc.narg(likes), fail_reason = '', failures = 0,
    verified_at = COALESCE(verified_at, @refreshed_at), refreshed_at = @refreshed_at,
    next_refresh_at = @next_refresh_at, updated_at = now()
WHERE id = @id
RETURNING *;

-- Временный сбой: статус не меняется, следующая попытка — позже.
-- name: SetSocialRetry :exec
UPDATE social_accounts
SET failures = failures + 1, next_refresh_at = @next_refresh_at, updated_at = now()
WHERE id = @id;

-- Доступ потерян: failed, причина, больше не обновляется сам.
-- name: SetSocialFailed :exec
UPDATE social_accounts
SET status = 'failed', fail_reason = @fail_reason, next_refresh_at = NULL, updated_at = now()
WHERE id = @id;

-- Аренда на обновление: due-аккаунты (по сроку), следующая попытка сдвигается на lease — другой сервер их не возьмёт.
-- name: ClaimDueSocialAccounts :many
UPDATE social_accounts SET next_refresh_at = @lease_until, updated_at = now()
WHERE id IN (
    SELECT a.id FROM social_accounts a
    WHERE a.next_refresh_at IS NOT NULL AND a.next_refresh_at <= @now
    ORDER BY a.next_refresh_at
    LIMIT @lim
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: DeleteSocialAccount :one
DELETE FROM social_accounts WHERE id = $1 AND user_id = $2 RETURNING *;

-- Сколько аккаунтов ещё пользуются доступом (отвязка последнего — отзыв токена).
-- name: CountCredentialAccounts :one
SELECT count(*)::int FROM social_accounts WHERE credential_id = $1;

-- name: InsertSocialStats :exec
INSERT INTO social_stats_history (account_id, at, followers, views, videos, likes)
VALUES (@account_id, @at, sqlc.narg(followers), sqlc.narg(views), sqlc.narg(videos), sqlc.narg(likes))
ON CONFLICT (account_id, at) DO NOTHING;

-- name: ListSocialStats :many
SELECT * FROM social_stats_history WHERE account_id = $1 ORDER BY at DESC LIMIT $2;

-- Telegram: все аккаунты канала у всех авторов (бот удалён — всем failed).
-- name: ListSocialAccountsByExternal :many
SELECT * FROM social_accounts WHERE platform = $1 AND external_id = $2;

-- ===== доступы OAuth =====

-- name: UpsertSocialCredential :one
INSERT INTO social_credentials (user_id, platform, subject, secret, access_expires_at, refresh_expires_at)
VALUES (@user_id, @platform, @subject, @secret, sqlc.narg(access_expires_at), sqlc.narg(refresh_expires_at))
ON CONFLICT (user_id, platform, subject) DO UPDATE
SET secret = EXCLUDED.secret, access_expires_at = EXCLUDED.access_expires_at,
    refresh_expires_at = EXCLUDED.refresh_expires_at, lock_until = NULL, updated_at = now()
RETURNING *;

-- name: GetSocialCredential :one
SELECT * FROM social_credentials WHERE id = $1;

-- Аренда доступа на обновление токена: только если не занят (или аренда истекла).
-- name: LockSocialCredential :one
UPDATE social_credentials SET lock_until = @lock_until
WHERE id = @id AND (lock_until IS NULL OR lock_until < @now)
RETURNING *;

-- Новые токены и снятие аренды.
-- name: SaveSocialCredential :exec
UPDATE social_credentials
SET secret = @secret, access_expires_at = sqlc.narg(access_expires_at), refresh_expires_at = sqlc.narg(refresh_expires_at),
    lock_until = NULL, updated_at = now()
WHERE id = @id;

-- name: UnlockSocialCredential :exec
UPDATE social_credentials SET lock_until = NULL WHERE id = $1;

-- name: DeleteSocialCredential :exec
DELETE FROM social_credentials WHERE id = $1;

-- ===== начатые подключения =====

-- name: CreateSocialState :exec
INSERT INTO social_oauth_states (state_hash, user_id, platform, verifier, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: CountSocialStates :one
SELECT count(*)::int AS started, COALESCE(min(created_at), 'epoch'::timestamptz)::timestamptz AS first_at
FROM social_oauth_states WHERE user_id = $1 AND created_at > $2;

-- Забрать state можно один раз и только до срока.
-- name: TakeSocialState :one
DELETE FROM social_oauth_states WHERE state_hash = $1 AND platform = $2 AND expires_at > $3 RETURNING *;

-- name: DeleteOldSocialStates :execrows
DELETE FROM social_oauth_states WHERE expires_at < $1;

-- ===== Telegram: каналы с ботом и привязка аккаунта =====

-- name: UpsertTelegramChannel :exec
INSERT INTO telegram_channels (chat_id, title, username, chat_type, added_by, bot_status, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (chat_id) DO UPDATE
SET title = EXCLUDED.title, username = EXCLUDED.username, chat_type = EXCLUDED.chat_type, added_by = EXCLUDED.added_by,
    bot_status = EXCLUDED.bot_status, updated_at = now();

-- name: GetTelegramChannel :one
SELECT * FROM telegram_channels WHERE chat_id = $1;

-- Каналы и группы, куда этот человек добавил бота.
-- name: ListTelegramChannelsAddedBy :many
SELECT * FROM telegram_channels WHERE added_by = $1 AND bot_status IN ('administrator', 'member') ORDER BY updated_at DESC LIMIT 20;

-- Telegram-аккаунт пользователя (вход через Telegram или привязка).
-- name: GetUserTelegramID :one
SELECT subject FROM identities WHERE user_id = $1 AND provider = 'telegram' ORDER BY last_used_at DESC LIMIT 1;

-- Начатое подключение без погашения (бот показывает, к какому аккаунту привязка).
-- name: GetSocialState :one
SELECT * FROM social_oauth_states WHERE state_hash = $1 AND platform = $2 AND expires_at > $3;

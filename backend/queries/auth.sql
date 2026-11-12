-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByIdentity :one
SELECT u.*
FROM identities i
JOIN users u ON u.id = i.user_id
WHERE i.provider = $1 AND i.subject = $2;

-- name: CreateUser :one
INSERT INTO users (name, email, avatar_url)
VALUES ($1, $2, $3)
RETURNING *;

-- name: TouchUserLogin :exec
UPDATE users SET last_login_at = now() WHERE id = $1;

-- Почта, подтверждённая новым способом входа, если у человека её ещё нет.
-- name: SetUserEmailIfEmpty :exec
UPDATE users SET email = $2, updated_at = now() WHERE id = $1 AND email IS NULL;

-- Общие данные профиля; первое сохранение отмечает «общие данные заполнены».
-- name: UpdateUserBasics :one
UPDATE users
SET name = $2, city = $3, onboarded_at = COALESCE(onboarded_at, now()), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListUserProviders :many
SELECT provider FROM identities WHERE user_id = $1 ORDER BY provider;

-- name: CreateIdentity :exec
INSERT INTO identities (user_id, provider, subject, email)
VALUES ($1, $2, $3, $4);

-- name: TouchIdentity :exec
UPDATE identities SET last_used_at = now() WHERE provider = $1 AND subject = $2;

-- Новый код делает прежние коды этой почты недействительными.
-- name: ConsumeActiveCodes :exec
UPDATE login_codes SET consumed_at = $2 WHERE email = $1 AND consumed_at IS NULL;

-- name: CreateLoginCode :exec
INSERT INTO login_codes (email, code_hash, ip, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- name: GetActiveCode :one
SELECT * FROM login_codes
WHERE email = $1 AND consumed_at IS NULL AND expires_at > $2
ORDER BY created_at DESC
LIMIT 1;

-- Попытка засчитывается атомарно: при параллельных запросах шестой не пройдёт.
-- name: IncCodeAttempts :one
UPDATE login_codes SET attempts = attempts + 1
WHERE id = $1 AND attempts < $2 AND consumed_at IS NULL
RETURNING attempts;

-- Код одноразовый: погасить может только один запрос.
-- name: ConsumeCode :execrows
UPDATE login_codes SET consumed_at = $2 WHERE id = $1 AND consumed_at IS NULL;

-- name: CodeStatsByEmail :one
SELECT count(*)::int AS sent, COALESCE(max(created_at), 'epoch'::timestamptz)::timestamptz AS last_at,
       COALESCE(min(created_at), 'epoch'::timestamptz)::timestamptz AS first_at
FROM login_codes WHERE email = $1 AND created_at > $2;

-- name: CodeStatsByIP :one
SELECT count(*)::int AS sent, COALESCE(min(created_at), 'epoch'::timestamptz)::timestamptz AS first_at
FROM login_codes WHERE ip = $1 AND created_at > $2;

-- name: DeleteOldCodes :execrows
DELETE FROM login_codes WHERE created_at < $1;

-- name: CreateSession :exec
INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3);

-- name: GetSessionUser :one
SELECT u.*
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.expires_at > $2 AND u.blocked_at IS NULL;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at < $1;

-- 2FA в сессии: когда подтверждена (доступ к админке — не старше 12 часов).
-- name: GetSessionMFA :one
SELECT id, mfa_at FROM sessions WHERE token_hash = @token_hash AND expires_at > @now;

-- name: SetSessionMFA :execrows
UPDATE sessions SET mfa_at = @mfa_at WHERE token_hash = @token_hash AND expires_at > @now;

-- name: AddLoginEvent :exec
INSERT INTO login_events (user_id, method, ip, user_agent) VALUES (@user_id, @method, @ip, @user_agent);

-- name: DeleteOldLoginEvents :execrows
DELETE FROM login_events WHERE created_at < @before;

-- Первый админ из ADMIN_EMAILS: роль назначается, только если её ещё нет (понижать роль так нельзя).
-- name: GrantBootstrapAdmin :one
UPDATE users SET staff_role = 'admin', updated_at = now() WHERE id = @id AND staff_role IS DISTINCT FROM 'admin'
RETURNING *;

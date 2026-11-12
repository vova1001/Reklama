-- Админка: 2FA сотрудников, журнал действий, настройки, очередь проверки (аренда, отложенные решения), свои
-- форматы и категории, жалобы, пользователи, сводка.

-- ===== 2FA =====

-- name: GetStaffTOTP :one
SELECT * FROM staff_totp WHERE user_id = @user_id;

-- Новый секрет — только пока 2FA не подтверждена (подтверждённую сбрасывает администратор).
-- name: UpsertStaffTOTP :one
INSERT INTO staff_totp (user_id, secret) VALUES (@user_id, @secret)
ON CONFLICT (user_id) DO UPDATE
SET secret = EXCLUDED.secret, last_step = 0, failures = 0, locked_until = NULL, updated_at = now()
WHERE staff_totp.confirmed_at IS NULL
RETURNING *;

-- Принять шаг TOTP: только больше последнего принятого (повтор кода не проходит); при настройке — подтверждение.
-- name: AcceptTOTPStep :execrows
UPDATE staff_totp
SET last_step = @step, failures = 0, locked_until = NULL, updated_at = now(),
    confirmed_at = COALESCE(confirmed_at, @now::timestamptz)
WHERE user_id = @user_id AND last_step < @step
  AND (sqlc.arg(confirming)::boolean = (confirmed_at IS NULL));

-- Неверный код: счётчик; на пределе — блокировка до lock_until и счётчик заново.
-- name: TOTPFailure :one
UPDATE staff_totp
SET failures = CASE WHEN failures + 1 >= @max_failures::int THEN 0 ELSE failures + 1 END,
    locked_until = CASE WHEN failures + 1 >= @max_failures::int THEN @lock_until::timestamptz ELSE locked_until END,
    updated_at = now()
WHERE user_id = @user_id
RETURNING failures, locked_until;

-- name: DeleteStaffTOTP :exec
DELETE FROM staff_totp WHERE user_id = @user_id;

-- name: DeleteBackupCodes :exec
DELETE FROM staff_backup_codes WHERE user_id = @user_id;

-- name: InsertBackupCode :exec
INSERT INTO staff_backup_codes (user_id, code_hash) VALUES (@user_id, @code_hash);

-- name: UseBackupCode :execrows
UPDATE staff_backup_codes SET used_at = @now WHERE user_id = @user_id AND code_hash = @code_hash AND used_at IS NULL;

-- name: CountBackupCodes :one
SELECT count(*)::int FROM staff_backup_codes WHERE user_id = @user_id AND used_at IS NULL;

-- Сбросить подтверждение 2FA во всех сессиях пользователя (сброс 2FA, смена роли).
-- name: ClearUserSessionsMFA :exec
UPDATE sessions SET mfa_at = NULL WHERE user_id = @user_id;

-- ===== журнал действий (только INSERT; UPDATE и DELETE запрещены триггером) =====

-- name: InsertAudit :one
INSERT INTO audit_log (actor_id, actor_label, actor_role, action, subject, subject_id, before, after, reason, ip, user_agent)
VALUES (sqlc.narg(actor_id), @actor_label, @actor_role, @action, @subject, @subject_id, sqlc.narg(before), sqlc.narg(after),
        @reason, @ip, @user_agent)
RETURNING id, created_at;

-- Поиск: текст — по действию, предмету, причине, сотруднику и адресу; фильтры пустые — без отбора. Новые сверху.
-- name: SearchAudit :many
SELECT * FROM audit_log a
WHERE (@q::text = '' OR a.action ILIKE '%' || @q::text || '%' OR a.subject_id = @q::text OR a.reason ILIKE '%' || @q::text || '%'
       OR a.actor_label ILIKE '%' || @q::text || '%' OR a.ip = @q::text)
  AND (@actor_id::bigint = 0 OR a.actor_id = @actor_id::bigint)
  AND (@action::text = '' OR a.action = @action::text)
  AND (@subject::text = '' OR a.subject = @subject::text)
  AND (@subject_id::text = '' OR a.subject_id = @subject_id::text)
  AND (sqlc.narg(since)::timestamptz IS NULL OR a.created_at >= sqlc.narg(since)::timestamptz)
  AND (sqlc.narg(until)::timestamptz IS NULL OR a.created_at < sqlc.narg(until)::timestamptz)
  AND (@before_id::bigint = 0 OR a.id < @before_id::bigint)
ORDER BY a.id DESC
LIMIT @lim;

-- ===== настройки =====

-- name: GetSetting :one
SELECT * FROM app_settings WHERE key = @key;

-- name: UpsertSetting :one
INSERT INTO app_settings (key, value, updated_by, updated_at) VALUES (@key, @value, sqlc.narg(updated_by), now())
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING *;

-- ===== очередь проверки =====

-- Взять в работу: свободно, истекло или уже своё — иначе 0 строк (в работе у другого).
-- name: ClaimLease :one
INSERT INTO review_leases (kind, item_id, moderator_id, taken_at, expires_at)
VALUES (@kind, @item_id, @moderator_id, @now::timestamptz, @expires_at)
ON CONFLICT (kind, item_id) DO UPDATE
SET moderator_id = EXCLUDED.moderator_id, expires_at = EXCLUDED.expires_at,
    taken_at = CASE WHEN review_leases.moderator_id = EXCLUDED.moderator_id THEN review_leases.taken_at ELSE EXCLUDED.taken_at END
WHERE review_leases.expires_at <= @now::timestamptz OR review_leases.moderator_id = EXCLUDED.moderator_id
RETURNING *;

-- name: GetLease :one
SELECT * FROM review_leases WHERE kind = @kind AND item_id = @item_id;

-- name: ReleaseLease :exec
DELETE FROM review_leases WHERE kind = @kind AND item_id = @item_id AND moderator_id = @moderator_id;

-- name: DeleteLease :exec
DELETE FROM review_leases WHERE kind = @kind AND item_id = @item_id;

-- name: DeleteExpiredLeases :execrows
DELETE FROM review_leases WHERE expires_at < @before;

-- Очередь: без взятых другими (аренда не истекла) и без ждущих применения решений. Старые первыми (или новые).
-- name: ListQueue :many
SELECT q.kind, q.item_id, q.title, q.owner_id, q.categories, q.city, q.waiting_since,
       COALESCE(NULLIF(b.name, ''), NULLIF(cp.name, ''), NULLIF(u.name, ''), '')::text AS owner_name,
       (l.moderator_id IS NOT NULL AND l.moderator_id = @me::bigint AND l.expires_at > @now::timestamptz)::boolean AS mine
FROM review_queue q
LEFT JOIN users u ON u.id = q.owner_id
LEFT JOIN business_profiles b ON b.user_id = q.owner_id AND q.kind IN ('task', 'task_revision')
LEFT JOIN creator_profiles cp ON cp.user_id = q.owner_id AND q.kind IN ('card', 'card_revision')
LEFT JOIN review_leases l ON l.kind = q.kind AND l.item_id = q.item_id
WHERE (cardinality(@kinds::text[]) = 0 OR q.kind = ANY(@kinds::text[]))
  AND (cardinality(@categories::text[]) = 0 OR q.categories && @categories::text[])
  AND (@city::text = '' OR lower(q.city) = lower(@city::text))
  AND (sqlc.narg(since)::timestamptz IS NULL OR q.waiting_since >= sqlc.narg(since)::timestamptz)
  AND (sqlc.narg(until)::timestamptz IS NULL OR q.waiting_since < sqlc.narg(until)::timestamptz)
  AND (@q::text = '' OR q.title ILIKE '%' || @q::text || '%' OR q.item_id = @q::text)
  AND (l.moderator_id IS NULL OR l.expires_at <= @now::timestamptz OR l.moderator_id = @me::bigint)
  AND NOT EXISTS (SELECT 1 FROM review_decisions d WHERE d.state = 'pending' AND d.kind = q.kind AND d.item_id::text = q.item_id)
ORDER BY CASE WHEN @newest::boolean THEN q.waiting_since END DESC NULLS LAST, q.waiting_since ASC, q.kind, q.item_id
LIMIT @lim OFFSET @off;

-- name: CountQueue :one
SELECT count(*)::int AS total, min(q.waiting_since)::timestamptz AS oldest
FROM review_queue q
LEFT JOIN review_leases l ON l.kind = q.kind AND l.item_id = q.item_id
WHERE (cardinality(@kinds::text[]) = 0 OR q.kind = ANY(@kinds::text[]))
  AND (cardinality(@categories::text[]) = 0 OR q.categories && @categories::text[])
  AND (@city::text = '' OR lower(q.city) = lower(@city::text))
  AND (sqlc.narg(since)::timestamptz IS NULL OR q.waiting_since >= sqlc.narg(since)::timestamptz)
  AND (sqlc.narg(until)::timestamptz IS NULL OR q.waiting_since < sqlc.narg(until)::timestamptz)
  AND (@q::text = '' OR q.title ILIKE '%' || @q::text || '%' OR q.item_id = @q::text)
  AND (l.moderator_id IS NULL OR l.expires_at <= @now::timestamptz OR l.moderator_id = @me::bigint)
  AND NOT EXISTS (SELECT 1 FROM review_decisions d WHERE d.state = 'pending' AND d.kind = q.kind AND d.item_id::text = q.item_id);

-- Сколько ждёт по видам (сводка; без учёта аренды).
-- name: QueueByKind :many
SELECT kind, count(*)::int AS total, min(waiting_since)::timestamptz AS oldest FROM review_queue GROUP BY kind ORDER BY kind;

-- Элемент ещё ждёт проверки?
-- name: QueueItem :one
SELECT * FROM review_queue WHERE kind = @kind AND item_id = @item_id;

-- ===== решения (применяются через apply_at) =====

-- name: CreateDecision :one
INSERT INTO review_decisions (kind, item_id, owner_id, moderator_id, action, reason, notes, waiting_since, apply_at)
VALUES (@kind, @item_id, @owner_id, @moderator_id, @action, @reason, @notes, sqlc.narg(waiting_since), @apply_at)
RETURNING *;

-- Отмена: своё, ещё не применено и не взято исполнителем.
-- name: UndoDecision :one
UPDATE review_decisions SET state = 'undone'
WHERE id = @id AND moderator_id = @moderator_id AND state = 'pending' AND apply_at > @now::timestamptz
  AND (lock_until IS NULL OR lock_until < @now::timestamptz)
RETURNING *;

-- Исполнитель берёт одно наступившее решение (несколько серверов — без двойного применения).
-- name: ClaimDueDecision :one
UPDATE review_decisions SET lock_until = @lock_until
WHERE id = (SELECT d.id FROM review_decisions d
            WHERE d.state = 'pending' AND d.apply_at <= @now::timestamptz AND (d.lock_until IS NULL OR d.lock_until < @now::timestamptz)
            ORDER BY d.apply_at FOR UPDATE SKIP LOCKED LIMIT 1)
RETURNING *;

-- name: FinishDecision :exec
UPDATE review_decisions SET state = @state, error = @error, applied_at = sqlc.narg(applied_at), lock_until = NULL
WHERE id = @id AND state = 'pending';

-- name: NextDecisionAt :one
SELECT min(apply_at)::timestamptz FROM review_decisions WHERE state = 'pending';

-- name: GetDecision :one
SELECT * FROM review_decisions WHERE id = @id;

-- История решений по владельцу (бизнес или автор).
-- name: OwnerDecisions :many
SELECT d.*, COALESCE(m.name, '')::text AS moderator_name
FROM review_decisions d LEFT JOIN users m ON m.id = d.moderator_id
WHERE d.owner_id = @owner_id AND d.state IN ('applied', 'pending')
ORDER BY d.created_at DESC LIMIT @lim;

-- ===== свои категории =====

-- name: CategoryUsage :one
SELECT (SELECT count(*) FROM tasks t WHERE @slug::text = ANY(t.categories))::int AS tasks,
       (SELECT count(*) FROM creator_cards c WHERE @slug::text = ANY(c.topics))::int AS cards;

-- Объединить: в заданиях, анкетах и профилях slug заменяется на общий (без повторов, порядок сохраняется).
-- name: ReplaceCategoryInTasks :exec
UPDATE tasks t SET categories = (
    SELECT COALESCE(array_agg(d.x ORDER BY d.n), '{}') FROM (
        SELECT a.x, min(a.n) AS n FROM unnest(array_replace(t.categories, @from_slug::text, @to_slug::text)) WITH ORDINALITY AS a(x, n) GROUP BY a.x
    ) d)
WHERE @from_slug::text = ANY(t.categories);

-- name: ReplaceCategoryInCards :exec
UPDATE creator_cards c SET topics = (
    SELECT COALESCE(array_agg(d.x ORDER BY d.n), '{}') FROM (
        SELECT a.x, min(a.n) AS n FROM unnest(array_replace(c.topics, @from_slug::text, @to_slug::text)) WITH ORDINALITY AS a(x, n) GROUP BY a.x
    ) d)
WHERE @from_slug::text = ANY(c.topics);

-- name: MarkCategoryMerged :one
UPDATE categories SET status = 'merged', merged_into = @into_slug
WHERE id = @id AND status = 'pending_review'
RETURNING *;

-- name: ApprovedCategory :one
SELECT * FROM categories WHERE slug = @slug AND status = 'approved';

-- ===== свои форматы =====

-- name: FormatUsage :one
SELECT count(*)::int AS tasks, COALESCE(min(x.f), '')::text AS title
FROM tasks t, unnest(t.formats) AS x(f)
WHERE replace(lower(x.f), 'ё', 'е') = @norm::text;

-- name: InsertFormatDecision :one
INSERT INTO format_decisions (norm, title, status, merged_into, decided_by)
VALUES (@norm, @title, @status, @merged_into, sqlc.narg(decided_by))
ON CONFLICT (norm) DO NOTHING
RETURNING *;

-- name: GetFormatDecision :one
SELECT * FROM format_decisions WHERE norm = @norm;

-- name: ApprovedFormats :many
SELECT title FROM format_decisions WHERE status = 'approved' ORDER BY title;

-- Объединить формат: в заданиях заменяется на выбранный (без повторов).
-- name: ReplaceFormatInTasks :execrows
UPDATE tasks t SET formats = (
    SELECT COALESCE(array_agg(d.x ORDER BY d.n), '{}') FROM (
        SELECT a.x, min(a.n) AS n FROM unnest(ARRAY(
            SELECT CASE WHEN replace(lower(f), 'ё', 'е') = @norm::text THEN @to_format::text ELSE f END FROM unnest(t.formats) AS f
        )) WITH ORDINALITY AS a(x, n) GROUP BY a.x
    ) d)
WHERE EXISTS (SELECT 1 FROM unnest(t.formats) AS f WHERE replace(lower(f), 'ё', 'е') = @norm::text);

-- ===== жалобы =====

-- Повторная жалоба того же человека на то же (пока открыта) — без новой строки.
-- name: CreateComplaint :one
INSERT INTO complaints (reporter_id, subject, subject_id, target_user_id, reason, comment)
VALUES (@reporter_id, @subject, @subject_id, @target_user_id, @reason, @comment)
ON CONFLICT (reporter_id, subject, subject_id) WHERE status = 'open' DO NOTHING
RETURNING *;

-- name: CountComplaintsSince :one
SELECT count(*)::int FROM complaints WHERE reporter_id = @reporter_id AND created_at > @since;

-- Очередь жалоб: по предмету — сколько, какие причины, когда первая.
-- name: OpenComplaintGroups :many
SELECT c.subject, c.subject_id, c.target_user_id, count(*)::int AS total, min(c.created_at)::timestamptz AS first_at,
       array_agg(DISTINCT c.reason)::text[] AS reasons,
       COALESCE(NULLIF(u.name, ''), u.email, '')::text AS target_name
FROM complaints c JOIN users u ON u.id = c.target_user_id
WHERE c.status = 'open' AND (@subject::text = '' OR c.subject = @subject::text)
GROUP BY c.subject, c.subject_id, c.target_user_id, u.name, u.email
ORDER BY min(c.created_at)
LIMIT @lim OFFSET @off;

-- name: CountOpenComplaintGroups :one
SELECT count(*)::int FROM (SELECT 1 FROM complaints WHERE status = 'open' GROUP BY subject, subject_id) g;

-- name: ComplaintsFor :many
SELECT c.*, COALESCE(NULLIF(u.name, ''), u.email, '')::text AS reporter_name
FROM complaints c JOIN users u ON u.id = c.reporter_id
WHERE c.subject = @subject AND c.subject_id = @subject_id
ORDER BY c.created_at DESC LIMIT 100;

-- name: ResolveComplaints :execrows
UPDATE complaints SET status = @status, action = @action, resolution = @resolution, resolved_by = @resolved_by, resolved_at = @now
WHERE subject = @subject AND subject_id = @subject_id AND status = 'open';

-- name: ComplaintsAgainst :many
SELECT * FROM complaints WHERE target_user_id = @target_user_id ORDER BY created_at DESC LIMIT @lim;

-- ===== пользователи =====

-- Поиск: id, почта или имя (в т. ч. в профилях режимов); фильтры: сотрудники, заблокированные.
-- name: SearchUsers :many
SELECT u.*, COALESCE(b.name, '')::text AS business_name, COALESCE(cp.name, '')::text AS creator_name
FROM users u
LEFT JOIN business_profiles b ON b.user_id = u.id
LEFT JOIN creator_profiles cp ON cp.user_id = u.id
WHERE (@q::text = '' OR u.id::text = @q::text OR u.email ILIKE '%' || @q::text || '%' OR u.name ILIKE '%' || @q::text || '%'
       OR b.name ILIKE '%' || @q::text || '%' OR cp.name ILIKE '%' || @q::text || '%')
  AND (NOT @staff_only::boolean OR u.staff_role IS NOT NULL)
  AND (NOT @blocked_only::boolean OR u.blocked_at IS NOT NULL)
ORDER BY u.created_at DESC, u.id DESC
LIMIT @lim OFFSET @off;

-- name: GetUserFull :one
SELECT u.*, COALESCE(b.name, '')::text AS business_name, COALESCE(cp.name, '')::text AS creator_name
FROM users u
LEFT JOIN business_profiles b ON b.user_id = u.id
LEFT JOIN creator_profiles cp ON cp.user_id = u.id
WHERE u.id = @id;

-- name: BlockUser :one
UPDATE users SET blocked_at = @now, blocked_reason = @reason, updated_at = now()
WHERE id = @id AND blocked_at IS NULL
RETURNING *;

-- name: UnblockUser :one
UPDATE users SET blocked_at = NULL, blocked_reason = '', updated_at = now()
WHERE id = @id AND blocked_at IS NOT NULL
RETURNING *;

-- name: SetStaffRole :one
UPDATE users SET staff_role = sqlc.narg(staff_role), updated_at = now() WHERE id = @id RETURNING *;

-- name: CountAdmins :one
SELECT count(*)::int FROM users WHERE staff_role = 'admin' AND blocked_at IS NULL;

-- name: UserLogins :many
SELECT * FROM login_events WHERE user_id = @user_id ORDER BY created_at DESC LIMIT @lim;

-- name: DeleteSessionsOfUser :execrows
DELETE FROM sessions WHERE user_id = @user_id;

-- ===== сводка =====

-- name: DashboardStats :one
SELECT
    (SELECT count(*) FROM review_decisions WHERE state = 'applied' AND applied_at >= @day_start::timestamptz)::int AS decided_today,
    (SELECT count(*) FROM review_decisions WHERE state = 'applied' AND applied_at >= @week_start::timestamptz)::int AS decided_week,
    (SELECT COALESCE(avg(extract(epoch FROM (created_at - waiting_since))), 0)
       FROM review_decisions WHERE state = 'applied' AND applied_at >= @week_start::timestamptz AND waiting_since IS NOT NULL)::float8 AS avg_review_seconds,
    (SELECT count(*) FROM users WHERE created_at >= @day_start::timestamptz)::int AS users_today,
    (SELECT count(*) FROM users WHERE created_at >= @week_start::timestamptz)::int AS users_week,
    (SELECT count(*) FROM complaints WHERE status = 'open')::int AS open_complaints;

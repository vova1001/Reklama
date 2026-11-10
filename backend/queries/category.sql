-- Справочник категорий и свои категории пользователей.

-- name: ListCommonCategories :many
SELECT * FROM categories WHERE status = 'approved' AND is_active ORDER BY position, id;

-- name: ListAuthorCategories :many
SELECT * FROM categories WHERE author_id = @author_id AND status <> 'approved' ORDER BY created_at, id;

-- name: FindCommonCategory :one
SELECT * FROM categories WHERE norm = @norm AND status = 'approved' AND is_active;

-- name: FindAuthorCategory :one
SELECT * FROM categories WHERE author_id = @author_id AND norm = @norm AND status = 'pending_review';

-- name: CountAuthorPendingCategories :one
SELECT count(*)::int FROM categories WHERE author_id = @author_id AND status = 'pending_review';

-- name: CreateCustomCategory :one
INSERT INTO categories (slug, title, icon, position, is_active, status, author_id, norm)
VALUES (@slug, @title, 'tag', 1000, false, 'pending_review', @author_id, @norm)
RETURNING *;

-- Сколько тем годится пользователю (общие или его свои на проверке) и сколько из них — свои.
-- name: UsableCategories :one
SELECT count(*)::int AS usable, count(*) FILTER (WHERE status <> 'approved')::int AS custom
FROM categories
WHERE slug = ANY(@slugs::text[])
  AND ((status = 'approved' AND is_active) OR (author_id = @user_id AND status = 'pending_review'));

-- name: GetCategory :one
SELECT * FROM categories WHERE id = @id;

-- Решение модератора: одобренная становится общей (видна всем, на главной — в конце списка).
-- name: SetCategoryStatus :one
UPDATE categories SET status = @to_status::text, is_active = (@to_status::text = 'approved')
WHERE id = @id AND status = @from_status::text
RETURNING *;

-- name: LogCategoryChange :exec
INSERT INTO moderation_log (subject, subject_id, from_status, to_status, reason, actor_id)
VALUES ('category', @subject_id, @from_status, @to_status, @reason, sqlc.narg(actor_id));

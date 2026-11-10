-- Режимы и профили режимов (бизнес / автор).

-- name: SetUserMode :one
UPDATE users SET active_mode = $2, updated_at = now() WHERE id = $1
RETURNING *;

-- name: GetBusinessProfile :one
SELECT * FROM business_profiles WHERE user_id = $1;

-- name: GetCreatorProfile :one
SELECT * FROM creator_profiles WHERE user_id = $1;

-- name: UpsertBusinessProfile :one
INSERT INTO business_profiles (user_id, name, city, spheres)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET name = EXCLUDED.name, city = EXCLUDED.city, spheres = EXCLUDED.spheres, updated_at = now()
RETURNING *;

-- name: UpsertCreatorProfile :one
INSERT INTO creator_profiles (user_id, name, city, spheres, channels)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id) DO UPDATE
SET name = EXCLUDED.name, city = EXCLUDED.city, spheres = EXCLUDED.spheres, channels = EXCLUDED.channels, updated_at = now()
RETURNING *;

-- name: CountActiveCategories :one
SELECT count(*)::int FROM categories WHERE is_active AND slug = ANY(sqlc.arg(slugs)::text[]);

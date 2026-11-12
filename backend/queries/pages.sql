-- Публичные страницы: витрина бизнеса и профиль автора (одна каждого вида на пользователя), адреса, проверка.

-- name: GetPage :one
SELECT * FROM profile_pages WHERE user_id = @user_id AND kind = @kind;

-- name: GetPageByID :one
SELECT * FROM profile_pages WHERE id = @id;

-- name: CreatePage :one
INSERT INTO profile_pages (user_id, kind, slug, title, city, spheres, logo, cover, blocks, media)
VALUES (@user_id, @kind, @slug, @title, @city, @spheres, sqlc.narg(logo), sqlc.narg(cover), @blocks, @media)
RETURNING *;

-- Занять адрес за страницей: свободный — занимается, свой прежний — возвращается; чужой — нет строки.
-- name: ClaimPageSlug :one
INSERT INTO page_slugs (kind, slug, page_id) VALUES (@kind, @slug, @page_id)
ON CONFLICT (kind, slug) DO UPDATE SET created_at = now() WHERE page_slugs.page_id = EXCLUDED.page_id
RETURNING page_id;

-- name: CountPageSlugs :one
SELECT count(*)::int FROM page_slugs WHERE page_id = @page_id;

-- name: PageSlugTaken :one
SELECT EXISTS (SELECT 1 FROM page_slugs WHERE kind = @kind AND slug = @slug)::boolean;

-- name: SetPageSlug :one
UPDATE profile_pages SET slug = @slug, updated_at = now() WHERE id = @id RETURNING *;

-- Содержимое — только в черновике, отклонённой и возвращённой на доработку.
-- name: UpdatePageContent :one
UPDATE profile_pages
SET title = @title, city = @city, spheres = @spheres, logo = sqlc.narg(logo), cover = sqlc.narg(cover), blocks = @blocks,
    media = @media, updated_at = now()
WHERE id = @id AND status IN ('draft', 'rejected', 'changes_requested')
RETURNING *;

-- name: SetPageStatus :one
UPDATE profile_pages
SET status = @to_status::text, reject_reason = CASE WHEN @to_status::text = 'rejected' THEN @reason::text ELSE '' END, updated_at = now(),
    review_notes = CASE WHEN @to_status::text = 'changes_requested' THEN @notes::jsonb ELSE '{}'::jsonb END,
    revision = CASE WHEN @to_status::text = 'published' THEN revision ELSE NULL END,
    revision_status = CASE WHEN @to_status::text = 'published' THEN revision_status ELSE NULL END,
    revision_reason = CASE WHEN @to_status::text = 'published' THEN revision_reason ELSE '' END,
    submitted_at = CASE WHEN @to_status::text = 'pending_review' THEN now() ELSE submitted_at END,
    published_at = CASE WHEN @to_status::text = 'published' THEN now() ELSE published_at END
WHERE id = @id AND status = @from_status::text
RETURNING *;

-- ===== правка опубликованной страницы =====

-- name: SavePageRevision :one
UPDATE profile_pages SET revision = @revision, revision_status = 'draft', revision_reason = '', updated_at = now()
WHERE id = @id AND status = 'published'
  AND (revision_status IS NULL OR revision_status IN ('draft', 'rejected', 'changes_requested'))
RETURNING *;

-- name: SetPageRevisionStatus :one
UPDATE profile_pages
SET revision_status = @to_status::text,
    revision_reason = CASE WHEN @to_status::text = 'rejected' THEN @reason::text ELSE '' END,
    review_notes = CASE WHEN @to_status::text = 'changes_requested' THEN @notes::jsonb ELSE '{}'::jsonb END,
    revision_submitted_at = CASE WHEN @to_status::text = 'pending_review' THEN now() ELSE revision_submitted_at END,
    updated_at = now()
WHERE id = @id AND status = 'published' AND revision_status = @from_status::text
RETURNING *;

-- name: DropPageRevision :one
UPDATE profile_pages SET revision = NULL, revision_status = NULL, revision_reason = '', revision_submitted_at = NULL,
                         review_notes = '{}', updated_at = now()
WHERE id = @id AND status = 'published' AND revision IS NOT NULL
RETURNING *;

-- Заменить опубликованное содержимое правкой — только из ожидаемого статуса и статуса правки.
-- name: ReplacePageContent :one
UPDATE profile_pages
SET title = @title, city = @city, spheres = @spheres, logo = sqlc.narg(logo), cover = sqlc.narg(cover), blocks = @blocks,
    media = @media, revision = NULL, revision_status = NULL, revision_reason = '', revision_submitted_at = NULL,
    review_notes = '{}', updated_at = now()
WHERE id = @id AND status = @status::text AND revision_status = @revision_status::text
RETURNING *;

-- ===== публичная страница =====

-- Опубликованная страница по адресу (нынешнему или прежнему) с названиями сфер; владелец не заблокирован.
-- name: GetPublishedPageBySlug :one
SELECT p.*, ARRAY(SELECT c.title FROM unnest(p.spheres) WITH ORDINALITY AS x(slug, n)
                  JOIN categories c ON c.slug = x.slug ORDER BY x.n)::text[] AS sphere_titles
FROM page_slugs s
JOIN profile_pages p ON p.id = s.page_id
JOIN users u ON u.id = p.user_id
WHERE s.kind = @kind AND s.slug = @slug AND p.status = 'published' AND u.blocked_at IS NULL;

-- Адреса опубликованных страниц пользователей (ссылки из ленты и со страницы задания).
-- name: PublishedPageSlugs :many
SELECT p.user_id, p.slug FROM profile_pages p JOIN users u ON u.id = p.user_id
WHERE p.kind = @kind AND p.user_id = ANY(@user_ids::bigint[]) AND p.status = 'published' AND u.blocked_at IS NULL;

-- Открытые задания бизнеса (на его странице): опубликованные, срок не прошёл, новые сверху.
-- name: ListOwnerOpenTasks :many
SELECT t.*, ARRAY(SELECT c.title FROM unnest(t.categories) WITH ORDINALITY AS x(slug, n)
                  JOIN categories c ON c.slug = x.slug ORDER BY x.n)::text[] AS category_titles
FROM tasks t
WHERE t.owner_id = @owner_id AND t.status = 'published' AND (t.deadline_at IS NULL OR t.deadline_at > now())
ORDER BY t.published_at DESC, t.id DESC
LIMIT @lim;

-- Названия сфер и тем по slug (блок «Что могу рекламировать»).
-- name: CategoryTitlesBySlug :many
SELECT slug, title FROM categories WHERE slug = ANY(@slugs::text[]);

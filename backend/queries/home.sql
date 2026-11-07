-- name: ListActiveCategories :many
SELECT slug, title, icon
FROM categories
WHERE is_active
ORDER BY position, id;

-- name: ListActivePlans :many
SELECT id, audience, slug, name, subtitle, price_kopecks, period, recommended
FROM plans
WHERE is_active
ORDER BY audience, position, id;

-- name: ListPlanFeatures :many
SELECT f.plan_id, f.text
FROM plan_features f
JOIN plans p ON p.id = f.plan_id
WHERE p.is_active
ORDER BY f.plan_id, f.position;

-- name: ListPublishedFAQ :many
SELECT id, question, answer
FROM faq
WHERE is_published
ORDER BY position, id;

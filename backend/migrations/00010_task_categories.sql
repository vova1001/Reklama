-- +goose Up
-- Сфер у задания несколько (до 5): category → categories text[] (slug по порядку выбора; проверяет сервис —
-- общая или своя автора). Форматов — до 3: из списка (slug) или свои (название, нормализует сервис).
-- Отбор ленты по сферам — пересечение массивов (GIN-индекс по опубликованным).
ALTER TABLE tasks ADD COLUMN categories text[] NOT NULL DEFAULT '{}';
UPDATE tasks SET categories = ARRAY[category] WHERE category IS NOT NULL;
ALTER TABLE tasks DROP COLUMN category;
ALTER TABLE tasks ADD CONSTRAINT tasks_categories_max CHECK (cardinality(categories) <= 5);
ALTER TABLE tasks ADD CONSTRAINT tasks_formats_max CHECK (cardinality(formats) <= 6);
CREATE INDEX tasks_categories_idx ON tasks USING gin (categories) WHERE status = 'published';
CREATE INDEX tasks_formats_idx ON tasks USING gin (formats) WHERE status = 'published';
CREATE INDEX creator_cards_topics_idx ON creator_cards USING gin (topics) WHERE status = 'published';

-- +goose Down
DROP INDEX creator_cards_topics_idx;
DROP INDEX tasks_formats_idx;
DROP INDEX tasks_categories_idx;
ALTER TABLE tasks DROP CONSTRAINT tasks_formats_max;
ALTER TABLE tasks DROP CONSTRAINT tasks_categories_max;
ALTER TABLE tasks ADD COLUMN category text REFERENCES categories (slug);
UPDATE tasks t SET category = t.categories[1]
WHERE cardinality(t.categories) > 0 AND EXISTS (SELECT 1 FROM categories c WHERE c.slug = t.categories[1]);
ALTER TABLE tasks DROP COLUMN categories;

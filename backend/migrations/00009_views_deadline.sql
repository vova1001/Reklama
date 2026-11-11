-- +goose Up
-- Условие выполнения — только просмотры (лайки убраны). Срок публикации — дата и, по желанию, время (по Москве);
-- deadline_at — тот же срок в UTC (считает сервис). Опубликованные с прошедшим сроком фоновая задача переводит
-- в «завершено» (expired) — из ленты они пропадают сразу (лента отбирает deadline_at > now()).
ALTER TABLE tasks DROP COLUMN min_likes;
ALTER TABLE tasks ADD COLUMN publish_time time;
ALTER TABLE tasks ADD COLUMN deadline_at timestamptz;
UPDATE tasks SET deadline_at = ((publish_by + time '23:59:59') - interval '3 hours') AT TIME ZONE 'UTC' WHERE publish_by IS NOT NULL;
ALTER TABLE tasks DROP CONSTRAINT tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check CHECK (status IN ('draft', 'pending_review', 'published', 'rejected', 'expired'));
CREATE INDEX tasks_deadline_idx ON tasks (deadline_at) WHERE status = 'published';

-- +goose Down
DROP INDEX tasks_deadline_idx;
UPDATE tasks SET status = 'draft' WHERE status = 'expired';
ALTER TABLE tasks DROP CONSTRAINT tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check CHECK (status IN ('draft', 'pending_review', 'published', 'rejected'));
ALTER TABLE tasks DROP COLUMN deadline_at;
ALTER TABLE tasks DROP COLUMN publish_time;
ALTER TABLE tasks ADD COLUMN min_likes bigint NOT NULL DEFAULT 0 CHECK (min_likes >= 0);

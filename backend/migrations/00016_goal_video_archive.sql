-- +goose Up
-- Задача 16: цель задания (goal: просмотры/подписчики/переходы/гости — единица условия выполнения),
-- видео-примеры у задания (videos, до 2), статус «архив» (владелец убирает сам, обратно — в черновик).
ALTER TABLE tasks DROP CONSTRAINT tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check
    CHECK (status IN ('draft', 'pending_review', 'published', 'rejected', 'expired', 'changes_requested', 'archived'));

ALTER TABLE tasks ADD COLUMN goal text NOT NULL DEFAULT 'views'
    CHECK (goal IN ('views', 'subscribers', 'clicks', 'guests'));

ALTER TABLE tasks ADD COLUMN videos text[] NOT NULL DEFAULT '{}';
ALTER TABLE tasks ADD CONSTRAINT tasks_videos_max CHECK (cardinality(videos) <= 2);

-- форматов теперь без видимого предела (защитный — в сервисе)
ALTER TABLE tasks DROP CONSTRAINT tasks_formats_max;

ALTER TABLE media DROP CONSTRAINT media_purpose_check;
ALTER TABLE media ADD CONSTRAINT media_purpose_check CHECK (purpose IN ('avatar', 'task_cover', 'task_photo', 'card_avatar',
    'card_work', 'page_logo', 'page_cover', 'page_photo', 'page_video', 'video_poster', 'task_video'));

-- +goose Down
ALTER TABLE tasks ADD CONSTRAINT tasks_formats_max CHECK (cardinality(formats) <= 6);

ALTER TABLE media DROP CONSTRAINT media_purpose_check;
ALTER TABLE media ADD CONSTRAINT media_purpose_check CHECK (purpose IN ('avatar', 'task_cover', 'task_photo', 'card_avatar',
    'card_work', 'page_logo', 'page_cover', 'page_photo', 'page_video', 'video_poster'));

ALTER TABLE tasks DROP CONSTRAINT tasks_videos_max;
ALTER TABLE tasks DROP COLUMN videos;
ALTER TABLE tasks DROP COLUMN goal;

UPDATE tasks SET status = 'draft' WHERE status = 'archived';
ALTER TABLE tasks DROP CONSTRAINT tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check
    CHECK (status IN ('draft', 'pending_review', 'published', 'rejected', 'expired', 'changes_requested'));

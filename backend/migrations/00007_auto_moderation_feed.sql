-- +goose Up
-- Автоматическая проверка (MODERATION_MODE=auto): в журнале — пометка «решила система».
-- Лента анкет для бизнеса: курсор (published_at, user_id), отбор по теме и городу.
ALTER TABLE moderation_log ADD COLUMN auto boolean NOT NULL DEFAULT false;
DROP INDEX creator_cards_published_idx;
CREATE INDEX creator_cards_feed_idx ON creator_cards (published_at DESC, user_id DESC) WHERE status = 'published';

-- +goose Down
DROP INDEX creator_cards_feed_idx;
CREATE INDEX creator_cards_published_idx ON creator_cards (published_at DESC) WHERE status = 'published';
ALTER TABLE moderation_log DROP COLUMN auto;

-- +goose Up
ALTER TABLE tasks ADD COLUMN details jsonb NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE tasks DROP COLUMN details;

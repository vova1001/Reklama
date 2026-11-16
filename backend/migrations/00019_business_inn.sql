-- +goose Up
ALTER TABLE business_profiles ADD COLUMN inn text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE business_profiles DROP COLUMN inn;

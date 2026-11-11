-- +goose Up
-- Состояние формы редактора (задание, анкета) — запасное хранилище черновиков, когда Redis не задан или недоступен.
-- data — как есть от формы (JSON-объект, до DRAFT_MAX_KB); живёт 7 дней с последнего сохранения.
CREATE TABLE form_drafts (
    user_id  bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    form     text   NOT NULL,
    data     jsonb  NOT NULL,
    saved_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, form)
);
CREATE INDEX form_drafts_saved_idx ON form_drafts (saved_at);

-- +goose Down
DROP TABLE form_drafts;

-- +goose Up
-- Уведомления в приложении (задача 15): владелец видит в кабинете решение модератора — опубликовано, отклонено
-- (причина) или на доработку (замечания) — и ссылку на задание, анкету или страницу. Запись создаётся в той же
-- транзакции, что и итог решения (review_decisions.state → applied); UNIQUE(decision_id) — ровно одно уведомление
-- на решение, без гонок при повторах исполнителя.

CREATE TABLE notifications (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    decision_id bigint NOT NULL REFERENCES review_decisions (id) ON DELETE CASCADE,
    kind        text   NOT NULL, -- review kind: task, task_revision, card, card_revision, page, page_revision
    item_id     bigint NOT NULL,
    action      text   NOT NULL CHECK (action IN ('approve', 'reject', 'rework')),
    title       text   NOT NULL DEFAULT '', -- название задания, анкеты или страницы
    reason      text   NOT NULL DEFAULT '', -- причина отказа / сводка замечаний
    path        text   NOT NULL DEFAULT '', -- где посмотреть: /app/tasks/12, /app/card, /app/page?kind=...
    read_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX notifications_decision_uq ON notifications (decision_id);
CREATE INDEX notifications_user_created_idx ON notifications (user_id, created_at DESC);

-- +goose Down
DROP TABLE notifications;

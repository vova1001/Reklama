-- +goose Up
-- Задача 17: цель «бартер» (автор получает товар/услугу вместо денег — оплата и число не нужны),
-- ссылка или промокод у задания (link — обязательна для цели «переходы», видна авторам).
ALTER TABLE tasks DROP CONSTRAINT tasks_goal_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_goal_check CHECK (goal IN ('views', 'subscribers', 'clicks', 'guests', 'barter'));

ALTER TABLE tasks ADD COLUMN link text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE tasks DROP COLUMN link;

UPDATE tasks SET goal = 'views' WHERE goal = 'barter';
ALTER TABLE tasks DROP CONSTRAINT tasks_goal_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_goal_check CHECK (goal IN ('views', 'subscribers', 'clicks', 'guests'));

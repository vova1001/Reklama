-- Состояние форм редактора (запасное хранилище черновиков, основное — Redis).

-- name: UpsertFormDraft :exec
INSERT INTO form_drafts (user_id, form, data, saved_at) VALUES (@user_id, @form, @data, @saved_at)
ON CONFLICT (user_id, form) DO UPDATE SET data = EXCLUDED.data, saved_at = EXCLUDED.saved_at;

-- name: GetFormDraft :one
SELECT form, data, saved_at FROM form_drafts WHERE user_id = @user_id AND form = @form AND saved_at > @since;

-- name: DeleteFormDraft :exec
DELETE FROM form_drafts WHERE user_id = @user_id AND form = @form;

-- name: DeleteOldFormDrafts :execrows
DELETE FROM form_drafts WHERE saved_at < @before;

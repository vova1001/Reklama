package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// ===== PostgreSQL — запасное хранилище состояний форм =====

// DraftsPG — состояния форм в таблице form_drafts. Срок жизни — по saved_at (старше ttl не читаются, чистятся фоново).
type DraftsPG struct{ q *store.Queries }

// NewDraftsPG создаёт хранилище черновиков форм в PostgreSQL.
func NewDraftsPG(db DB) *DraftsPG { return &DraftsPG{q: store.New(db)} }

// GetDraft — состояние формы не старше ttl последнего сохранения (7 дней); нет — ErrNotFound.
func (r *DraftsPG) GetDraft(ctx context.Context, userID int64, form string) (domain.FormDraft, error) {
	row, err := r.q.GetFormDraft(ctx, store.GetFormDraftParams{UserID: userID, Form: form, Since: ts(time.Now().Add(-draftTTL))})
	if err != nil {
		return domain.FormDraft{}, dbErr("черновик формы", err)
	}
	return domain.FormDraft{Form: row.Form, Data: row.Data, SavedAt: row.SavedAt.Time}, nil
}

// draftTTL — сколько читать запасные копии (совпадает со сроком в Redis по умолчанию).
const draftTTL = 7 * 24 * time.Hour

// PutDraft — сохранить (заменить) состояние формы.
func (r *DraftsPG) PutDraft(ctx context.Context, userID int64, d domain.FormDraft, _ time.Duration) error {
	return dbErr("сохранение черновика формы", r.q.UpsertFormDraft(ctx, store.UpsertFormDraftParams{UserID: userID, Form: d.Form, Data: d.Data, SavedAt: ts(d.SavedAt)}))
}

// DeleteDraft — убрать состояние формы.
func (r *DraftsPG) DeleteDraft(ctx context.Context, userID int64, form string) error {
	return dbErr("удаление черновика формы", r.q.DeleteFormDraft(ctx, store.DeleteFormDraftParams{UserID: userID, Form: form}))
}

// Cleanup — удалить состояния старше before; сколько удалено.
func (r *DraftsPG) Cleanup(ctx context.Context, before time.Time) (int64, error) {
	n, err := r.q.DeleteOldFormDrafts(ctx, ts(before))
	return n, dbErr("очистка черновиков форм", err)
}

// ===== Redis — основное хранилище =====

// DraftsRedis — состояния форм в Redis: ключ ohvat:draft:<user>:<form>, значение — {data, savedAt}, срок — TTL.
type DraftsRedis struct{ rdb redis.Cmdable }

// NewDraftsRedis создаёт хранилище черновиков форм в Redis.
func NewDraftsRedis(rdb redis.Cmdable) *DraftsRedis { return &DraftsRedis{rdb: rdb} }

type redisDraft struct {
	Data    json.RawMessage `json:"data"`
	SavedAt time.Time       `json:"savedAt"`
}

func draftKey(userID int64, form string) string {
	return "ohvat:draft:" + strconv.FormatInt(userID, 10) + ":" + form
}

// GetDraft — состояние формы; нет или истекло — ErrNotFound.
func (r *DraftsRedis) GetDraft(ctx context.Context, userID int64, form string) (domain.FormDraft, error) {
	raw, err := r.rdb.Get(ctx, draftKey(userID, form)).Bytes()
	if errors.Is(err, redis.Nil) {
		return domain.FormDraft{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.FormDraft{}, fmt.Errorf("черновик формы в Redis: %w", err)
	}
	var v redisDraft
	if err := json.Unmarshal(raw, &v); err != nil {
		return domain.FormDraft{}, fmt.Errorf("черновик формы из Redis: %w", err)
	}
	return domain.FormDraft{Form: form, Data: v.Data, SavedAt: v.SavedAt}, nil
}

// PutDraft — сохранить состояние формы на ttl.
func (r *DraftsRedis) PutDraft(ctx context.Context, userID int64, d domain.FormDraft, ttl time.Duration) error {
	raw, err := json.Marshal(redisDraft{Data: d.Data, SavedAt: d.SavedAt})
	if err != nil {
		return fmt.Errorf("черновик формы в JSON: %w", err)
	}
	if err := r.rdb.Set(ctx, draftKey(userID, d.Form), raw, ttl).Err(); err != nil {
		return fmt.Errorf("сохранение черновика формы в Redis: %w", err)
	}
	return nil
}

// DeleteDraft — убрать состояние формы.
func (r *DraftsRedis) DeleteDraft(ctx context.Context, userID int64, form string) error {
	if err := r.rdb.Del(ctx, draftKey(userID, form)).Err(); err != nil {
		return fmt.Errorf("удаление черновика формы в Redis: %w", err)
	}
	return nil
}

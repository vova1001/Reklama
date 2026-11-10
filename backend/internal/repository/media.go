package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// Media — метаданные фото, права на просмотр, квоты и очистка. SQL — только в queries/media.sql.
type Media struct {
	db DB
	q  *store.Queries
}

// NewMedia создаёт репозиторий фото.
func NewMedia(db DB) *Media { return &Media{db: db, q: store.New(db)} }

// CreateMedia — запись о загруженном фото (файлы уже в хранилище).
func (r *Media) CreateMedia(ctx context.Context, m domain.Media) (domain.Media, error) {
	row, err := r.q.CreateMedia(ctx, store.CreateMediaParams{
		ID: m.ID, OwnerID: m.OwnerID, Purpose: m.Purpose, Width: int32(m.Width), Height: int32(m.Height), Bytes: m.Bytes,
	})
	return mediaOf(row), dbErr("новое фото", err)
}

// OwnMedia — свои фото из списка (чужих и несуществующих в ответе нет).
func (r *Media) OwnMedia(ctx context.Context, ownerID int64, ids []string) ([]domain.Media, error) {
	rows, err := r.q.GetOwnMedia(ctx, store.GetOwnMediaParams{OwnerID: ownerID, Ids: nonNil(ids)})
	if err != nil {
		return nil, dbErr("свои фото", err)
	}
	out := make([]domain.Media, 0, len(rows))
	for _, m := range rows {
		out = append(out, mediaOf(m))
	}
	return out, nil
}

// Usage — сколько фото у пользователя и сколько они занимают.
func (r *Media) Usage(ctx context.Context, ownerID int64) (int, int64, error) {
	u, err := r.q.MediaUsage(ctx, ownerID)
	return int(u.Files), u.Bytes, dbErr("квота фото", err)
}

// Access — владелец и видно ли фото всем; нет — ErrNotFound.
func (r *Media) Access(ctx context.Context, id string) (domain.MediaAccess, error) {
	a, err := r.q.MediaAccess(ctx, id)
	return domain.MediaAccess{ID: a.ID, OwnerID: a.OwnerID, Public: a.Public}, dbErr("доступ к фото", err)
}

// DeleteLoose — удалить своё неприкреплённое фото; нет или прикреплено — ErrNotFound.
func (r *Media) DeleteLoose(ctx context.Context, ownerID int64, id string) error {
	n, err := r.q.DeleteOwnLooseMedia(ctx, store.DeleteOwnLooseMediaParams{ID: id, OwnerID: ownerID})
	if err != nil {
		return dbErr("удаление фото", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SetAvatar — аватар профиля: фото прикрепляется к пользователю (прежнее откреплено), адрес — в профиле.
// id "" — убрать аватар. Чужое или не аватар — ErrConflict.
func (r *Media) SetAvatar(ctx context.Context, userID int64, id, url string) error {
	err := inTx(ctx, r.db, r.q, func(q *store.Queries) error {
		if id != "" {
			n, err := q.AttachAvatar(ctx, store.AttachAvatarParams{ID: id, OwnerID: pgInt8(userID)})
			if err != nil {
				return err
			}
			if n == 0 {
				return domain.ErrConflict
			}
		}
		if err := q.DetachAvatars(ctx, store.DetachAvatarsParams{OwnerID: pgInt8(userID), Keep: id}); err != nil {
			return err
		}
		return q.SetUserAvatar(ctx, store.SetUserAvatarParams{ID: userID, AvatarUrl: url})
	})
	if errors.Is(err, domain.ErrConflict) {
		return err
	}
	return dbErr("аватар", err)
}

// Orphans — неприкреплённые дольше срока (к удалению), не больше limit.
func (r *Media) Orphans(ctx context.Context, before time.Time, limit int) ([]string, error) {
	ids, err := r.q.ListOrphanMedia(ctx, store.ListOrphanMediaParams{Before: ts(before), Lim: int32(limit)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return ids, dbErr("сироты", err)
}

// DeleteMedia — удалить записи (только неприкреплённые: за время очистки фото могли прикрепить).
func (r *Media) DeleteMedia(ctx context.Context, ids []string) error {
	return dbErr("удаление записей фото", r.q.DeleteMedia(ctx, nonNil(ids)))
}

func mediaOf(m store.Medium) domain.Media {
	return domain.Media{
		ID: m.ID, OwnerID: m.OwnerID, Purpose: m.Purpose, Width: int(m.Width), Height: int(m.Height), Bytes: m.Bytes,
		Subject: m.Subject.String, SubjectID: m.SubjectID.Int64, CreatedAt: m.CreatedAt.Time,
	}
}

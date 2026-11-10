// Package storage — хранилища файлов за интерфейсом service.BlobStore: PostgreSQL (хостинг без диска) и
// S3-совместимое (Cloudflare R2, Яндекс Object Storage, MinIO). Выбор — настройкой MEDIA_STORE, без правки кода.
package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
	"ohvat/backend/internal/store"
)

// Postgres — файлы в таблице media_blobs (bytea).
type Postgres struct{ q *store.Queries }

// NewPostgres создаёт хранилище в PostgreSQL.
func NewPostgres(db store.DBTX) *Postgres { return &Postgres{q: store.New(db)} }

// Put — сохранить файл (повтор с тем же ключом — перезапись).
func (p *Postgres) Put(ctx context.Context, key, contentType string, data []byte) error {
	return p.q.PutBlob(ctx, store.PutBlobParams{Key: key, ContentType: contentType, Data: data})
}

// Get — файл; нет — domain.ErrNotFound.
func (p *Postgres) Get(ctx context.Context, key string) (service.Blob, error) {
	b, err := p.q.GetBlob(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.Blob{}, domain.ErrNotFound
	}
	if err != nil {
		return service.Blob{}, err
	}
	return service.Blob{ContentType: b.ContentType, Data: b.Data, ModTime: b.CreatedAt.Time}, nil
}

// Delete — удалить файлы (нет — не ошибка).
func (p *Postgres) Delete(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	return p.q.DeleteBlobs(ctx, keys)
}

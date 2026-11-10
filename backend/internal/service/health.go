package service

import "context"

// Pinger — проверка живости хранилища.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Health — проверка живости сервера и базы для /healthz.
type Health struct{ db Pinger }

// NewHealth создаёт проверку живости.
func NewHealth(db Pinger) *Health { return &Health{db: db} }

// Check — база отвечает.
func (h *Health) Check(ctx context.Context) error { return h.db.Ping(ctx) }

package repository

import "context"

// Pinger — подключение, которое умеет проверять себя (pgxpool.Pool).
type Pinger interface {
	Ping(ctx context.Context) error
}

// Health — живость базы.
type Health struct{ db Pinger }

// NewHealth создаёт проверку базы.
func NewHealth(db Pinger) *Health { return &Health{db: db} }

// Ping — база отвечает.
func (h *Health) Ping(ctx context.Context) error { return h.db.Ping(ctx) }

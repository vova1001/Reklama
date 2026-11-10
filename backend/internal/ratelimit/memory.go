package ratelimit

import (
	"context"
	"math"
	"sync"
	"time"
)

// Memory — токен-бакеты в памяти процесса. Годится для одного сервера; старые ключи чистит Run.
type Memory struct {
	mu      sync.Mutex
	rate    Rate
	buckets map[string]*bucket
	now     func() time.Time
	idle    time.Duration
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewMemory создаёт ограничитель в памяти.
func NewMemory(r Rate) *Memory {
	r = r.normalized()
	idle := r.idleAfter()
	if idle < 10*time.Minute {
		idle = 10 * time.Minute
	}
	return &Memory{rate: r, buckets: map[string]*bucket{}, now: time.Now, idle: idle}
}

// Allow тратит токен ключа.
func (m *Memory) Allow(_ context.Context, key string) (bool, time.Duration, error) {
	ok, wait := m.take(key)
	return ok, wait, nil
}

func (m *Memory) take(key string) (bool, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	burst := float64(m.rate.Burst)
	b, ok := m.buckets[key]
	if !ok {
		b = &bucket{tokens: burst, last: now}
		m.buckets[key] = b
	}
	if el := now.Sub(b.last).Seconds(); el > 0 {
		b.tokens = math.Min(burst, b.tokens+el*m.rate.RPS)
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / m.rate.RPS * float64(time.Second))
}

// Sweep удаляет бакеты, к которым давно не обращались (они всё равно полные). Возвращает, сколько удалено.
func (m *Memory) Sweep() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	n := 0
	for k, b := range m.buckets {
		if now.Sub(b.last) > m.idle {
			delete(m.buckets, k)
			n++
		}
	}
	return n
}

// Size — сколько ключей сейчас в памяти.
func (m *Memory) Size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.buckets)
}

// Run чистит старые ключи раз в every до отмены ctx.
func (m *Memory) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Sweep()
		}
	}
}

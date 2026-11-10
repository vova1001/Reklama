// Package ratelimit — ограничение частоты запросов: токен-бакет по ключу (адрес, пользователь, вебхук).
// Реализации за одним интерфейсом Limiter: Memory — в памяти процесса (один сервер), Redis — общий бакет
// для нескольких серверов (REDIS_URL), Fallback — Redis с запасным Memory, если Redis недоступен.
package ratelimit

import (
	"context"
	"time"
)

// Limiter — тратит токен ключа. false — токенов нет, retryAfter — когда появится следующий.
// Ошибка — сбой хранилища лимитов (вызывающий решает: пропустить или отказать).
type Limiter interface {
	Allow(ctx context.Context, key string) (ok bool, retryAfter time.Duration, err error)
}

// Rate — правило бакета: RPS токенов в секунду, запас Burst подряд.
type Rate struct {
	RPS   float64
	Burst int
}

func (r Rate) normalized() Rate {
	if r.RPS <= 0 {
		r.RPS = 1
	}
	if r.Burst < 1 {
		r.Burst = 1
	}
	return r
}

// idleAfter — через сколько неиспользуемый бакет снова полон и его можно забыть.
func (r Rate) idleAfter() time.Duration {
	d := time.Duration(float64(r.Burst) / r.RPS * float64(time.Second))
	return d + time.Second
}

package ratelimit

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// Fallback — основной ограничитель (Redis) с запасным (Memory): сбой основного не открывает и не закрывает
// сайт — лимит временно считается в памяти процесса. О сбое пишет в журнал не чаще раза в минуту.
type Fallback struct {
	Primary   Limiter
	Secondary Limiter
	Log       *slog.Logger
	lastWarn  atomic.Int64
}

// Allow — решение основного ограничителя, при его сбое — запасного.
func (f *Fallback) Allow(ctx context.Context, key string) (bool, time.Duration, error) {
	ok, wait, err := f.Primary.Allow(ctx, key)
	if err == nil {
		return ok, wait, nil
	}
	if now := time.Now().Unix(); now-f.lastWarn.Load() >= 60 {
		f.lastWarn.Store(now)
		log := f.Log
		if log == nil {
			log = slog.Default()
		}
		log.WarnContext(ctx, "лимиты: основное хранилище недоступно, считаю в памяти", "err", err)
	}
	return f.Secondary.Allow(ctx, key)
}

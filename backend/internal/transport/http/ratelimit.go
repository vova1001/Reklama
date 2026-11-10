package httpapi

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimiter — токен-бакет на адрес клиента: rps токенов в секунду, запас burst.
// Хранится в памяти процесса (один сервер); старые ключи чистит Run. Для нескольких серверов — Redis.
type RateLimiter struct {
	mu      sync.Mutex
	rps     float64
	burst   float64
	buckets map[string]*tokenBucket
	now     func() time.Time
	idle    time.Duration
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter создаёт ограничитель.
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{rps: rps, burst: float64(burst), buckets: map[string]*tokenBucket{}, now: time.Now, idle: 10 * time.Minute}
}

// Allow тратит токен; нет токена — false и через сколько появится.
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rps)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rps * float64(time.Second))
	return false, wait
}

// sweep удаляет бакеты, к которым давно не обращались (они всё равно полные).
func (l *RateLimiter) sweep() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	n := 0
	for k, b := range l.buckets {
		if now.Sub(b.last) > l.idle {
			delete(l.buckets, k)
			n++
		}
	}
	return n
}

// Size — сколько адресов сейчас в памяти.
func (l *RateLimiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// Run чистит старые ключи раз в every до отмены ctx.
func (l *RateLimiter) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.sweep()
		}
	}
}

// Middleware отвечает 429 с Retry-After, когда у адреса кончились токены.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := l.Allow(clientIP(r)); !ok {
			sec := int(math.Ceil(wait.Seconds()))
			if sec < 1 {
				sec = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(sec))
			writeJSON(w, http.StatusTooManyRequests, errorDTO{Error: errorBody{
				Code: "rate_limited", Message: "Слишком много попыток подряд. Подождите немного и попробуйте снова.", RetryAfter: sec,
			}})
			return
		}
		next.ServeHTTP(w, r)
	})
}

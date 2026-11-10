package httpapi

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"
)

// Limiter — ограничитель частоты по ключу (реализации — internal/ratelimit: память процесса или Redis).
type Limiter interface {
	Allow(ctx context.Context, key string) (ok bool, retryAfter time.Duration, err error)
}

// keyFunc — ключ бакета для запроса; "" — не ограничивать.
type keyFunc func(r *http.Request) string

func byIP(prefix string) keyFunc {
	return func(r *http.Request) string { return prefix + clientIP(r) }
}

func byUser(prefix string) keyFunc {
	return func(r *http.Request) string {
		u := currentUser(r)
		if u.ID == 0 {
			return ""
		}
		return prefix + strconv.FormatInt(u.ID, 10)
	}
}

// limit — 429 с Retry-After, когда у ключа кончились токены. Сбой хранилища лимитов не роняет сайт:
// запрос пропускается, ошибка — в журнал (Fallback в ratelimit обычно подменяет Redis памятью раньше).
func limit(l Limiter, key keyFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if l == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := key(r)
			if k == "" {
				next.ServeHTTP(w, r)
				return
			}
			ok, wait, err := l.Allow(r.Context(), k)
			if err != nil {
				slog.WarnContext(r.Context(), "лимит не проверен — запрос пропущен", "err", err)
				next.ServeHTTP(w, r)
				return
			}
			if !ok {
				tooMany(w, wait)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func tooMany(w http.ResponseWriter, wait time.Duration) {
	sec := int(math.Ceil(wait.Seconds()))
	if sec < 1 {
		sec = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(sec))
	writeJSON(w, http.StatusTooManyRequests, errorDTO{Error: errorBody{
		Code: "rate_limited", Message: "Слишком много запросов подряд. Подождите немного и попробуйте снова.", RetryAfter: sec,
	}})
}

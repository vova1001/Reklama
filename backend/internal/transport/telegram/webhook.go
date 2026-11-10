// Package telegram — вход бота Telegram: вебхук POST /api/telegram/webhook (секрет в заголовке
// X-Telegram-Bot-Api-Secret-Token) и запасной приём через getUpdates (poller.go). Разбирает обновления
// и передаёт их сервису бота; логики бота здесь нет.
package telegram

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"ohvat/backend/internal/service"
)

// BotService — что транспорту нужно от бота.
type BotService interface {
	Handle(ctx context.Context, u service.BotUpdate) error
}

// Limiter — общий лимит частоты вебхука (internal/ratelimit). Ключ один: все обновления приходят от Telegram.
type Limiter interface {
	Allow(ctx context.Context, key string) (ok bool, retryAfter time.Duration, err error)
}

// SecretHeader — заголовок с секретом вебхука (secret_token из setWebhook).
const SecretHeader = "X-Telegram-Bot-Api-Secret-Token"

// Options — лимиты входа бота.
type Options struct {
	Secret        string        // TELEGRAM_WEBHOOK_SECRET; пусто — вебхук отвечает 404
	MaxBody       int64         // размер обновления, 1 МБ
	MaxConcurrent int           // одновременных обновлений (больше — 503, Telegram повторит), 32
	Timeout       time.Duration // на обработку одного обновления, 8 с
	UserRPS       float64       // событий в секунду от одного человека (остальное игнорируется), 1
	UserBurst     int           // запас подряд, 5
	// Limiter — лимит обновлений в секунду на весь вебхук (после проверки секрета); nil — без лимита.
	// Сверх лимита — 429 с Retry-After: Telegram повторит доставку позже.
	Limiter Limiter
	Log     *slog.Logger
}

// Handler — приём обновлений.
type Handler struct {
	bot   BotService
	opt   Options
	sem   chan struct{}
	seen  *recentIDs
	flood *userLimiter
	log   *slog.Logger
}

// New создаёт приём обновлений.
func New(bot BotService, opt Options) *Handler {
	if opt.MaxBody <= 0 {
		opt.MaxBody = 1 << 20
	}
	if opt.MaxConcurrent <= 0 {
		opt.MaxConcurrent = 32
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 8 * time.Second
	}
	if opt.UserRPS <= 0 {
		opt.UserRPS = 1
	}
	if opt.UserBurst <= 0 {
		opt.UserBurst = 5
	}
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	return &Handler{bot: bot, opt: opt, sem: make(chan struct{}, opt.MaxConcurrent), seen: newRecentIDs(2048),
		flood: newUserLimiter(opt.UserRPS, opt.UserBurst), log: opt.Log}
}

// ServeHTTP — POST /api/telegram/webhook.
// 401 — чужой запрос (секрет не совпал); 503 — перегрузка; 500 — сбой базы (Telegram повторит);
// 200 — обработано или не нужно боту (повторять незачем).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.opt.Secret == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	got := r.Header.Get(SecretHeader)
	if subtle.ConstantTimeCompare([]byte(got), []byte(h.opt.Secret)) != 1 {
		h.log.WarnContext(r.Context(), "бот: вебхук без верного секрета")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if h.opt.Limiter != nil {
		ok, wait, err := h.opt.Limiter.Allow(r.Context(), "telegram")
		if err != nil {
			h.log.WarnContext(r.Context(), "бот: лимит вебхука не проверен", "err", err)
		} else if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Max(1, math.Ceil(wait.Seconds())))))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
	}
	select {
	case h.sem <- struct{}{}:
		defer func() { <-h.sem }()
	default:
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.opt.MaxBody))
	if err != nil {
		h.log.WarnContext(r.Context(), "бот: обновление не прочитано", "err", err)
		w.WriteHeader(http.StatusOK) // слишком большое — повтор не поможет
		return
	}
	if err := h.Process(r.Context(), raw); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Process — одно обновление (из вебхука или getUpdates). Ошибка — стоит повторить.
func (h *Handler) Process(ctx context.Context, raw []byte) error {
	var u update
	if err := json.Unmarshal(raw, &u); err != nil || u.UpdateID == 0 {
		h.log.WarnContext(ctx, "бот: обновление не разобрано")
		return nil
	}
	ev, ok := u.toBot()
	if !ok {
		return nil
	}
	if !h.seen.add(u.UpdateID) {
		return nil // Telegram прислал повтор, уже обработано
	}
	if ev.From.ID != 0 && !ev.Blocked && !h.flood.allow(ev.From.ID) {
		h.log.WarnContext(ctx, "бот: слишком часто — событие пропущено", "tg_user", ev.From.ID)
		return nil
	}
	hctx, cancel := context.WithTimeout(ctx, h.opt.Timeout)
	defer cancel()
	if err := h.bot.Handle(hctx, ev); err != nil {
		h.seen.remove(u.UpdateID) // повтор от Telegram обработаем заново
		if !errors.Is(err, context.Canceled) {
			h.log.ErrorContext(ctx, "бот: событие не обработано", "update", u.UpdateID, "err", err)
		}
		return err
	}
	return nil
}

// Sweep — очистка ограничителя частоты от давно молчащих пользователей.
func (h *Handler) Sweep() { h.flood.sweep(10 * time.Minute) }

// RunSweep чистит ограничитель раз в every до отмены ctx.
func (h *Handler) RunSweep(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Sweep()
		}
	}
}

// recentIDs — последние n id обновлений (защита от повторной доставки).
type recentIDs struct {
	mu   sync.Mutex
	set  map[int64]struct{}
	ring []int64
	pos  int
}

func newRecentIDs(n int) *recentIDs {
	return &recentIDs{set: make(map[int64]struct{}, n), ring: make([]int64, n)}
}

// add — false, если id уже был.
func (r *recentIDs) add(id int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.set[id]; ok {
		return false
	}
	if old := r.ring[r.pos]; old != 0 {
		delete(r.set, old)
	}
	r.ring[r.pos] = id
	r.pos = (r.pos + 1) % len(r.ring)
	r.set[id] = struct{}{}
	return true
}

func (r *recentIDs) remove(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.set, id) // ячейка кольца освободится сама при перезаписи
}

// userLimiter — токен-бакет на пользователя Telegram.
type userLimiter struct {
	mu    sync.Mutex
	rps   float64
	burst float64
	b     map[int64]*bucket
	now   func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newUserLimiter(rps float64, burst int) *userLimiter {
	return &userLimiter{rps: rps, burst: float64(burst), b: map[int64]*bucket{}, now: time.Now}
}

func (l *userLimiter) allow(id int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.b[id]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.b[id] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rps
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *userLimiter) sweep(idle time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, b := range l.b {
		if now.Sub(b.last) > idle {
			delete(l.b, k)
		}
	}
}

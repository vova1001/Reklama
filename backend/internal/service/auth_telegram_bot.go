package service

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"ohvat/backend/internal/domain"
)

// ===== вход через бота Telegram =====
// 1. Сайт создаёт одноразовый токен (24 случайных байта) и секрет браузера (32 байта, httpOnly-cookie);
//    в базе — только их SHA-256. Ссылка: https://t.me/<бот>?start=login_<токен> (38 символов из 64 допустимых).
// 2. Бот по /start показывает «Подтвердить вход»; нажатие подтверждает вход этим аккаунтом Telegram.
// 3. Страница ждёт (long-poll) и по cookie забирает вход — атомарно и один раз — и получает сессию.
// Чужой браузер, даже зная ссылку, вход не заберёт: нужен секрет из cookie того браузера, что начал вход.

// BotStartPrefix — начало параметра start у ссылки входа.
const BotStartPrefix = "login_"

const botTokenLen = 32 // 24 байта в base64url

// Ошибки входа через бота.
var (
	ErrBotLoginExpired   = errors.New("ссылка входа устарела или уже использована")
	ErrBotLoginCancelled = errors.New("вход отменён в Telegram")
)

// TelegramLoginRepository — входы через бота.
type TelegramLoginRepository interface {
	CreateTelegramLogin(ctx context.Context, l domain.NewTelegramLogin) error
	TelegramLoginStats(ctx context.Context, ip string, since time.Time) (int, time.Time, error)
	PendingTelegramLogin(ctx context.Context, tokenHash []byte, now time.Time) (domain.TelegramLogin, error)
	ConfirmTelegramLogin(ctx context.Context, tokenHash []byte, p domain.TelegramProfile, now, graceUntil time.Time) (domain.TelegramLogin, error)
	CancelTelegramLogin(ctx context.Context, tokenHash []byte, now time.Time) (domain.TelegramLogin, error)
	TelegramLoginByBrowser(ctx context.Context, browserHash []byte) (domain.TelegramLogin, error)
	ConsumeTelegramLogin(ctx context.Context, browserHash []byte, now time.Time) (domain.TelegramLogin, error)
	DeleteTelegramLoginsBefore(ctx context.Context, t time.Time) (int64, error)
}

// TelegramBotStart — ссылка на бота и секрет для cookie браузера.
type TelegramBotStart struct {
	URL           string
	BrowserSecret string
	ExpiresIn     time.Duration
}

// StartTelegramBot — начать вход через бота. Нужно согласие; лимит — BotLoginPerIP запусков в час с адреса.
func (s *Auth) StartTelegramBot(ctx context.Context, role string, consent bool, ip string) (TelegramBotStart, error) {
	if !s.cfg.TelegramBot {
		return TelegramBotStart{}, ErrProviderDisabled
	}
	if !consent {
		return TelegramBotStart{}, errConsent()
	}
	if role != "" && !domain.ValidRole(role) {
		return TelegramBotStart{}, &ValidationError{Fields: map[string]string{"role": "Выберите: бизнес или автор"}}
	}
	now := s.clock.Now()
	n, first, err := s.tgLogins.TelegramLoginStats(ctx, ip, now.Add(-time.Hour))
	if err != nil {
		return TelegramBotStart{}, err
	}
	if n >= s.cfg.BotLoginPerIP {
		return TelegramBotStart{}, &LimitError{Reason: "ip_hourly", RetryAfter: first.Add(time.Hour).Sub(now)}
	}
	tb := make([]byte, 24)
	if _, err := io.ReadFull(s.rnd, tb); err != nil {
		return TelegramBotStart{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(tb)
	secret, err := randomToken(s.rnd)
	if err != nil {
		return TelegramBotStart{}, err
	}
	if err := s.tgLogins.CreateTelegramLogin(ctx, domain.NewTelegramLogin{
		TokenHash: hashToken(token), BrowserHash: hashToken(secret), Role: role, IP: ip,
		CreatedAt: now, ExpiresAt: now.Add(s.cfg.BotLoginTTL),
	}); err != nil {
		return TelegramBotStart{}, err
	}
	return TelegramBotStart{
		URL:           "https://t.me/" + s.cfg.TelegramBotName + "?start=" + BotStartPrefix + token,
		BrowserSecret: secret, ExpiresIn: s.cfg.BotLoginTTL,
	}, nil
}

// ParseBotStart — токен входа из параметра /start; не похоже на токен — false.
func ParseBotStart(payload string) (string, bool) {
	tok, ok := strings.CutPrefix(payload, BotStartPrefix)
	if !ok || len(tok) != botTokenLen {
		return "", false
	}
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return "", false
		}
	}
	return tok, true
}

// BotLoginPending — ожидающий вход по токену (бот показывает «Подтвердить вход»); нет — ErrBotLoginExpired.
func (s *Auth) BotLoginPending(ctx context.Context, token string) (domain.TelegramLogin, error) {
	if !s.cfg.TelegramBot {
		return domain.TelegramLogin{}, ErrProviderDisabled
	}
	l, err := s.tgLogins.PendingTelegramLogin(ctx, hashToken(token), s.clock.Now())
	if errors.Is(err, domain.ErrNotFound) {
		return l, ErrBotLoginExpired
	}
	return l, err
}

// ConfirmBotLogin — человек из Telegram подтвердил вход. Повтор тем же человеком — без ошибки;
// истёк, отменён или подтверждён другим — ErrBotLoginExpired.
func (s *Auth) ConfirmBotLogin(ctx context.Context, token string, p domain.TelegramProfile) (domain.TelegramLogin, error) {
	if !s.cfg.TelegramBot {
		return domain.TelegramLogin{}, ErrProviderDisabled
	}
	if p.IsBot || p.ID <= 0 {
		return domain.TelegramLogin{}, ErrTelegramInvalid
	}
	now := s.clock.Now()
	l, err := s.tgLogins.ConfirmTelegramLogin(ctx, hashToken(token), p, now, now.Add(s.cfg.BotLoginGrace))
	if errors.Is(err, domain.ErrNotFound) {
		return l, ErrBotLoginExpired
	}
	if err != nil {
		return l, err
	}
	s.signals.signal(hex.EncodeToString(l.BrowserHash))
	return l, nil
}

// CancelBotLogin — «Это не я»: неподтверждённый вход отменяется, страница входа узнаёт об этом.
func (s *Auth) CancelBotLogin(ctx context.Context, token string) error {
	if !s.cfg.TelegramBot {
		return ErrProviderDisabled
	}
	l, err := s.tgLogins.CancelTelegramLogin(ctx, hashToken(token), s.clock.Now())
	if errors.Is(err, domain.ErrNotFound) {
		return ErrBotLoginExpired
	}
	if err != nil {
		return err
	}
	s.signals.signal(hex.EncodeToString(l.BrowserHash))
	return nil
}

// BotLoginStatus — состояние ожидания для страницы входа.
type BotLoginStatus string

// Состояния ожидания.
const (
	BotLoginWaiting BotLoginStatus = "pending"
	BotLoginDone    BotLoginStatus = "ok"
)

// WaitTelegramBot — ждать подтверждения не дольше wait (long-poll). Подтверждён — вход (сессия), иначе
// BotLoginWaiting. Ссылка истекла — ErrBotLoginExpired, отменена — ErrBotLoginCancelled.
// Сигнал о подтверждении приходит сразу (в этом процессе); раз в 2 секунды — сверка с базой (несколько серверов).
func (s *Auth) WaitTelegramBot(ctx context.Context, browserSecret string, wait time.Duration) (BotLoginStatus, LoginResult, error) {
	if !s.cfg.TelegramBot {
		return "", LoginResult{}, ErrProviderDisabled
	}
	if browserSecret == "" || len(browserSecret) > 128 {
		return "", LoginResult{}, ErrBotLoginExpired
	}
	bh := hashToken(browserSecret)
	sig, stop := s.signals.subscribe(hex.EncodeToString(bh))
	defer stop()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		st, res, err := s.tryConsumeBot(ctx, bh)
		if err != nil || st == BotLoginDone {
			return st, res, err
		}
		select {
		case <-sig:
		case <-tick.C:
		case <-timer.C:
			return BotLoginWaiting, LoginResult{}, nil
		case <-ctx.Done():
			return BotLoginWaiting, LoginResult{}, ctx.Err()
		}
	}
}

func (s *Auth) tryConsumeBot(ctx context.Context, bh []byte) (BotLoginStatus, LoginResult, error) {
	now := s.clock.Now()
	l, err := s.tgLogins.ConsumeTelegramLogin(ctx, bh, now)
	if err == nil {
		id := domain.Identity{Provider: domain.ProviderTelegram, Subject: strconv.FormatInt(l.TgUserID, 10)}
		res, err := s.login(ctx, id, false, domain.NewUser{Role: l.Role, Name: clip(l.TgName, 80)})
		if err != nil {
			return "", LoginResult{}, err
		}
		return BotLoginDone, res, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return "", LoginResult{}, err
	}
	l, err = s.tgLogins.TelegramLoginByBrowser(ctx, bh)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return "", LoginResult{}, ErrBotLoginExpired
	case err != nil:
		return "", LoginResult{}, err
	case l.Cancelled:
		return "", LoginResult{}, ErrBotLoginCancelled
	case l.ConsumedAt != nil || !l.ExpiresAt.After(now):
		return "", LoginResult{}, ErrBotLoginExpired
	}
	return BotLoginWaiting, LoginResult{}, nil
}

// LogoutEverywhere — завершить все сеансы пользователя.
func (s *Auth) LogoutEverywhere(ctx context.Context, userID int64) (int64, error) {
	return s.sessions.DeleteUserSessions(ctx, userID)
}

// signalHub — «подтверждено» для ждущих запросов этого процесса: ключ → подписчики.
type signalHub struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
}

func newSignalHub() *signalHub { return &signalHub{subs: map[string]map[chan struct{}]struct{}{}} }

func (h *signalHub) subscribe(key string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[key] == nil {
		h.subs[key] = map[chan struct{}]struct{}{}
	}
	h.subs[key][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[key], ch)
		if len(h.subs[key]) == 0 {
			delete(h.subs, key)
		}
		h.mu.Unlock()
	}
}

func (h *signalHub) signal(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[key] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (h *signalHub) size() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

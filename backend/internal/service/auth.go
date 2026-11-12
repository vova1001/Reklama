package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"ohvat/backend/internal/domain"
)

// ===== зависимости сервиса входа (только интерфейсы) =====

// UserRepository — пользователи и способы входа.
type UserRepository interface {
	UserByIdentity(ctx context.Context, provider, subject string) (domain.User, error)
	UserByEmail(ctx context.Context, email string) (domain.User, error)
	// CreateUserWithIdentity создаёт пользователя и его первый способ входа одной транзакцией.
	// Почта или способ входа уже заняты — domain.ErrConflict.
	CreateUserWithIdentity(ctx context.Context, u domain.NewUser, id domain.Identity) (domain.User, error)
	// LinkIdentity привязывает способ входа к пользователю (и подтверждённую почту, если её не было).
	LinkIdentity(ctx context.Context, userID int64, id domain.Identity) error
	// TouchLogin отмечает вход: время входа и способ.
	TouchLogin(ctx context.Context, userID int64, id domain.Identity) error
}

// CodeRepository — одноразовые коды из письма.
type CodeRepository interface {
	CodeStatsByEmail(ctx context.Context, email string, since time.Time) (domain.CodeStats, error)
	CodeStatsByIP(ctx context.Context, ip string, since time.Time) (domain.CodeStats, error)
	// CreateCode гасит прежние коды почты и записывает новый (одной транзакцией).
	CreateCode(ctx context.Context, c domain.NewLoginCode) error
	ActiveCode(ctx context.Context, email string, now time.Time) (domain.LoginCode, error)
	// IncAttempts засчитывает попытку; лимит исчерпан или код погашен — domain.ErrNotFound.
	IncAttempts(ctx context.Context, id int64, max int) (int, error)
	ConsumeCode(ctx context.Context, id int64, now time.Time) (bool, error)
	DeleteCodesBefore(ctx context.Context, t time.Time) (int64, error)
}

// SessionRepository — сессии (в базе только хеш токена).
type SessionRepository interface {
	CreateSession(ctx context.Context, userID int64, tokenHash []byte, expires time.Time) error
	SessionUser(ctx context.Context, tokenHash []byte, now time.Time) (domain.User, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)
	// DeleteUserSessions завершает все сеансы пользователя.
	DeleteUserSessions(ctx context.Context, userID int64) (int64, error)
}

// Mailer — отправка письма с кодом. Реализация ставит письмо в очередь и не ждёт отправки.
type Mailer interface {
	SendLoginCode(ctx context.Context, to, code string) error
}

// GoogleProfile — кто вошёл через Google.
type GoogleProfile struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
}

// GoogleProvider — OAuth 2.0 Google (Authorization Code + PKCE).
type GoogleProvider interface {
	AuthURL(state, codeChallenge string) string
	Exchange(ctx context.Context, code, codeVerifier string) (GoogleProfile, error)
}

// Clock — текущее время (в тестах — подставное).
type Clock interface{ Now() time.Time }

// SystemClock — настоящее время в UTC.
type SystemClock struct{}

// Now — текущее время UTC.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// ===== настройки =====

// AuthConfig — правила входа. Нули заменяются значениями по умолчанию.
type AuthConfig struct {
	Secret          []byte        // ключ HMAC для хешей кодов (AUTH_SECRET)
	CodeTTL         time.Duration // срок кода, 10 мин
	MaxAttempts     int           // попыток на код, 5
	ResendAfter     time.Duration // пауза между письмами на одну почту, 60 с
	PerEmailHour    int           // писем на почту в час, 5
	PerIPHour       int           // писем с одного адреса в час, 20
	SessionTTL      time.Duration // срок сессии, 30 дней
	TelegramToken   string        // TELEGRAM_BOT_TOKEN; пусто — вход через Telegram выключен
	TelegramBotName string        // TELEGRAM_BOT_NAME — для подписи на странице входа
	TelegramMaxAge  time.Duration // насколько свежей должна быть подпись Telegram, 1 час
	TelegramBot     bool          // вход через бота включён (бот принимает сообщения)
	BotLoginTTL     time.Duration // срок ссылки входа через бота, 5 мин
	BotLoginGrace   time.Duration // сколько после подтверждения браузер может забрать вход, 2 мин
	BotLoginPerIP   int           // запусков входа через бота с адреса в час, 30
	EmailEnabled    bool          // вход по почте доступен (есть способ отправки)
	CodeRetention   time.Duration // сколько хранить строки кодов (для лимитов), 24 ч
	// AdminEmails — ADMIN_EMAILS: подтверждённая почта из списка при входе получает роль администратора
	// (только назначает; снять роль — в админке).
	AdminEmails []string
	// LoginRetention — сколько хранить журнал входов, 180 дней.
	LoginRetention time.Duration
}

func (c *AuthConfig) defaults() {
	if c.CodeTTL == 0 {
		c.CodeTTL = 10 * time.Minute
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 5
	}
	if c.ResendAfter == 0 {
		c.ResendAfter = 60 * time.Second
	}
	if c.PerEmailHour == 0 {
		c.PerEmailHour = 5
	}
	if c.PerIPHour == 0 {
		c.PerIPHour = 20
	}
	if c.SessionTTL == 0 {
		c.SessionTTL = 30 * 24 * time.Hour
	}
	if c.TelegramMaxAge == 0 {
		c.TelegramMaxAge = time.Hour
	}
	if c.BotLoginTTL == 0 {
		c.BotLoginTTL = 5 * time.Minute
	}
	if c.BotLoginGrace == 0 {
		c.BotLoginGrace = 2 * time.Minute
	}
	if c.BotLoginPerIP == 0 {
		c.BotLoginPerIP = 30
	}
	if c.CodeRetention == 0 {
		c.CodeRetention = 24 * time.Hour
	}
	if c.LoginRetention == 0 {
		c.LoginRetention = 180 * 24 * time.Hour
	}
}

// ===== ошибки сервиса (транспорт переводит их в ответы) =====

var (
	ErrUnauthorized     = errors.New("нужно войти")
	ErrCodeExpired      = errors.New("код устарел или не запрашивался")
	ErrTooManyAttempts  = errors.New("слишком много попыток")
	ErrTelegramInvalid  = errors.New("подпись Telegram не прошла проверку")
	ErrGoogleFailed     = errors.New("вход через Google не удался")
	ErrProviderDisabled = errors.New("способ входа не подключён")
	ErrMailUnavailable  = errors.New("письмо не отправлено")
)

// CodeInvalidError — неверный код; Left — сколько попыток осталось.
type CodeInvalidError struct{ Left int }

func (e *CodeInvalidError) Error() string {
	return fmt.Sprintf("неверный код, осталось попыток: %d", e.Left)
}

// LimitError — сработало ограничение частоты; RetryAfter — когда можно снова.
type LimitError struct {
	Reason     string // email_cooldown | email_hourly | ip_hourly
	RetryAfter time.Duration
}

func (e *LimitError) Error() string { return "ограничение частоты: " + e.Reason }

// ValidationError — ошибки полей: поле → понятный текст.
type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return "неверные данные" }

func (e *ValidationError) add(field, msg string) {
	if e.Fields == nil {
		e.Fields = map[string]string{}
	}
	if _, ok := e.Fields[field]; !ok {
		e.Fields[field] = msg
	}
}

func (e *ValidationError) orNil() error {
	if len(e.Fields) == 0 {
		return nil
	}
	return e
}

// ===== сервис =====

// AuthDeps — всё, от чего зависит сервис входа.
type AuthDeps struct {
	Users     UserRepository
	Codes     CodeRepository
	Sessions  SessionRepository
	Mailer    Mailer                  // nil — вход по почте выключен
	Google    GoogleProvider          // nil — вход через Google выключен
	BotLogins TelegramLoginRepository // nil — вход через бота выключен
	Devices   DeviceRepository        // nil — устройства не запоминаются (уведомление о каждом входе)
	Staff     StaffAccessRepository   // nil — без журнала входов и первого администратора (тесты)
	Notifier  Notifier                // nil — уведомления не отправляются
	Clock     Clock                   // nil — SystemClock
	Random    io.Reader               // nil — crypto/rand
	Log       *slog.Logger            // nil — slog.Default()
}

// Auth — регистрация и вход: почта с кодом, Telegram, Google; сессии.
type Auth struct {
	users    UserRepository
	codes    CodeRepository
	sessions SessionRepository
	mailer   Mailer
	google   GoogleProvider
	tgLogins TelegramLoginRepository
	devices  DeviceRepository
	staff    StaffAccessRepository
	notifier Notifier
	signals  *signalHub
	clock    Clock
	rnd      io.Reader
	log      *slog.Logger
	cfg      AuthConfig
}

// NewAuth создаёт сервис входа.
func NewAuth(d AuthDeps, cfg AuthConfig) *Auth {
	cfg.defaults()
	if d.Clock == nil {
		d.Clock = SystemClock{}
	}
	if d.Random == nil {
		d.Random = cryptoRand
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Mailer == nil {
		cfg.EmailEnabled = false
	}
	if d.BotLogins == nil || cfg.TelegramBotName == "" {
		cfg.TelegramBot = false
	}
	if len(cfg.Secret) == 0 {
		// без ключа коды всё равно хешируются — случайным ключом процесса (коды не переживут перезапуск)
		cfg.Secret = make([]byte, 32)
		_, _ = io.ReadFull(d.Random, cfg.Secret)
	}
	return &Auth{
		users: d.Users, codes: d.Codes, sessions: d.Sessions, mailer: d.Mailer, google: d.Google,
		tgLogins: d.BotLogins, devices: d.Devices, staff: d.Staff, notifier: d.Notifier, signals: newSignalHub(),
		clock: d.Clock, rnd: d.Random, log: d.Log, cfg: cfg,
	}
}

// Methods — какие способы входа включены (для страницы входа).
type Methods struct {
	Email           bool
	Google          bool
	TelegramBotID   string // "" — Telegram выключен
	TelegramBotName string
	TelegramBot     bool // вход через бота (ссылка t.me/<бот>?start=…)
}

// Methods — включённые способы входа.
func (s *Auth) Methods() Methods {
	return Methods{Email: s.cfg.EmailEnabled, Google: s.google != nil, TelegramBotID: telegramBotID(s.cfg.TelegramToken), TelegramBotName: s.cfg.TelegramBotName, TelegramBot: s.cfg.TelegramBot}
}

// LoginResult — успешный вход: токен для cookie и пользователь.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	User      domain.User
	Created   bool // пользователь новый
}

// login находит или создаёт пользователя по способу входа и открывает сессию.
// Порядок: способ уже привязан → он; подтверждённая почта совпадает → тот же пользователь, способ привязывается;
// иначе — новый пользователь. При гонке (двойной клик) — повтор поиска.
func (s *Auth) login(ctx context.Context, id domain.Identity, emailVerified bool, nu domain.NewUser) (LoginResult, error) {
	u, created, err := s.resolveUser(ctx, id, emailVerified, nu)
	if errors.Is(err, domain.ErrConflict) {
		u, created, err = s.resolveUser(ctx, id, emailVerified, nu)
	}
	if err != nil {
		return LoginResult{}, err
	}
	if u.Blocked() {
		return LoginResult{}, ErrBlocked
	}
	if u, err = s.bootstrapAdmin(ctx, u); err != nil {
		return LoginResult{}, err
	}
	if err := s.users.TouchLogin(ctx, u.ID, id); err != nil {
		return LoginResult{}, err
	}
	if s.staff != nil {
		ci := clientFrom(ctx)
		if err := s.staff.AddLoginEvent(ctx, u.ID, id.Provider, ci.IP, clip(ci.UserAgent, 300)); err != nil {
			s.log.WarnContext(ctx, "вход не записан в журнал входов", "user", u.ID, "err", err)
		}
	}
	tok, exp, err := s.newSession(ctx, u.ID)
	if err != nil {
		return LoginResult{}, err
	}
	s.afterLogin(ctx, u, id, created)
	return LoginResult{Token: tok, ExpiresAt: exp, User: u, Created: created}, nil
}

func (s *Auth) resolveUser(ctx context.Context, id domain.Identity, emailVerified bool, nu domain.NewUser) (domain.User, bool, error) {
	u, err := s.users.UserByIdentity(ctx, id.Provider, id.Subject)
	if err == nil {
		return u, false, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, false, err
	}
	if emailVerified && id.Email != "" {
		u, err = s.users.UserByEmail(ctx, id.Email)
		switch {
		case err == nil:
			if err := s.users.LinkIdentity(ctx, u.ID, id); err != nil {
				return domain.User{}, false, err
			}
			return u, false, nil
		case !errors.Is(err, domain.ErrNotFound):
			return domain.User{}, false, err
		}
		nu.Email = id.Email
	} else {
		nu.Email = ""
	}
	u, err = s.users.CreateUserWithIdentity(ctx, nu, id)
	if err != nil {
		return domain.User{}, false, err
	}
	return u, true, nil
}

func (s *Auth) newSession(ctx context.Context, userID int64) (string, time.Time, error) {
	tok, err := randomToken(s.rnd)
	if err != nil {
		return "", time.Time{}, err
	}
	exp := s.clock.Now().Add(s.cfg.SessionTTL)
	if err := s.sessions.CreateSession(ctx, userID, hashToken(tok), exp); err != nil {
		return "", time.Time{}, err
	}
	return tok, exp, nil
}

// Session — кто вошёл по токену из cookie; нет сессии или истекла — ErrUnauthorized.
func (s *Auth) Session(ctx context.Context, token string) (domain.User, error) {
	if token == "" || len(token) > 128 {
		return domain.User{}, ErrUnauthorized
	}
	u, err := s.sessions.SessionUser(ctx, hashToken(token), s.clock.Now())
	if errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, ErrUnauthorized
	}
	return u, err
}

// Logout удаляет сессию.
func (s *Auth) Logout(ctx context.Context, token string) error {
	if token == "" || len(token) > 128 {
		return nil
	}
	return s.sessions.DeleteSession(ctx, hashToken(token))
}

// Cleanup удаляет старые коды (старше срока хранения) и истёкшие сессии.
func (s *Auth) Cleanup(ctx context.Context) error {
	now := s.clock.Now()
	codes, err := s.codes.DeleteCodesBefore(ctx, now.Add(-s.cfg.CodeRetention))
	if err != nil {
		return fmt.Errorf("очистка кодов: %w", err)
	}
	sess, err := s.sessions.DeleteExpiredSessions(ctx, now)
	if err != nil {
		return fmt.Errorf("очистка сессий: %w", err)
	}
	var tg, logins int64
	if s.staff != nil {
		if logins, err = s.staff.DeleteLoginEventsBefore(ctx, now.Add(-s.cfg.LoginRetention)); err != nil {
			return fmt.Errorf("очистка журнала входов: %w", err)
		}
	}
	if s.tgLogins != nil {
		if tg, err = s.tgLogins.DeleteTelegramLoginsBefore(ctx, now.Add(-s.cfg.CodeRetention)); err != nil {
			return fmt.Errorf("очистка входов через бота: %w", err)
		}
	}
	if codes+sess+tg+logins > 0 {
		s.log.InfoContext(ctx, "очистка входа", "codes", codes, "sessions", sess, "telegram_logins", tg, "login_events", logins)
	}
	return nil
}

// RunCleanup — фоновая очистка по тикеру до отмены ctx.
func (s *Auth) RunCleanup(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.Cleanup(ctx); err != nil && ctx.Err() == nil {
			s.log.ErrorContext(ctx, "фоновая очистка", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func hashToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

// StaffAccessRepository — журнал входов и роль первого администратора.
type StaffAccessRepository interface {
	AddLoginEvent(ctx context.Context, userID int64, method, ip, ua string) error
	DeleteLoginEventsBefore(ctx context.Context, before time.Time) (int64, error)
	// GrantBootstrapAdmin — роль администратора с записью в журнал; уже администратор — domain.ErrNotFound.
	GrantBootstrapAdmin(ctx context.Context, userID int64, e domain.AuditEntry) (domain.User, error)
}

// bootstrapAdmin — подтверждённая почта из ADMIN_EMAILS получает роль администратора (один раз, в журнал).
func (s *Auth) bootstrapAdmin(ctx context.Context, u domain.User) (domain.User, error) {
	if s.staff == nil || u.Email == "" || u.StaffRole == domain.RoleAdmin {
		return u, nil
	}
	email := strings.ToLower(u.Email)
	for _, a := range s.cfg.AdminEmails {
		if a != email {
			continue
		}
		e := auditFor(ctx, domain.User{ID: 0, Name: "ADMIN_EMAILS"}, domain.AuditRoleBootstrap, "user", strconv.FormatInt(u.ID, 10))
		e.ActorLabel = "система (ADMIN_EMAILS)"
		e.Before = map[string]string{"role": string(u.StaffRole)}
		e.After = map[string]string{"role": string(domain.RoleAdmin)}
		out, err := s.staff.GrantBootstrapAdmin(ctx, u.ID, e)
		if errors.Is(err, domain.ErrNotFound) {
			return u, nil
		}
		if err != nil {
			return u, err
		}
		s.log.InfoContext(ctx, "роль администратора из ADMIN_EMAILS", "user", u.ID)
		return out, nil
	}
	return u, nil
}

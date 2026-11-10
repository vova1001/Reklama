package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	// TouchLogin отмечает вход: время входа, способ, роль — если ещё не выбрана.
	TouchLogin(ctx context.Context, userID int64, id domain.Identity, role string) error
	Providers(ctx context.Context, userID int64) ([]string, error)
	UpdateProfile(ctx context.Context, userID int64, p domain.Profile) (domain.User, error)
	CountCategories(ctx context.Context, slugs []string) (int, error)
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
	EmailEnabled    bool          // вход по почте доступен (есть способ отправки)
	CodeRetention   time.Duration // сколько хранить строки кодов (для лимитов), 24 ч
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
	if c.CodeRetention == 0 {
		c.CodeRetention = 24 * time.Hour
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
	ErrRoleLocked       = errors.New("роль уже выбрана")
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
	Users    UserRepository
	Codes    CodeRepository
	Sessions SessionRepository
	Mailer   Mailer         // nil — вход по почте выключен
	Google   GoogleProvider // nil — вход через Google выключен
	Clock    Clock          // nil — SystemClock
	Random   io.Reader      // nil — crypto/rand
	Log      *slog.Logger   // nil — slog.Default()
}

// Auth — регистрация и вход: почта с кодом, Telegram, Google; сессии; анкета.
type Auth struct {
	users    UserRepository
	codes    CodeRepository
	sessions SessionRepository
	mailer   Mailer
	google   GoogleProvider
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
	if len(cfg.Secret) == 0 {
		// без ключа коды всё равно хешируются — случайным ключом процесса (коды не переживут перезапуск)
		cfg.Secret = make([]byte, 32)
		_, _ = io.ReadFull(d.Random, cfg.Secret)
	}
	return &Auth{
		users: d.Users, codes: d.Codes, sessions: d.Sessions, mailer: d.Mailer, google: d.Google,
		clock: d.Clock, rnd: d.Random, log: d.Log, cfg: cfg,
	}
}

// Methods — какие способы входа включены (для страницы входа).
type Methods struct {
	Email           bool
	Google          bool
	TelegramBotID   string // "" — Telegram выключен
	TelegramBotName string
}

// Methods — включённые способы входа.
func (s *Auth) Methods() Methods {
	return Methods{Email: s.cfg.EmailEnabled, Google: s.google != nil, TelegramBotID: telegramBotID(s.cfg.TelegramToken), TelegramBotName: s.cfg.TelegramBotName}
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
	if err := s.users.TouchLogin(ctx, u.ID, id, nu.Role); err != nil {
		return LoginResult{}, err
	}
	if u.Role == "" && domain.ValidRole(nu.Role) {
		u.Role = nu.Role
	}
	tok, exp, err := s.newSession(ctx, u.ID)
	if err != nil {
		return LoginResult{}, err
	}
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
	if !domain.ValidRole(nu.Role) {
		nu.Role = ""
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

// Account — пользователь и его способы входа.
type Account struct {
	User      domain.User
	Providers []string
}

// Account — данные кабинета.
func (s *Auth) Account(ctx context.Context, u domain.User) (Account, error) {
	p, err := s.users.Providers(ctx, u.ID)
	if err != nil {
		return Account{}, err
	}
	return Account{User: u, Providers: p}, nil
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
	if codes+sess > 0 {
		s.log.InfoContext(ctx, "очистка входа", "codes", codes, "sessions", sess)
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

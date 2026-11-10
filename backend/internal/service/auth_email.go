package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/mail"
	"strings"
	"time"

	"ohvat/backend/internal/domain"
)

// CodeSent — письмо с кодом поставлено в отправку.
type CodeSent struct {
	Email       string
	ResendAfter time.Duration
	ExpiresIn   time.Duration
}

// NormalizeEmail — почта в нижнем регистре без пробелов; неверная — ValidationError.
func NormalizeEmail(raw string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(raw))
	ve := &ValidationError{}
	switch {
	case e == "":
		ve.add("email", "Введите почту")
	case len(e) > 254:
		ve.add("email", "Слишком длинный адрес почты")
	default:
		a, err := mail.ParseAddress(e)
		at := strings.LastIndexByte(e, '@')
		if err != nil || a.Address != e || at < 1 || !strings.Contains(e[at+1:], ".") || strings.ContainsAny(e, " <>\"") {
			ve.add("email", "Проверьте адрес почты — например, name@mail.ru")
		}
	}
	return e, ve.orNil()
}

// StartEmail — отправить код на почту. Лимиты: одно письмо на почту в ResendAfter, PerEmailHour в час на почту,
// PerIPHour в час с адреса. Нужно согласие на обработку данных.
func (s *Auth) StartEmail(ctx context.Context, rawEmail string, consent bool, ip string) (CodeSent, error) {
	if !s.cfg.EmailEnabled {
		return CodeSent{}, ErrProviderDisabled
	}
	email, err := NormalizeEmail(rawEmail)
	if err == nil && !consent {
		err = errConsent()
	}
	if err != nil {
		return CodeSent{}, err
	}
	now := s.clock.Now()
	hourAgo := now.Add(-time.Hour)

	byEmail, err := s.codes.CodeStatsByEmail(ctx, email, hourAgo)
	if err != nil {
		return CodeSent{}, err
	}
	if byEmail.Sent > 0 {
		if wait := byEmail.LastAt.Add(s.cfg.ResendAfter).Sub(now); wait > 0 {
			return CodeSent{}, &LimitError{Reason: "email_cooldown", RetryAfter: wait}
		}
	}
	if byEmail.Sent >= s.cfg.PerEmailHour {
		return CodeSent{}, &LimitError{Reason: "email_hourly", RetryAfter: byEmail.FirstAt.Add(time.Hour).Sub(now)}
	}
	byIP, err := s.codes.CodeStatsByIP(ctx, ip, hourAgo)
	if err != nil {
		return CodeSent{}, err
	}
	if byIP.Sent >= s.cfg.PerIPHour {
		return CodeSent{}, &LimitError{Reason: "ip_hourly", RetryAfter: byIP.FirstAt.Add(time.Hour).Sub(now)}
	}

	code, err := randomDigits(s.rnd, 6)
	if err != nil {
		return CodeSent{}, err
	}
	if err := s.codes.CreateCode(ctx, domain.NewLoginCode{
		Email: email, CodeHash: s.codeHash(email, code), IP: ip,
		CreatedAt: now, ExpiresAt: now.Add(s.cfg.CodeTTL),
	}); err != nil {
		return CodeSent{}, err
	}
	if err := s.mailer.SendLoginCode(ctx, email, code); err != nil {
		s.log.ErrorContext(ctx, "письмо с кодом не поставлено в очередь", "to", MaskEmail(email), "err", err)
		return CodeSent{}, ErrMailUnavailable
	}
	return CodeSent{Email: email, ResendAfter: s.cfg.ResendAfter, ExpiresIn: s.cfg.CodeTTL}, nil
}

// VerifyEmail — проверить код из письма и войти. Неверный код — CodeInvalidError с остатком попыток;
// попытки кончились — ErrTooManyAttempts; кода нет или истёк — ErrCodeExpired.
func (s *Auth) VerifyEmail(ctx context.Context, rawEmail, code string) (LoginResult, error) {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return LoginResult{}, err
	}
	code = strings.TrimSpace(code)
	if !isDigits(code, 6) {
		return LoginResult{}, &ValidationError{Fields: map[string]string{"code": "Код — 6 цифр из письма"}}
	}
	now := s.clock.Now()
	c, err := s.codes.ActiveCode(ctx, email, now)
	if errors.Is(err, domain.ErrNotFound) {
		return LoginResult{}, ErrCodeExpired
	}
	if err != nil {
		return LoginResult{}, err
	}
	n, err := s.codes.IncAttempts(ctx, c.ID, s.cfg.MaxAttempts)
	if errors.Is(err, domain.ErrNotFound) {
		return LoginResult{}, ErrTooManyAttempts
	}
	if err != nil {
		return LoginResult{}, err
	}
	if !hmac.Equal(s.codeHash(email, code), c.CodeHash) {
		left := s.cfg.MaxAttempts - n
		if left <= 0 {
			return LoginResult{}, ErrTooManyAttempts
		}
		return LoginResult{}, &CodeInvalidError{Left: left}
	}
	ok, err := s.codes.ConsumeCode(ctx, c.ID, now)
	if err != nil {
		return LoginResult{}, err
	}
	if !ok {
		return LoginResult{}, ErrCodeExpired
	}
	id := domain.Identity{Provider: domain.ProviderEmail, Subject: email, Email: email}
	return s.login(ctx, id, true, domain.NewUser{})
}

// codeHash — HMAC-SHA256(секрет, почта:код): в базе нет самих кодов, а без секрета их не подобрать по хешу.
func (s *Auth) codeHash(email, code string) []byte {
	m := hmac.New(sha256.New, s.cfg.Secret)
	m.Write([]byte(email))
	m.Write([]byte{':'})
	m.Write([]byte(code))
	return m.Sum(nil)
}

func isDigits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// MaskEmail — почта для журналов без персональных данных: a***@mail.ru.
func MaskEmail(e string) string {
	at := strings.LastIndexByte(e, '@')
	if at < 1 {
		return "***"
	}
	return e[:1] + "***" + e[at:]
}

// errConsent — без согласия на обработку персональных данных вход не начинается (152-ФЗ).
func errConsent() error {
	return &ValidationError{Fields: map[string]string{"consent": "Отметьте согласие на обработку данных"}}
}

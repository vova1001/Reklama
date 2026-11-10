package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"ohvat/backend/internal/domain"
)

// GoogleStart — начало входа через Google: адрес Google и одноразовые state/verifier,
// которые транспорт кладёт в короткую httpOnly-cookie и сверяет на возврате.
type GoogleStart struct {
	URL      string
	State    string
	Verifier string
}

// StartGoogle — адрес входа Google c state и PKCE (S256).
func (s *Auth) StartGoogle(consent bool) (GoogleStart, error) {
	if s.google == nil {
		return GoogleStart{}, ErrProviderDisabled
	}
	if !consent {
		return GoogleStart{}, errConsent()
	}
	state, err := randomToken(s.rnd)
	if err != nil {
		return GoogleStart{}, err
	}
	verifier, err := randomToken(s.rnd)
	if err != nil {
		return GoogleStart{}, err
	}
	return GoogleStart{URL: s.google.AuthURL(state, pkceChallenge(verifier)), State: state, Verifier: verifier}, nil
}

// LoginGoogle — обмен кода Google на профиль и вход. Совпадение подтверждённой почты Google с почтой
// пользователя — тот же аккаунт.
func (s *Auth) LoginGoogle(ctx context.Context, code, verifier, role string) (LoginResult, error) {
	if s.google == nil {
		return LoginResult{}, ErrProviderDisabled
	}
	if code == "" || len(code) > 2048 || verifier == "" || len(verifier) > 128 {
		return LoginResult{}, ErrGoogleFailed
	}
	p, err := s.google.Exchange(ctx, code, verifier)
	if err != nil {
		s.log.WarnContext(ctx, "Google: обмен кода", "err", err)
		return LoginResult{}, ErrGoogleFailed
	}
	if p.Subject == "" {
		return LoginResult{}, ErrGoogleFailed
	}
	email := strings.ToLower(strings.TrimSpace(p.Email))
	if _, err := NormalizeEmail(email); err != nil {
		email = ""
	}
	verified := p.EmailVerified && email != ""
	picture := p.Picture
	if !strings.HasPrefix(picture, "https://") {
		picture = ""
	}
	id := domain.Identity{Provider: domain.ProviderGoogle, Subject: p.Subject, Email: email}
	if !verified {
		id.Email = ""
	}
	return s.login(ctx, id, verified, domain.NewUser{Role: role, Name: clip(strings.TrimSpace(p.Name), 80), AvatarURL: picture})
}

// pkceChallenge — code_challenge = BASE64URL(SHA256(verifier)).
func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

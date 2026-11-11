package service

import (
	"context"
	"errors"
	"time"

	"ohvat/backend/internal/domain"
)

// ===== подтверждение соцсетей: что сервис ждёт от площадки =====
// Реализации — gateway/platforms/<площадка>: только официальные API (Telegram Bot API, Google OAuth + YouTube Data
// API, VK ID + VK API, TikTok Login Kit, Instagram API with Instagram Login, Threads API). Страницы профилей не
// загружаются, цифры со слов автора не принимаются.

// VerifyStart — первый шаг подключения.
type VerifyStart struct {
	State       string // одноразовый state OAuth (или токен ссылки на бота)
	Challenge   string // PKCE S256 code_challenge ("" — площадка без PKCE)
	RedirectURL string // ${PUBLIC_URL}/api/social/<площадка>/callback
}

// VerifyStep — куда отправить автора: страница разрешения площадки или ссылка на бота.
type VerifyStep struct {
	URL string
}

// VerifyCheck — второй шаг: код OAuth или канал Telegram.
type VerifyCheck struct {
	Code        string // OAuth: код из возврата
	Verifier    string // OAuth: PKCE verifier
	RedirectURL string
	State       string // OAuth: state (VK ID требует его в обмене кода)
	DeviceID    string // VK ID: device_id из возврата
	ChatID      int64  // Telegram: канал
	TgUserID    int64  // Telegram: кто должен быть админом
}

// Tokens — токены OAuth автора (хранятся только зашифрованно, используются только для обновления цифр).
type Tokens struct {
	Access           string    `json:"a"`
	Refresh          string    `json:"r,omitempty"`
	AccessExpiresAt  time.Time `json:"ae,omitempty"`
	RefreshExpiresAt time.Time `json:"re,omitempty"`
	IssuedAt         time.Time `json:"i,omitempty"`
	DeviceID         string    `json:"d,omitempty"` // VK ID: нужен для обновления токена
	Scope            string    `json:"s,omitempty"`
}

// VerifiedChannel — аккаунт, которым автор владеет, и его цифры.
type VerifiedChannel struct {
	ExternalID string
	Kind       string // domain.SocialProfile | SocialChannel | SocialGroup
	Handle     string
	Title      string
	URL        string
	Stats      domain.SocialStats
}

// VerifyResult — кто подтвердил (Subject — id человека на площадке) и его аккаунты.
type VerifyResult struct {
	Subject  string
	Channels []VerifiedChannel
	Tokens   *Tokens // nil — площадка без токенов (Telegram)
}

// RefreshInput — обновить цифры аккаунта.
type RefreshInput struct {
	Account  domain.SocialAccount
	Tokens   *Tokens // свежие (Renew уже вызван)
	TgUserID int64   // Telegram: владелец канала (проверка, что он всё ещё админ)
}

// PlatformVerifier — подтверждение аккаунта на площадке: Start → куда отправить автора; Check → владелец и цифры;
// Refresh → свежие цифры подтверждённого аккаунта.
type PlatformVerifier interface {
	Platform() domain.SocialPlatform
	Method() domain.SocialMethod
	Start(ctx context.Context, in VerifyStart) (VerifyStep, error)
	Check(ctx context.Context, in VerifyCheck) (VerifyResult, error)
	Refresh(ctx context.Context, in RefreshInput) (VerifiedChannel, error)
}

// TokenKeeper — токены площадок с OAuth: обновление по правилам площадки и отзыв при отвязке.
type TokenKeeper interface {
	// Renew — свежий токен доступа, если нужен (changed=false — прежний годен). Отозван — ErrSocialRevoked.
	Renew(ctx context.Context, t Tokens, now time.Time) (out Tokens, changed bool, err error)
	// Revoke — отозвать доступ у площадки (если площадка умеет; иначе nil).
	Revoke(ctx context.Context, t Tokens) error
}

// Ошибки площадок (адаптеры возвращают их, обёрнутыми в подробности для журнала).
var (
	ErrSocialDisabled    = errors.New("площадка не подключена (нет ключей)")
	ErrSocialState       = errors.New("подключение устарело или начато в другом браузере")
	ErrSocialDenied      = errors.New("автор отменил разрешение")
	ErrSocialRevoked     = errors.New("доступ к аккаунту отозван")
	ErrSocialNotOwner    = errors.New("автор не владелец и не админ")
	ErrSocialNoChannel   = errors.New("на площадке нет канала или профиль не подходит")
	ErrSocialUnavailable = errors.New("площадка временно недоступна")
	ErrSocialTooMany     = errors.New("слишком много аккаунтов")
	ErrTelegramTaken     = errors.New("Telegram привязан к другому аккаунту")
)

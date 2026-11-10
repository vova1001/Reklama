package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// TelegramUser — проверенные данные из Telegram Login.
type TelegramUser struct {
	ID       string
	Name     string
	Username string
	PhotoURL string
	AuthDate time.Time
}

// telegramFields — поля, которые подписывает Telegram Login Widget.
var telegramFields = map[string]bool{
	"id": true, "first_name": true, "last_name": true, "username": true, "photo_url": true, "auth_date": true, "hash": true,
}

// VerifyTelegram проверяет данные Telegram Login Widget
// (https://core.telegram.org/widgets/login#checking-authorization):
// HMAC-SHA256 от строки «ключ=значение» по алфавиту через \n, ключ — SHA-256 токена бота;
// auth_date не старше maxAge и не из будущего (запас 5 минут на расхождение часов).
func VerifyTelegram(data map[string]string, botToken string, now time.Time, maxAge time.Duration) (TelegramUser, error) {
	if botToken == "" {
		return TelegramUser{}, ErrProviderDisabled
	}
	got := data["hash"]
	if len(got) != 64 || data["id"] == "" || data["auth_date"] == "" {
		return TelegramUser{}, ErrTelegramInvalid
	}
	keys := make([]string, 0, len(data))
	for k, v := range data {
		if !telegramFields[k] || len(v) > 512 {
			return TelegramUser{}, ErrTelegramInvalid
		}
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k + "=" + data[k])
	}
	secret := sha256.Sum256([]byte(botToken))
	m := hmac.New(sha256.New, secret[:])
	m.Write([]byte(b.String()))
	want := m.Sum(nil)
	gotBytes, err := hex.DecodeString(got)
	if err != nil || !hmac.Equal(gotBytes, want) {
		return TelegramUser{}, ErrTelegramInvalid
	}
	sec, err := strconv.ParseInt(data["auth_date"], 10, 64)
	if err != nil {
		return TelegramUser{}, ErrTelegramInvalid
	}
	at := time.Unix(sec, 0).UTC()
	if now.Sub(at) > maxAge || at.Sub(now) > 5*time.Minute {
		return TelegramUser{}, ErrTelegramInvalid
	}
	if _, err := strconv.ParseInt(data["id"], 10, 64); err != nil {
		return TelegramUser{}, ErrTelegramInvalid
	}
	name := strings.TrimSpace(strings.TrimSpace(data["first_name"]) + " " + strings.TrimSpace(data["last_name"]))
	photo := data["photo_url"]
	if !strings.HasPrefix(photo, "https://") {
		photo = ""
	}
	return TelegramUser{ID: data["id"], Name: clip(name, 80), Username: data["username"], PhotoURL: photo, AuthDate: at}, nil
}

// LoginTelegram — вход через Telegram по данным виджета.
func (s *Auth) LoginTelegram(ctx context.Context, data map[string]string, consent bool) (LoginResult, error) {
	if !consent {
		return LoginResult{}, errConsent()
	}
	tg, err := VerifyTelegram(data, s.cfg.TelegramToken, s.clock.Now(), s.cfg.TelegramMaxAge)
	if err != nil {
		return LoginResult{}, err
	}
	id := domain.Identity{Provider: domain.ProviderTelegram, Subject: tg.ID}
	return s.login(ctx, id, false, domain.NewUser{Name: tg.Name, AvatarURL: tg.PhotoURL})
}

// telegramBotID — числовой id бота из токена «123456:ABC…» (нужен кнопке входа); нет токена — "".
func telegramBotID(token string) string {
	id, _, ok := strings.Cut(token, ":")
	if !ok {
		return ""
	}
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return ""
	}
	return id
}

// clip обрезает строку до n символов.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

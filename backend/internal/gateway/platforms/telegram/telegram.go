// Package telegram — подтверждение канала или группы Telegram ботом: автор добавляет бота в чат админом (права не
// нужны), бот проверяет getChatAdministrators (автор — создатель или админ) и getChatMemberCount (подписчики
// канала / участники группы). Реализует service.PlatformVerifier. Токенов автора нет — только Bot API.
//
// Документация: https://core.telegram.org/bots/api#getchatadministrators, #getchatmembercount, #getchat.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"ohvat/backend/internal/domain"
	tggw "ohvat/backend/internal/gateway/telegram"
	"ohvat/backend/internal/service"
)

// API — методы Bot API, которые нужны проверке (tggw.Client; в тестах — подставной).
type API interface {
	GetChat(ctx context.Context, chatID int64) (tggw.Chat, error)
	GetChatAdministrators(ctx context.Context, chatID int64) ([]tggw.ChatMember, error)
	GetChatMemberCount(ctx context.Context, chatID int64) (int64, error)
}

// Verifier — Telegram.
type Verifier struct {
	api     API
	botName string
}

// New создаёт адаптер; botName — имя бота без @ (ссылка t.me/<бот>).
func New(api API, botName string) *Verifier { return &Verifier{api: api, botName: botName} }

// Platform — telegram.
func (*Verifier) Platform() domain.SocialPlatform { return domain.PlatformTelegram }

// Method — бот.
func (*Verifier) Method() domain.SocialMethod { return domain.MethodBot }

// Start — ссылка на бота с одноразовым токеном привязки Telegram к аккаунту.
func (v *Verifier) Start(_ context.Context, in service.VerifyStart) (service.VerifyStep, error) {
	return service.VerifyStep{URL: "https://t.me/" + v.botName + "?start=" + service.SocialStartPrefix + in.State}, nil
}

// Check — автор (TgUserID) — создатель или админ чата; цифры — число подписчиков / участников.
func (v *Verifier) Check(ctx context.Context, in service.VerifyCheck) (service.VerifyResult, error) {
	ch, err := v.chat(ctx, in.ChatID, in.TgUserID)
	if err != nil {
		return service.VerifyResult{}, err
	}
	return service.VerifyResult{Subject: strconv.FormatInt(in.TgUserID, 10), Channels: []service.VerifiedChannel{ch}}, nil
}

// Refresh — свежие цифры и проверка, что владелец всё ещё админ, а бот — в чате.
func (v *Verifier) Refresh(ctx context.Context, in service.RefreshInput) (service.VerifiedChannel, error) {
	id, err := strconv.ParseInt(in.Account.ExternalID, 10, 64)
	if err != nil {
		return service.VerifiedChannel{}, fmt.Errorf("%w: telegram: id чата %q", service.ErrSocialNoChannel, in.Account.ExternalID)
	}
	return v.chat(ctx, id, in.TgUserID)
}

func (v *Verifier) chat(ctx context.Context, chatID, tgUserID int64) (service.VerifiedChannel, error) {
	if chatID == 0 || tgUserID <= 0 {
		return service.VerifiedChannel{}, service.ErrSocialNotOwner
	}
	c, err := v.api.GetChat(ctx, chatID)
	if err != nil {
		return service.VerifiedChannel{}, classify("getChat", err)
	}
	kind := domain.SocialGroup
	switch c.Type {
	case "channel":
		kind = domain.SocialChannel
	case "group", "supergroup":
	default:
		return service.VerifiedChannel{}, fmt.Errorf("%w: telegram: чат вида %q", service.ErrSocialNoChannel, c.Type)
	}
	admins, err := v.api.GetChatAdministrators(ctx, chatID)
	if err != nil {
		return service.VerifiedChannel{}, classify("getChatAdministrators", err)
	}
	owner := false
	for _, a := range admins {
		if a.User.ID == tgUserID && !a.User.IsBot && (a.Status == "creator" || a.Status == "administrator") {
			owner = true
			break
		}
	}
	if !owner {
		return service.VerifiedChannel{}, service.ErrSocialNotOwner
	}
	n, err := v.api.GetChatMemberCount(ctx, chatID)
	if err != nil {
		return service.VerifiedChannel{}, classify("getChatMemberCount", err)
	}
	out := service.VerifiedChannel{ExternalID: strconv.FormatInt(c.ID, 10), Kind: kind, Handle: c.Username, Title: c.Title}
	if out.ExternalID == "0" {
		out.ExternalID = strconv.FormatInt(chatID, 10)
	}
	if c.Username != "" {
		out.URL = "https://t.me/" + c.Username
	}
	if kind == domain.SocialChannel || n > 0 {
		out.Stats.Followers = &n
	}
	return out, nil
}

// classify — ошибка Bot API: бот не в чате / чат удалён (400, 403) — доступ потерян; лимит, сбой, сеть — временно.
func classify(method string, err error) error {
	var ae *tggw.APIError
	if errors.As(err, &ae) {
		switch {
		case ae.Temporary():
			return fmt.Errorf("%w: telegram %s: %d", service.ErrSocialUnavailable, method, ae.Code)
		case ae.Code == http.StatusBadRequest || ae.Code == http.StatusForbidden:
			return fmt.Errorf("%w: telegram %s: %s", service.ErrSocialRevoked, method, ae.Description)
		}
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	return fmt.Errorf("%w: telegram %s: %w", service.ErrSocialUnavailable, method, err)
}

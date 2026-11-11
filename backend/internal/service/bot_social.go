package service

import (
	"context"
	"errors"
	"html"
	"strconv"
	"strings"

	"ohvat/backend/internal/domain"
)

// ===== бот: подтверждение каналов и групп Telegram =====

// BotSocial — привязка Telegram и события каналов/групп (сервис Social).
type BotSocial interface {
	TelegramLinkInfo(ctx context.Context, token string) (TelegramLink, error)
	LinkTelegram(ctx context.Context, token string, p domain.TelegramProfile) (TelegramLinkResult, error)
	CancelTelegramLink(ctx context.Context, token string) error
	ChannelChanged(ctx context.Context, ev ChannelEvent) (ChannelResult, error)
}

// Действия кнопок привязки (callback_data ≤ 64 байт: 3 + 43).
const (
	cbLink       = "sl:"
	cbLinkCancel = "sx:"
)

// socialStart — /start ch_<токен>: спросить, привязать ли Telegram к аккаунту на сайте (защита от чужой ссылки).
func (b *Bot) socialStart(ctx context.Context, u BotUpdate, tok string) error {
	if b.social == nil {
		b.send(ctx, b.texts.unknown(u.ChatID))
		return nil
	}
	l, err := b.social.TelegramLinkInfo(ctx, tok)
	switch {
	case errors.Is(err, ErrSocialState):
		b.send(ctx, b.texts.linkExpired(u.ChatID, 0))
		return nil
	case err != nil:
		return err
	}
	b.send(ctx, b.texts.linkConfirm(u.ChatID, tok, l.Name))
	return nil
}

// socialCallback — «Привязать» / «Это не я».
func (b *Bot) socialCallback(ctx context.Context, u BotUpdate) (bool, error) {
	d := u.CallbackData
	if b.social == nil || (!strings.HasPrefix(d, cbLink) && !strings.HasPrefix(d, cbLinkCancel)) {
		return false, nil
	}
	tok, ok := ParseSocialStart(SocialStartPrefix + d[3:])
	if !ok {
		b.answer(ctx, u.CallbackID, "")
		return true, nil
	}
	if strings.HasPrefix(d, cbLinkCancel) {
		if err := b.social.CancelTelegramLink(ctx, tok); err != nil {
			return true, err
		}
		b.answer(ctx, u.CallbackID, "Отменено")
		b.send(ctx, BotMessage{ChatID: u.ChatID, EditMessageID: u.MessageID, Text: "Привязка отменена."})
		return true, nil
	}
	res, err := b.social.LinkTelegram(ctx, tok, u.From)
	switch {
	case errors.Is(err, ErrSocialState):
		b.answer(ctx, u.CallbackID, "Ссылка устарела")
		b.send(ctx, b.texts.linkExpired(u.ChatID, u.MessageID))
		return true, nil
	case errors.Is(err, ErrTelegramTaken):
		b.answer(ctx, u.CallbackID, "")
		b.send(ctx, b.texts.linkTaken(u.ChatID, u.MessageID))
		return true, nil
	case err != nil:
		return true, err
	}
	b.answer(ctx, u.CallbackID, "Telegram привязан")
	b.send(ctx, b.texts.linked(u.ChatID, u.MessageID, b.botName, res.Verified))
	return true, nil
}

// channel — бота добавили в канал/группу или убрали: ответ — в личный чат тому, кто это сделал.
func (b *Bot) channel(ctx context.Context, u BotUpdate) error {
	if b.social == nil || u.Channel == nil {
		return nil
	}
	res, err := b.social.ChannelChanged(ctx, *u.Channel)
	if err != nil {
		return err
	}
	to := u.Channel.From.ID
	title := u.Channel.Title
	switch res.Outcome {
	case ChannelVerified:
		b.send(ctx, b.texts.channelVerified(to, *res.Account))
	case ChannelPending:
		b.send(ctx, BotMessage{ChatID: to, Text: "Проверяю «" + html.EscapeString(title) + "» — Telegram ответил не сразу. Отметка появится в анкете в течение нескольких минут."})
	case ChannelNotLinked:
		b.send(ctx, b.texts.channelNotLinked(to, title))
	case ChannelNotAdmin:
		b.send(ctx, BotMessage{ChatID: to, Text: "Не вижу вас среди админов «" + html.EscapeString(title) + "». Подтвердить можно только свой канал или группу: вы — создатель или админ."})
	case ChannelRemoved:
		b.send(ctx, BotMessage{ChatID: to, Text: "Бот убран из «" + html.EscapeString(title) + "» — отметка «Подтверждено» в анкете снята."})
	}
	return nil
}

// ===== тексты =====

func (t botTexts) linkConfirm(chat int64, tok, name string) BotMessage {
	who := ""
	if n := strings.TrimSpace(name); n != "" {
		who = " «" + html.EscapeString(n) + "»"
	}
	return BotMessage{ChatID: chat,
		Text: "<b>Подтверждение канала</b>\n\nПривязать этот Telegram к аккаунту" + who + " на сайте " + t.brand +
			"? После этого бот сможет подтвердить ваши каналы и группы.\n\nЕсли вы не нажимали «Подключить» на сайте — нажмите «Это не я».",
		Buttons: [][]BotButton{{{Text: "Привязать", Data: cbLink + tok}}, {{Text: "Это не я", Data: cbLinkCancel + tok}}}}
}

func (t botTexts) linkExpired(chat, msg int64) BotMessage {
	return BotMessage{ChatID: chat, EditMessageID: msg,
		Text: "Ссылка устарела или уже использована. Откройте анкету на сайте → «Соцсети» → Telegram → «Подключить».", Buttons: rows(t.link("Открыть анкету", "/app/card"))}
}

func (t botTexts) linkTaken(chat, msg int64) BotMessage {
	return BotMessage{ChatID: chat, EditMessageID: msg,
		Text: "Этот Telegram уже привязан к другому аккаунту " + t.brand + ". Войдите на сайт через Telegram и подтвердите канал там."}
}

// linked — что делать дальше: добавить бота админом (кнопка выбора канала / группы).
func (t botTexts) linked(chat, msg int64, bot string, verified []domain.SocialAccount) BotMessage {
	var b strings.Builder
	b.WriteString("<b>Telegram привязан</b>\n\n")
	for _, a := range verified {
		b.WriteString("Подтверждено: «" + html.EscapeString(a.Title) + "» — " + followersText(a) + ".\n")
	}
	if len(verified) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("Добавьте бота @" + html.EscapeString(bot) + " в канал или группу администратором — права можно снять все. " +
		"Я проверю, что вы создатель или админ, и посчитаю подписчиков.")
	var btn [][]BotButton
	if bot != "" {
		btn = [][]BotButton{
			{{Text: "Добавить в канал", URL: "https://t.me/" + bot + "?startchannel&admin=post_messages"}},
			{{Text: "Добавить в группу", URL: "https://t.me/" + bot + "?startgroup=ohvat"}},
		}
	}
	return BotMessage{ChatID: chat, EditMessageID: msg, Text: b.String(), Buttons: btn}
}

func (t botTexts) channelVerified(chat int64, a domain.SocialAccount) BotMessage {
	head := "Канал «" + html.EscapeString(a.Title) + "» подтверждён"
	if a.Kind == domain.SocialGroup {
		head = "Группа «" + html.EscapeString(a.Title) + "» подтверждена"
	}
	return BotMessage{ChatID: chat,
		Text: "<b>" + head + "</b>\n\n" + capFirst(followersText(a)) +
			". Цифры обновляются сами раз в сутки. Права бота можно снять — нужно только, чтобы он оставался в чате.",
		Buttons: rows(t.link("Открыть анкету", "/app/card"))}
}

func (t botTexts) channelNotLinked(chat int64, title string) BotMessage {
	return BotMessage{ChatID: chat,
		Text:    "Чтобы подтвердить «" + html.EscapeString(title) + "», откройте анкету на сайте → «Соцсети» → Telegram → «Подключить» и нажмите «Привязать» здесь. Канал подтвердится сам.",
		Buttons: rows(t.link("Открыть анкету", "/app/card"))}
}

func followersText(a domain.SocialAccount) string {
	if a.Stats.Followers == nil {
		return "число подписчиков скрыто"
	}
	word := "подписчиков"
	if a.Kind == domain.SocialGroup {
		word = "участников"
	}
	return groupDigits(*a.Stats.Followers) + " " + word
}

func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteString(" ")
		}
		b.WriteRune(r)
	}
	return b.String()
}

func capFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ohvat/backend/internal/domain"
)

// Бот: «Подключить Telegram» → /start ch_<токен> → «Привязать» → бота добавляют в канал/группу → «подтверждено».
func TestBotSocialFlow(t *testing.T) {
	ctx := context.Background()
	se := newSocialEnv(t)
	out := &fakeMessenger{}
	bot := NewBot(BotDeps{Chats: newMemTG(), Messenger: out, Social: se.svc, Log: se.svc.log},
		BotConfig{SiteURL: "https://ohvat.test", BotName: "reklama_auth_bot"})

	st, err := se.svc.Start(ctx, creator, domain.PlatformTelegram)
	if err != nil || !strings.HasPrefix(st.URL, "https://example.test/") {
		t.Fatal(err, st.URL)
	}
	tok := st.URL[strings.Index(st.URL, "state=")+6:]
	tok = tok[:strings.Index(tok, "&")]

	// /start ch_<токен> — вопрос «Привязать?» с именем аккаунта
	if err := bot.Handle(ctx, msg("/start "+SocialStartPrefix+tok)); err != nil {
		t.Fatal(err)
	}
	m := out.last()
	if !strings.Contains(m.Text, "«Алина»") || m.Buttons[0][0].Data != cbLink+tok || m.Buttons[1][0].Data != cbLinkCancel+tok {
		t.Fatalf("%+v", m)
	}
	// битая ссылка
	_ = bot.Handle(ctx, msg("/start ch_битая"))
	if !strings.Contains(out.last().Text, "Ссылка устарела") {
		t.Errorf("%+v", out.last())
	}
	_ = bot.Handle(ctx, msg("/start "+SocialStartPrefix+strings.Repeat("z", 43)))
	if !strings.Contains(out.last().Text, "Ссылка устарела") {
		t.Errorf("чужой токен: %+v", out.last())
	}
	// «Привязать» — привязан, кнопки «Добавить в канал / группу»
	if err := bot.Handle(ctx, press(cbLink+tok)); err != nil {
		t.Fatal(err)
	}
	m = out.last()
	if m.EditMessageID != 55 || !strings.Contains(m.Text, "Telegram привязан") || len(m.Buttons) != 2 ||
		!strings.Contains(m.Buttons[0][0].URL, "t.me/reklama_auth_bot?startchannel") || !strings.Contains(m.Buttons[1][0].URL, "?startgroup") {
		t.Fatalf("%+v", m)
	}
	if se.repo.tg[creator.ID] != anna.ID {
		t.Fatal("Telegram не привязан")
	}
	// повторное нажатие — ссылка устарела
	_ = bot.Handle(ctx, press(cbLink+tok))
	if !strings.Contains(out.last().Text, "устарела") {
		t.Errorf("%+v", out.last())
	}
	// бота добавили в канал — подтверждено, ответ в личный чат добавившего
	ev := BotUpdate{ID: 3, ChatID: -1001, From: anna, Channel: &ChannelEvent{ChatID: -1001, Title: "Канал Алины", Type: "channel", Status: "administrator", From: anna}}
	if err := bot.Handle(ctx, ev); err != nil {
		t.Fatal(err)
	}
	m = out.last()
	if m.ChatID != anna.ID || !strings.Contains(m.Text, "подтверждён") || !strings.Contains(m.Text, "2 000 подписчиков") {
		t.Fatalf("%+v", m)
	}
	// группа
	se.tg.channels = []VerifiedChannel{{ExternalID: "-1002", Kind: domain.SocialGroup, Title: "Чат", Stats: domain.SocialStats{Followers: ptr(1234567)}}}
	ev.Channel = &ChannelEvent{ChatID: -1002, Title: "Чат", Type: "supergroup", Status: "member", From: anna}
	_ = bot.Handle(ctx, ev)
	if m := out.last(); !strings.Contains(m.Text, "Группа «Чат» подтверждена") || !strings.Contains(m.Text, "1 234 567 участников") {
		t.Errorf("%+v", m)
	}
	// не админ, временно, убрали
	se.tg.checkErr = ErrSocialNotOwner
	ev.Channel = &ChannelEvent{ChatID: -1003, Title: "Чужой", Type: "channel", Status: "administrator", From: anna}
	_ = bot.Handle(ctx, ev)
	if !strings.Contains(out.last().Text, "Не вижу вас среди админов «Чужой»") {
		t.Errorf("%+v", out.last())
	}
	se.tg.checkErr = ErrSocialUnavailable
	ev.Channel = &ChannelEvent{ChatID: -1004, Title: "Медленный", Type: "channel", Status: "administrator", From: anna}
	_ = bot.Handle(ctx, ev)
	if !strings.Contains(out.last().Text, "Проверяю «Медленный»") {
		t.Errorf("%+v", out.last())
	}
	ev.Channel = &ChannelEvent{ChatID: -1001, Title: "Канал Алины", Type: "channel", Status: "kicked", From: anna}
	_ = bot.Handle(ctx, ev)
	if !strings.Contains(out.last().Text, "отметка «Подтверждено» в анкете снята") {
		t.Errorf("%+v", out.last())
	}
	// Telegram не привязан
	other := domain.TelegramProfile{ID: 999, FirstName: "Б"}
	_ = bot.Handle(ctx, BotUpdate{ChatID: -5, From: other, Channel: &ChannelEvent{ChatID: -5, Title: "<b>X</b>", Type: "channel", Status: "administrator", From: other}})
	if m := out.last(); m.ChatID != 999 || !strings.Contains(m.Text, "«&lt;b&gt;X&lt;/b&gt;»") || m.Buttons[0][0].URL != "https://ohvat.test/app/card" {
		t.Errorf("%+v", m)
	}
}

func TestBotSocialCancelAndTaken(t *testing.T) {
	ctx := context.Background()
	se := newSocialEnv(t)
	out := &fakeMessenger{}
	bot := NewBot(BotDeps{Chats: newMemTG(), Messenger: out, Social: se.svc, Log: se.svc.log}, BotConfig{BotName: "b_bot"})
	start := func() string {
		st, _ := se.svc.Start(ctx, creator, domain.PlatformTelegram)
		tok := st.URL[strings.Index(st.URL, "state=")+6:]
		return tok[:strings.Index(tok, "&")]
	}
	tok := start()
	_ = bot.Handle(ctx, press(cbLinkCancel+tok))
	if !strings.Contains(out.last().Text, "Привязка отменена") {
		t.Errorf("%+v", out.last())
	}
	if _, err := se.svc.LinkTelegram(ctx, tok, anna); !errors.Is(err, ErrSocialState) {
		t.Error("после отмены ссылка не работает")
	}
	se.repo.tg[9] = anna.ID
	_ = bot.Handle(ctx, press(cbLink+start()))
	if !strings.Contains(out.last().Text, "уже привязан к другому аккаунту") {
		t.Errorf("%+v", out.last())
	}
	// испорченные данные кнопки
	before := out.count()
	_ = bot.Handle(ctx, press(cbLink+"короткий"))
	if out.count() != before {
		t.Errorf("на битую кнопку — только ответ на нажатие: %+v", out.last())
	}
	// без сервиса соцсетей — /start ch_ как неизвестная команда, события каналов игнорируются
	plain := NewBot(BotDeps{Chats: newMemTG(), Messenger: out, Log: se.svc.log}, BotConfig{})
	_ = plain.Handle(ctx, msg("/start "+SocialStartPrefix+strings.Repeat("a", 43)))
	if !strings.Contains(out.last().Text, "Не знаю") {
		t.Errorf("%+v", out.last())
	}
	n := out.count()
	_ = plain.Handle(ctx, BotUpdate{ChatID: -1, From: anna, Channel: &ChannelEvent{ChatID: -1, Status: "administrator", From: anna}})
	if out.count() != n {
		t.Error("ответ без сервиса")
	}
	if s := followersText(domain.SocialAccount{}); s != "число подписчиков скрыто" {
		t.Error(s)
	}
	if capFirst("") != "" {
		t.Error("capFirst")
	}
}

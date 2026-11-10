package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
)

func msg(text string) BotUpdate {
	return BotUpdate{ID: 1, ChatID: 777, Private: true, From: anna, Text: text}
}

func press(data string) BotUpdate {
	return BotUpdate{ID: 2, ChatID: 777, Private: true, From: anna, CallbackID: "cb1", CallbackData: data, MessageID: 55}
}

func TestBotCommands(t *testing.T) {
	ctx := context.Background()
	e := newBotEnv(t)
	cases := []struct {
		text, want string
		buttons    int
	}{
		{"/start", "Здравствуйте, Анна!", 1},
		{"/start@reklama_auth_bot", "Это бот <b>Охват</b>", 1},
		{"/help", "Подтвердить вход", 1},
		{"/HELP", "Команды", 1},
		{"привет", "Не знаю такой команды", 0},
		{"/profile", "Вы ещё не входили", 1},
		{"/start login_битая", "Ссылка для входа устарела", 1},
		{"/start login_" + strings.Repeat("a", 32), "Ссылка для входа устарела", 1},
	}
	for _, c := range cases {
		if err := e.bot.Handle(ctx, msg(c.text)); err != nil {
			t.Fatal(c.text, err)
		}
		m := e.out.last()
		if m.ChatID != 777 || !strings.Contains(m.Text, c.want) || len(m.Buttons) != c.buttons {
			t.Errorf("%s: %+v", c.text, m)
		}
	}
	if c := e.tg.chat(777); c.ChatID != 777 || c.Username != "anna" || !c.Notify {
		t.Errorf("чат: %+v", c)
	}
	// /stop и снова /start
	_ = e.bot.Handle(ctx, msg("/stop"))
	if e.tg.chat(777).Notify || !strings.Contains(e.out.last().Text, "выключены") {
		t.Error("stop")
	}
	_ = e.bot.Handle(ctx, msg("/start"))
	if !e.tg.chat(777).Notify || !strings.Contains(e.out.last().Text, "снова включены") {
		t.Error("start после stop")
	}
	// /profile вошедшего
	_, _ = e.repo.CreateUserWithIdentity(ctx, domain.NewUser{Role: "business", Name: "Кафе <Б>", Email: "cafe@mail.ru"}, domain.Identity{Provider: "telegram", Subject: "777"})
	_ = e.bot.Handle(ctx, msg("/profile"))
	if m := e.out.last(); !strings.Contains(m.Text, "<b>Кафе &lt;Б&gt;</b>") || !strings.Contains(m.Text, "Роль: бизнес") ||
		!strings.Contains(m.Text, "c***@mail.ru") || !strings.Contains(m.Text, "включены") || m.Buttons[0][0].URL != "https://ohvat.test/account" {
		t.Errorf("%+v", m)
	}
}

func TestBotIgnoresAndErrors(t *testing.T) {
	ctx := context.Background()
	e := newBotEnv(t)
	for _, u := range []BotUpdate{
		{ChatID: -100, Private: false, From: anna, Text: "/start"},
		{ChatID: 1, Private: true, From: domain.TelegramProfile{ID: 2, IsBot: true}, Text: "/start"},
		{ChatID: 1, Private: true, From: domain.TelegramProfile{}, Text: "/start"},
		{ChatID: 777, Private: true, From: anna},
		{ChatID: 777, Private: true, From: anna, Unblocked: true},
	} {
		if err := e.bot.Handle(ctx, u); err != nil {
			t.Error(err)
		}
	}
	if e.out.count() != 0 {
		t.Error("ответил там, где не нужно")
	}
	// остановили бота → чат помечен
	_ = e.bot.Handle(ctx, msg("/start"))
	_ = e.bot.Handle(ctx, BotUpdate{ChatID: 777, Private: true, From: anna, Blocked: true})
	if !e.tg.chat(777).Blocked {
		t.Error("blocked")
	}
	_ = e.bot.Handle(ctx, msg("/start"))
	if e.tg.chat(777).Blocked {
		t.Error("снова запустил")
	}
	e.bot.ChatBlocked(777)
	if !e.tg.chat(777).Blocked {
		t.Error("403 от Telegram")
	}
	// сбой базы — ошибка (Telegram повторит)
	e.tg.fail["SaveChat"] = errBoom
	if err := e.bot.Handle(ctx, msg("/help")); !errors.Is(err, errBoom) {
		t.Error(err)
	}
	e.tg.fail = map[string]error{"SetNotify": errBoom}
	if err := e.bot.Handle(ctx, msg("/stop")); !errors.Is(err, errBoom) {
		t.Error(err)
	}
	// очередь переполнена — не ошибка обработки (повтор не поможет), только журнал
	e.tg.fail = map[string]error{}
	e.out.err = errors.New("full")
	if err := e.bot.Handle(ctx, msg("/help")); err != nil {
		t.Error(err)
	}
	if err := e.bot.Handle(ctx, press("zzz")); err != nil {
		t.Error(err)
	}
}

func TestBotLoginButtons(t *testing.T) {
	ctx := context.Background()
	e := newBotEnv(t)
	st, _ := e.a.StartTelegramBot(ctx, "business", true, "ip")
	tok := tokenOf(t, st.URL)

	// /start с токеном → кнопки «Подтвердить вход» и «Это не я»
	if err := e.bot.Handle(ctx, msg("/start login_"+tok)); err != nil {
		t.Fatal(err)
	}
	m := e.out.last()
	if !strings.Contains(m.Text, "Подтвердите") || !strings.Contains(m.Text, "15:05") || len(m.Buttons) != 2 ||
		m.Buttons[0][0].Data != "ok:"+tok || m.Buttons[1][0].Data != "no:"+tok || len(m.Buttons[0][0].Data) > 64 {
		t.Fatalf("%+v", m)
	}
	// нажатие → вход подтверждён, сообщение заменено
	if err := e.bot.Handle(ctx, press("ok:"+tok)); err != nil {
		t.Fatal(err)
	}
	if m := e.out.last(); m.EditMessageID != 55 || !strings.Contains(m.Text, "Вход подтверждён") || e.out.answers[len(e.out.answers)-1] != "Вход подтверждён" {
		t.Fatalf("%+v", m)
	}
	if s, res, err := e.a.WaitTelegramBot(ctx, st.BrowserSecret, time.Millisecond); err != nil || s != BotLoginDone || res.User.Role != "business" {
		t.Fatal(s, err)
	}
	// повторное нажатие после входа — «устарела»
	_ = e.bot.Handle(ctx, press("ok:"+tok))
	if !strings.Contains(e.out.last().Text, "устарела") {
		t.Error("повтор")
	}
	// «Это не я»
	st2, _ := e.a.StartTelegramBot(ctx, "", true, "ip")
	tok2 := tokenOf(t, st2.URL)
	_ = e.bot.Handle(ctx, press("no:"+tok2))
	if m := e.out.last(); m.EditMessageID != 55 || !strings.Contains(m.Text, "отменён") {
		t.Errorf("%+v", m)
	}
	if _, _, err := e.a.WaitTelegramBot(ctx, st2.BrowserSecret, time.Millisecond); !errors.Is(err, ErrBotLoginCancelled) {
		t.Error(err)
	}
	// кривые данные кнопок — просто ответ на нажатие
	for _, d := range []string{"ok:short", "no:!!", "out:abc"} {
		if err := e.bot.Handle(ctx, press(d)); err != nil {
			t.Error(d, err)
		}
	}
	// сбой базы при подтверждении — ошибка
	st3, _ := e.a.StartTelegramBot(ctx, "", true, "ip")
	e.tg.fail["ConfirmTelegramLogin"] = errBoom
	if err := e.bot.Handle(ctx, press("ok:"+tokenOf(t, st3.URL))); !errors.Is(err, errBoom) {
		t.Error(err)
	}
}

func TestBotLogoutAll(t *testing.T) {
	ctx := context.Background()
	e := newBotEnv(t)
	u, _ := e.repo.CreateUserWithIdentity(ctx, domain.NewUser{Name: "A"}, domain.Identity{Provider: "telegram", Subject: "777"})
	other, _ := e.repo.CreateUserWithIdentity(ctx, domain.NewUser{Name: "B"}, domain.Identity{Provider: "email", Subject: "b@b.ru"})
	_, _, _ = e.a.newSession(ctx, u.ID)
	_, _, _ = e.a.newSession(ctx, other.ID)
	// чужой аккаунт в кнопке — отказ
	_ = e.bot.Handle(ctx, press("out:"+itoa(other.ID)))
	if e.repo.sessionCount(other.ID) != 1 || !strings.Contains(e.out.answers[len(e.out.answers)-1], "не привязан") {
		t.Fatal("чужие сеансы")
	}
	_ = e.bot.Handle(ctx, press("out:"+itoa(u.ID)))
	if e.repo.sessionCount(u.ID) != 0 || !strings.Contains(e.out.last().Text, "Все сеансы завершены") {
		t.Fatal("свои сеансы")
	}
	e.repo.fail["DeleteUserSessions"] = errBoom
	if err := e.bot.Handle(ctx, press("out:"+itoa(u.ID))); !errors.Is(err, errBoom) {
		t.Error(err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestBotNotifier(t *testing.T) {
	ctx := context.Background()
	tg, out := newMemTG(), &fakeMessenger{}
	n := NewBotNotifier(tg, out, "https://ohvat.test", nil)
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	// Telegram не привязан — молча ничего
	if err := n.Notify(ctx, 1, Notification{Kind: NotifyNewLogin}); err != nil || out.count() != 0 {
		t.Fatal(err)
	}
	tg.idents[1] = 777
	_, _ = tg.SaveChat(ctx, 777, 777, "anna")
	cases := []struct {
		n     Notification
		want  []string
		btn   string
		error bool
	}{
		{Notification{Kind: NotifyNewLogin, At: at, Method: "google", Device: "Chrome, Windows", IP: "1.2.3.x"},
			[]string{"нового устройства", "Способ: Google", "Chrome, Windows", "1.2.3.x", "10.11.2026 15:00 МСК"}, "out:1", false},
		{Notification{Kind: NotifyNewTask, Title: "Обзор <кафе>", Topic: "Еда", Path: "/tasks/5"},
			[]string{"по теме «Еда»", "Обзор &lt;кафе&gt;"}, "https://ohvat.test/tasks/5", false},
		{Notification{Kind: NotifyNewTask, Title: "Без темы", Path: "//evil.com"}, []string{"<b>Новое задание</b>"}, "https://ohvat.test/account", false},
		{Notification{Kind: NotifyNewResponse, Title: "Обзор", From: "Иван"}, []string{"Новый отклик", "От: Иван"}, "https://ohvat.test/account", false},
		{Notification{Kind: NotifyPayout, AmountKopecks: 150000, Status: PayoutDone}, []string{"Выплата 1 500 ₽</b> зачислена"}, "https://ohvat.test/account", false},
		{Notification{Kind: NotifyPayout, AmountKopecks: 100, Status: "lost"}, nil, "", true},
		{Notification{Kind: NotifyNewTask}, nil, "", true},
		{Notification{Kind: "spam"}, nil, "", true},
	}
	for _, c := range cases {
		before := out.count()
		err := n.Notify(ctx, 1, c.n)
		if c.error {
			if !errors.Is(err, ErrBadNotification) || out.count() != before {
				t.Errorf("%s: %v", c.n.Kind, err)
			}
			continue
		}
		m := out.last()
		if err != nil || m.ChatID != 777 {
			t.Fatalf("%s: %v %+v", c.n.Kind, err, m)
		}
		for _, w := range c.want {
			if !strings.Contains(m.Text, w) {
				t.Errorf("%s: нет %q в %q", c.n.Kind, w, m.Text)
			}
		}
		if b := m.Buttons[0][0]; b.Data != c.btn && b.URL != c.btn {
			t.Errorf("%s: кнопка %+v", c.n.Kind, b)
		}
	}
	// /stop — не пишем; сбой базы и очереди — ошибка
	_ = tg.SetNotify(ctx, 777, false)
	before := out.count()
	if err := n.Notify(ctx, 1, cases[0].n); err != nil || out.count() != before {
		t.Error("после /stop")
	}
	_ = tg.SetNotify(ctx, 777, true)
	tg.fail["NotifyChat"] = errBoom
	if err := n.Notify(ctx, 1, cases[0].n); !errors.Is(err, errBoom) {
		t.Error(err)
	}
	delete(tg.fail, "NotifyChat")
	out.err = errBoom
	if err := n.Notify(ctx, 1, cases[0].n); !errors.Is(err, errBoom) {
		t.Error(err)
	}
}

func TestBotTextsWithoutHTTPS(t *testing.T) {
	tx := newBotTexts("http://localhost:8000")
	if m := tx.welcome(1, domain.TelegramProfile{}, false); len(m.Buttons) != 0 || !strings.HasPrefix(m.Text, "Здравствуйте!") {
		t.Errorf("%+v", m) // Telegram не принимает http-ссылки в кнопках
	}
	if got := parseCmd("  /Start@Bot  login_x "); got != "/start|login_x" {
		t.Error(got)
	}
}

func parseCmd(s string) string { c, a := parseCommand(s); return c + "|" + a }

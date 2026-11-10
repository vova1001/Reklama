package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
)

type botEnv struct {
	env
	tg    *memTG
	notes *fakeNotifier
	out   *fakeMessenger
	bot   *Bot
}

func newBotEnv(t *testing.T, mut ...func(*AuthDeps, *AuthConfig)) botEnv {
	t.Helper()
	b := botEnv{tg: newMemTG(), notes: &fakeNotifier{}, out: &fakeMessenger{}}
	b.env = newEnv(t, append([]func(*AuthDeps, *AuthConfig){func(d *AuthDeps, c *AuthConfig) {
		d.BotLogins, d.Devices, d.Notifier = b.tg, b.tg, b.notes
		c.TelegramBot, c.TelegramBotName = true, "reklama_auth_bot"
	}}, mut...)...)
	b.bot = NewBot(BotDeps{Auth: b.a, Users: b.repo, Chats: b.tg, Messenger: b.out, Clock: b.clk, Log: b.a.log},
		BotConfig{SiteURL: "https://ohvat.test"})
	return b
}

// tokenOf — токен из ссылки t.me/<бот>?start=login_<токен>.
func tokenOf(t *testing.T, url string) string {
	t.Helper()
	_, payload, ok := strings.Cut(url, "?start=")
	tok, good := ParseBotStart(payload)
	if !ok || !good {
		t.Fatalf("ссылка: %s", url)
	}
	return tok
}

var anna = domain.TelegramProfile{ID: 777, FirstName: "Анна", LastName: "К", Username: "anna"}

func TestBotLoginFlow(t *testing.T) {
	ctx := context.Background()
	e := newBotEnv(t)
	st, err := e.a.StartTelegramBot(ctx, "creator", true, "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(st.URL, "https://t.me/reklama_auth_bot?start=login_") || len(st.URL[strings.Index(st.URL, "=")+1:]) > 64 ||
		st.ExpiresIn != 5*time.Minute || st.BrowserSecret == "" {
		t.Fatalf("%+v", st)
	}
	tok := tokenOf(t, st.URL)
	// в базе нет ни токена, ни секрета браузера — только хеши
	if l := e.tg.logins[0]; strings.Contains(string(l.token), tok) || strings.Contains(string(l.BrowserHash), st.BrowserSecret) {
		t.Fatal("в базе открытые значения")
	}
	// пока не подтверждён — ждём
	stt, _, err := e.a.WaitTelegramBot(ctx, st.BrowserSecret, 10*time.Millisecond)
	if err != nil || stt != BotLoginWaiting {
		t.Fatalf("%v %v", stt, err)
	}
	// подтверждение будит ждущий запрос сразу
	var wg sync.WaitGroup
	var res LoginResult
	var werr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		stt, res, werr = e.a.WaitTelegramBot(ctx, st.BrowserSecret, 5*time.Second)
	}()
	for e.a.signals.size() == 0 {
		time.Sleep(time.Millisecond)
	}
	started := time.Now()
	if _, err := e.a.ConfirmBotLogin(ctx, tok, anna); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if werr != nil || stt != BotLoginDone || res.Token == "" || !res.Created || res.User.Role != "creator" || res.User.Name != "Анна К" {
		t.Fatalf("%v %v %+v", stt, werr, res)
	}
	if time.Since(started) > time.Second {
		t.Error("сигнал не разбудил ожидание")
	}
	if e.a.signals.size() != 0 {
		t.Error("подписка не снята")
	}
	// тот же способ входа, что у виджета: telegram:<id>
	if u, err := e.repo.UserByIdentity(ctx, domain.ProviderTelegram, "777"); err != nil || u.ID != res.User.ID {
		t.Fatalf("%v %+v", err, u)
	}
	// одноразовость: второй раз не забрать, подтвердить снова — нельзя
	if _, _, err := e.a.WaitTelegramBot(ctx, st.BrowserSecret, time.Millisecond); !errors.Is(err, ErrBotLoginExpired) {
		t.Errorf("повтор: %v", err)
	}
	if _, err := e.a.ConfirmBotLogin(ctx, tok, anna); !errors.Is(err, ErrBotLoginExpired) {
		t.Errorf("повторное подтверждение: %v", err)
	}
	// новый пользователь — уведомления о входе нет
	if e.notes.count() != 0 {
		t.Error("уведомление при регистрации")
	}
}

func TestBotLoginErrors(t *testing.T) {
	ctx := context.Background()
	t.Run("выключен", func(t *testing.T) {
		e := newBotEnv(t, func(d *AuthDeps, _ *AuthConfig) { d.BotLogins = nil })
		if _, err := e.a.StartTelegramBot(ctx, "", true, "ip"); !errors.Is(err, ErrProviderDisabled) {
			t.Error(err)
		}
		if e.a.Methods().TelegramBot {
			t.Error("Methods")
		}
		for _, err := range []error{
			func() error { _, err := e.a.BotLoginPending(ctx, "x"); return err }(),
			func() error { _, err := e.a.ConfirmBotLogin(ctx, "x", anna); return err }(),
			e.a.CancelBotLogin(ctx, "x"),
			func() error { _, _, err := e.a.WaitTelegramBot(ctx, "x", time.Millisecond); return err }(),
		} {
			if !errors.Is(err, ErrProviderDisabled) {
				t.Error(err)
			}
		}
	})
	t.Run("согласие и роль", func(t *testing.T) {
		e := newBotEnv(t)
		var ve *ValidationError
		if _, err := e.a.StartTelegramBot(ctx, "", false, "ip"); !errors.As(err, &ve) || ve.Fields["consent"] == "" {
			t.Error(err)
		}
		if _, err := e.a.StartTelegramBot(ctx, "admin", true, "ip"); !errors.As(err, &ve) || ve.Fields["role"] == "" {
			t.Error(err)
		}
	})
	t.Run("лимит с адреса", func(t *testing.T) {
		e := newBotEnv(t, func(_ *AuthDeps, c *AuthConfig) { c.BotLoginPerIP = 3 })
		for i := 0; i < 3; i++ {
			if _, err := e.a.StartTelegramBot(ctx, "", true, "9.9.9.9"); err != nil {
				t.Fatal(err)
			}
			e.clk.add(time.Minute)
		}
		var le *LimitError
		if _, err := e.a.StartTelegramBot(ctx, "", true, "9.9.9.9"); !errors.As(err, &le) || le.RetryAfter != 57*time.Minute {
			t.Fatalf("%v", err)
		}
		if _, err := e.a.StartTelegramBot(ctx, "", true, "8.8.8.8"); err != nil {
			t.Error("другой адрес", err)
		}
	})
	t.Run("ссылка истекла", func(t *testing.T) {
		e := newBotEnv(t)
		st, _ := e.a.StartTelegramBot(ctx, "", true, "ip")
		e.clk.add(5*time.Minute + time.Second)
		if _, err := e.a.BotLoginPending(ctx, tokenOf(t, st.URL)); !errors.Is(err, ErrBotLoginExpired) {
			t.Error(err)
		}
		if _, err := e.a.ConfirmBotLogin(ctx, tokenOf(t, st.URL), anna); !errors.Is(err, ErrBotLoginExpired) {
			t.Error(err)
		}
		if _, _, err := e.a.WaitTelegramBot(ctx, st.BrowserSecret, time.Millisecond); !errors.Is(err, ErrBotLoginExpired) {
			t.Error(err)
		}
	})
	t.Run("подтверждение продлевает срок на grace", func(t *testing.T) {
		e := newBotEnv(t)
		st, _ := e.a.StartTelegramBot(ctx, "", true, "ip")
		e.clk.add(4*time.Minute + 59*time.Second)
		if _, err := e.a.ConfirmBotLogin(ctx, tokenOf(t, st.URL), anna); err != nil {
			t.Fatal(err)
		}
		e.clk.add(time.Minute)
		if s, _, err := e.a.WaitTelegramBot(ctx, st.BrowserSecret, time.Millisecond); err != nil || s != BotLoginDone {
			t.Fatal(s, err)
		}
	})
	t.Run("отмена", func(t *testing.T) {
		e := newBotEnv(t)
		st, _ := e.a.StartTelegramBot(ctx, "", true, "ip")
		if err := e.a.CancelBotLogin(ctx, tokenOf(t, st.URL)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.a.WaitTelegramBot(ctx, st.BrowserSecret, time.Millisecond); !errors.Is(err, ErrBotLoginCancelled) {
			t.Error(err)
		}
		if _, err := e.a.ConfirmBotLogin(ctx, tokenOf(t, st.URL), anna); !errors.Is(err, ErrBotLoginExpired) {
			t.Error("после отмены", err)
		}
		if err := e.a.CancelBotLogin(ctx, tokenOf(t, st.URL)); !errors.Is(err, ErrBotLoginExpired) {
			t.Error("повторная отмена", err)
		}
	})
	t.Run("подтвердил другой аккаунт — второй не перехватит", func(t *testing.T) {
		e := newBotEnv(t)
		st, _ := e.a.StartTelegramBot(ctx, "", true, "ip")
		tok := tokenOf(t, st.URL)
		if _, err := e.a.ConfirmBotLogin(ctx, tok, anna); err != nil {
			t.Fatal(err)
		}
		if _, err := e.a.ConfirmBotLogin(ctx, tok, anna); err != nil {
			t.Error("повтор тем же — без ошибки", err)
		}
		if _, err := e.a.ConfirmBotLogin(ctx, tok, domain.TelegramProfile{ID: 9}); !errors.Is(err, ErrBotLoginExpired) {
			t.Error(err)
		}
	})
	t.Run("бот не может подтвердить", func(t *testing.T) {
		e := newBotEnv(t)
		st, _ := e.a.StartTelegramBot(ctx, "", true, "ip")
		if _, err := e.a.ConfirmBotLogin(ctx, tokenOf(t, st.URL), domain.TelegramProfile{ID: 5, IsBot: true}); !errors.Is(err, ErrTelegramInvalid) {
			t.Error(err)
		}
	})
	t.Run("чужой секрет браузера", func(t *testing.T) {
		e := newBotEnv(t)
		for _, s := range []string{"", strings.Repeat("x", 200), "чужой"} {
			if _, _, err := e.a.WaitTelegramBot(ctx, s, time.Millisecond); !errors.Is(err, ErrBotLoginExpired) {
				t.Errorf("%q: %v", s, err)
			}
		}
	})
	t.Run("ожидание прерывается отменой запроса", func(t *testing.T) {
		e := newBotEnv(t)
		st, _ := e.a.StartTelegramBot(ctx, "", true, "ip")
		cctx, cancel := context.WithCancel(ctx)
		go func() { time.Sleep(10 * time.Millisecond); cancel() }()
		if s, _, err := e.a.WaitTelegramBot(cctx, st.BrowserSecret, 5*time.Second); !errors.Is(err, context.Canceled) || s != BotLoginWaiting {
			t.Error(s, err)
		}
	})
	t.Run("сбои базы", func(t *testing.T) {
		for _, m := range []string{"TelegramLoginStats", "CreateTelegramLogin"} {
			e := newBotEnv(t)
			e.tg.fail[m] = errBoom
			if _, err := e.a.StartTelegramBot(ctx, "", true, "ip"); !errors.Is(err, errBoom) {
				t.Error(m, err)
			}
		}
		e := newBotEnv(t)
		st, _ := e.a.StartTelegramBot(ctx, "", true, "ip")
		e.tg.fail["ConfirmTelegramLogin"] = errBoom
		if _, err := e.a.ConfirmBotLogin(ctx, tokenOf(t, st.URL), anna); !errors.Is(err, errBoom) {
			t.Error(err)
		}
		for _, m := range []string{"ConsumeTelegramLogin", "TelegramLoginByBrowser"} {
			e.tg.fail = map[string]error{m: errBoom}
			if _, _, err := e.a.WaitTelegramBot(ctx, st.BrowserSecret, time.Millisecond); !errors.Is(err, errBoom) {
				t.Error(m, err)
			}
		}
	})
	t.Run("сбой создания сессии после подтверждения", func(t *testing.T) {
		e := newBotEnv(t)
		st, _ := e.a.StartTelegramBot(ctx, "", true, "ip")
		_, _ = e.a.ConfirmBotLogin(ctx, tokenOf(t, st.URL), anna)
		e.repo.fail["CreateSession"] = errBoom
		if _, _, err := e.a.WaitTelegramBot(ctx, st.BrowserSecret, time.Millisecond); !errors.Is(err, errBoom) {
			t.Error(err)
		}
	})
}

func TestParseBotStart(t *testing.T) {
	good := strings.Repeat("aZ0-_", 6) + "xy"
	cases := map[string]bool{
		"login_" + good: true, good: false, "login_" + good[:31]: false, "login_" + good + "a": false,
		"login_" + strings.Repeat("!", 32): false, "": false, "login_": false,
	}
	for in, want := range cases {
		if _, ok := ParseBotStart(in); ok != want {
			t.Errorf("%q: %v", in, ok)
		}
	}
}

func TestNewDeviceNotification(t *testing.T) {
	ctx := context.Background()
	e := newBotEnv(t)
	login := func(dev, ua string) LoginResult {
		t.Helper()
		_, err := e.a.StartEmail(ctx, "a@mail.ru", "business", true, "1.1.1.1")
		if err != nil {
			t.Fatal(err)
		}
		cctx := WithClient(ctx, ClientInfo{IP: "203.0.113.7", UserAgent: ua, DeviceID: dev})
		r, err := e.a.VerifyEmail(cctx, "a@mail.ru", e.mail.code("a@mail.ru"))
		if err != nil {
			t.Fatal(err)
		}
		e.clk.add(13 * time.Minute) // лимит: 5 писем в час на почту
		return r
	}
	chrome := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"
	login("dev-1", chrome) // регистрация — без уведомления
	if e.notes.count() != 0 {
		t.Fatal("регистрация")
	}
	login("dev-1", chrome) // то же устройство
	if e.notes.count() != 0 {
		t.Fatal("то же устройство")
	}
	login("dev-2", chrome) // новое
	if e.notes.count() != 1 {
		t.Fatal("новое устройство")
	}
	n := e.notes.got[0]
	if n.Kind != NotifyNewLogin || n.Method != "email" || n.Device != "Chrome, Windows" || n.IP != "203.0.113.x" || !n.At.Equal(e.clk.Now().Add(-13*time.Minute)) {
		t.Errorf("%+v", n)
	}
	// ошибки устройства и уведомления не мешают входу
	e.tg.fail["TouchDevice"] = errBoom
	login("dev-3", chrome)
	delete(e.tg.fail, "TouchDevice")
	e.notes.err = errBoom
	if r := login("dev-4", chrome); r.Token == "" {
		t.Error("вход должен пройти")
	}
	// без cookie устройства — считаем новым
	e.notes.err = nil
	before := e.notes.count()
	login("", "")
	if e.notes.count() != before+1 || e.notes.got[before].Device != "неизвестное устройство" {
		t.Errorf("%+v", e.notes.got)
	}
}

func TestLogoutEverywhere(t *testing.T) {
	ctx := context.Background()
	e := newBotEnv(t)
	u, _ := e.repo.CreateUserWithIdentity(ctx, domain.NewUser{Name: "A"}, domain.Identity{Provider: "telegram", Subject: "777"})
	for i := 0; i < 3; i++ {
		if _, _, err := e.a.newSession(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := e.a.LogoutEverywhere(ctx, u.ID); err != nil || n != 3 || e.repo.sessionCount(u.ID) != 0 {
		t.Fatal(n, err)
	}
}

func TestCleanupTelegramLogins(t *testing.T) {
	ctx := context.Background()
	e := newBotEnv(t)
	_, _ = e.a.StartTelegramBot(ctx, "", true, "ip")
	e.clk.add(25 * time.Hour)
	_, _ = e.a.StartTelegramBot(ctx, "", true, "ip")
	if err := e.a.Cleanup(ctx); err != nil || len(e.tg.logins) != 1 {
		t.Fatal(err, len(e.tg.logins))
	}
	e.tg.fail["DeleteTelegramLoginsBefore"] = errBoom
	if err := e.a.Cleanup(ctx); !errors.Is(err, errBoom) {
		t.Error(err)
	}
}

func TestDescribeUAAndMaskIP(t *testing.T) {
	ua := map[string]string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1": "Safari, iPhone",
		"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36 YaBrowser/24.1":                 "Яндекс Браузер, Android",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0; rv:131.0) Gecko/20100101 Firefox/131.0":                                                     "Firefox, macOS",
		"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36 Edg/130.0":                               "Edge, Windows",
		"curl/8.0": "неизвестное устройство", "Firefox/1": "Firefox", "Linux": "Linux",
	}
	for in, want := range ua {
		if got := DescribeUA(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	for in, want := range map[string]string{"203.0.113.7": "203.0.113.x", "2001:db8:85a3:1::1": "2001:db8:85a3::x", "nope": "", "::1": "0:0:0::x"} {
		if got := MaskIP(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

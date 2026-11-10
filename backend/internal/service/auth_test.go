package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
)

var t0 = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

type env struct {
	a    *Auth
	repo *memRepo
	mail *fakeMailer
	clk  *fakeClock
	g    *fakeGoogle
}

func newEnv(t *testing.T, mut ...func(*AuthDeps, *AuthConfig)) env {
	t.Helper()
	e := env{repo: newMem(), mail: &fakeMailer{}, clk: &fakeClock{t: t0}, g: &fakeGoogle{}}
	d := AuthDeps{Users: e.repo, Codes: e.repo, Sessions: e.repo, Mailer: e.mail, Google: e.g, Clock: e.clk,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c := AuthConfig{Secret: []byte("test-secret-test-secret-test-secret"), EmailEnabled: true, TelegramToken: "123456:TEST-token"}
	for _, m := range mut {
		m(&d, &c)
	}
	e.a = NewAuth(d, c)
	return e
}

func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"  Anna@Mail.RU ", "anna@mail.ru", true},
		{"a.b+c@sub.example.com", "a.b+c@sub.example.com", true},
		{"", "", false},
		{"anna", "", false},
		{"anna@localhost", "", false},
		{"Anna <anna@mail.ru>", "", false},
		{"@mail.ru", "", false},
		{strings.Repeat("a", 250) + "@x.ru", "", false},
	}
	for _, c := range cases {
		got, err := NormalizeEmail(c.in)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("%q → %q, %v", c.in, got, err)
		}
	}
}

func TestStartEmail(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		prep    func(e env)
		email   string
		consent bool
		ip      string
		reason  string // ожидаемая причина лимита
		field   string // ожидаемое поле ошибки
		err     error
	}{
		{name: "успех", email: "Anna@Mail.ru", consent: true, ip: "1.1.1.1"},
		{name: "без согласия", email: "a@mail.ru", ip: "1.1.1.1", field: "consent"},
		{name: "плохая почта", email: "anna", consent: true, ip: "1.1.1.1", field: "email"},
		{name: "повтор раньше 60 с", email: "a@mail.ru", consent: true, ip: "1.1.1.1", reason: "email_cooldown",
			prep: func(e env) { mustStart(t, e, "a@mail.ru", "2.2.2.2"); e.clk.add(30 * time.Second) }},
		{name: "6-е письмо за час на почту", email: "a@mail.ru", consent: true, ip: "9.9.9.9", reason: "email_hourly",
			prep: func(e env) {
				for i := 0; i < 5; i++ {
					mustStart(t, e, "a@mail.ru", "3.3.3."+strconv.Itoa(i))
					e.clk.add(61 * time.Second)
				}
			}},
		{name: "21-е письмо за час с адреса", email: "new@mail.ru", consent: true, ip: "4.4.4.4", reason: "ip_hourly",
			prep: func(e env) {
				for i := 0; i < 20; i++ {
					mustStart(t, e, "u"+strconv.Itoa(i)+"@mail.ru", "4.4.4.4")
				}
			}},
		{name: "почта выключена", email: "a@mail.ru", consent: true, ip: "1.1.1.1", err: ErrProviderDisabled,
			prep: func(e env) { e.a.cfg.EmailEnabled = false }},
		{name: "очередь писем недоступна", email: "a@mail.ru", consent: true, ip: "1.1.1.1", err: ErrMailUnavailable,
			prep: func(e env) { e.mail.err = errBoom }},
		{name: "ошибка базы", email: "a@mail.ru", consent: true, ip: "1.1.1.1", err: errBoom,
			prep: func(e env) { e.repo.fail["CodeStatsByEmail"] = errBoom }},
		{name: "ошибка базы (адрес)", email: "a@mail.ru", consent: true, ip: "1.1.1.1", err: errBoom,
			prep: func(e env) { e.repo.fail["CodeStatsByIP"] = errBoom }},
		{name: "ошибка записи кода", email: "a@mail.ru", consent: true, ip: "1.1.1.1", err: errBoom,
			prep: func(e env) { e.repo.fail["CreateCode"] = errBoom }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			if c.prep != nil {
				c.prep(e)
			}
			sent, err := e.a.StartEmail(ctx, c.email, c.consent, c.ip)
			var le *LimitError
			var ve *ValidationError
			switch {
			case c.reason != "":
				if !errors.As(err, &le) || le.Reason != c.reason || le.RetryAfter <= 0 {
					t.Fatalf("ждали лимит %s, получили %v", c.reason, err)
				}
			case c.field != "":
				if !errors.As(err, &ve) || ve.Fields[c.field] == "" {
					t.Fatalf("ждали ошибку поля %s, получили %v", c.field, err)
				}
			case c.err != nil:
				if !errors.Is(err, c.err) {
					t.Fatalf("ждали %v, получили %v", c.err, err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				if sent.Email != "anna@mail.ru" || sent.ResendAfter != time.Minute || sent.ExpiresIn != 10*time.Minute {
					t.Errorf("ответ: %+v", sent)
				}
				code := e.mail.code("anna@mail.ru")
				if !isDigits(code, 6) {
					t.Fatalf("код %q", code)
				}
				// в базе только HMAC, не сам код
				stored := e.repo.codes[0].CodeHash
				if bytes.Contains(stored, []byte(code)) || !hmac.Equal(stored, e.a.codeHash("anna@mail.ru", code)) {
					t.Error("в базе должен быть только HMAC кода")
				}
			}
		})
	}
}

func TestEmailCooldownPasses(t *testing.T) {
	e := newEnv(t)
	mustStart(t, e, "a@mail.ru", "1.1.1.1")
	first := e.mail.code("a@mail.ru")
	e.clk.add(61 * time.Second)
	mustStart(t, e, "a@mail.ru", "1.1.1.1")
	// прежний код погашен: работает только новый
	if _, err := e.a.VerifyEmail(context.Background(), "a@mail.ru", first); err == nil && first != e.mail.code("a@mail.ru") {
		t.Fatal("старый код не должен подходить")
	}
}

func mustStart(t *testing.T, e env, email, ip string) {
	t.Helper()
	if _, err := e.a.StartEmail(context.Background(), email, true, ip); err != nil {
		t.Fatalf("StartEmail(%s): %v", email, err)
	}
}

func wrong(code string) string {
	if code == "000000" {
		return "111111"
	}
	return "000000"
}

func TestVerifyEmail(t *testing.T) {
	ctx := context.Background()
	t.Run("успех: новый пользователь в режиме «Бизнес» и сессия", func(t *testing.T) {
		e := newEnv(t)
		if _, err := e.a.StartEmail(ctx, "a@mail.ru", true, "1.1.1.1"); err != nil {
			t.Fatal(err)
		}
		res, err := e.a.VerifyEmail(ctx, " A@mail.ru ", e.mail.code("a@mail.ru"))
		if err != nil {
			t.Fatal(err)
		}
		if !res.Created || res.User.ActiveMode != "business" || res.User.Email != "a@mail.ru" || res.Token == "" {
			t.Fatalf("%+v", res)
		}
		if !res.ExpiresAt.Equal(t0.Add(30 * 24 * time.Hour)) {
			t.Errorf("срок сессии %v", res.ExpiresAt)
		}
		u, err := e.a.Session(ctx, res.Token)
		if err != nil || u.ID != res.User.ID {
			t.Fatalf("сессия: %v %+v", err, u)
		}
		// код одноразовый
		if _, err := e.a.VerifyEmail(ctx, "a@mail.ru", e.mail.code("a@mail.ru")); !errors.Is(err, ErrCodeExpired) {
			t.Errorf("повторный код: %v", err)
		}
		// повторный вход — тот же пользователь
		e.clk.add(2 * time.Minute)
		mustStart(t, e, "a@mail.ru", "1.1.1.1")
		res2, err := e.a.VerifyEmail(ctx, "a@mail.ru", e.mail.code("a@mail.ru"))
		if err != nil || res2.User.ID != res.User.ID || res2.Created {
			t.Fatalf("повторный вход: %v %+v", err, res2)
		}
	})

	cases := []struct {
		name string
		run  func(t *testing.T, e env)
	}{
		{"неверный формат кода", func(t *testing.T, e env) {
			var ve *ValidationError
			for _, c := range []string{"", "12345", "1234567", "12a456"} {
				if _, err := e.a.VerifyEmail(ctx, "a@mail.ru", c); !errors.As(err, &ve) || ve.Fields["code"] == "" {
					t.Errorf("%q: %v", c, err)
				}
			}
			if _, err := e.a.VerifyEmail(ctx, "bad", "123456"); !errors.As(err, &ve) {
				t.Errorf("почта: %v", err)
			}
		}},
		{"кода нет", func(t *testing.T, e env) {
			if _, err := e.a.VerifyEmail(ctx, "a@mail.ru", "123456"); !errors.Is(err, ErrCodeExpired) {
				t.Fatal(err)
			}
		}},
		{"код истёк через 10 минут", func(t *testing.T, e env) {
			mustStart(t, e, "a@mail.ru", "1.1.1.1")
			e.clk.add(10*time.Minute + time.Second)
			if _, err := e.a.VerifyEmail(ctx, "a@mail.ru", e.mail.code("a@mail.ru")); !errors.Is(err, ErrCodeExpired) {
				t.Fatal(err)
			}
		}},
		{"5 попыток, затем блок даже для верного кода", func(t *testing.T, e env) {
			mustStart(t, e, "a@mail.ru", "1.1.1.1")
			code := e.mail.code("a@mail.ru")
			for left := 4; left >= 1; left-- {
				var ce *CodeInvalidError
				if _, err := e.a.VerifyEmail(ctx, "a@mail.ru", wrong(code)); !errors.As(err, &ce) || ce.Left != left {
					t.Fatalf("осталось %d: %v", left, err)
				}
			}
			if _, err := e.a.VerifyEmail(ctx, "a@mail.ru", wrong(code)); !errors.Is(err, ErrTooManyAttempts) {
				t.Fatalf("5-я неверная: %v", err)
			}
			if _, err := e.a.VerifyEmail(ctx, "a@mail.ru", code); !errors.Is(err, ErrTooManyAttempts) {
				t.Fatalf("верный после блока: %v", err)
			}
		}},
		{"ошибка базы при входе", func(t *testing.T, e env) {
			mustStart(t, e, "a@mail.ru", "1.1.1.1")
			e.repo.fail["CreateSession"] = errBoom
			if _, err := e.a.VerifyEmail(ctx, "a@mail.ru", e.mail.code("a@mail.ru")); !errors.Is(err, errBoom) {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.run(t, newEnv(t)) })
	}
}

// signTelegram — подпись как у Telegram Login Widget.
func signTelegram(token string, data map[string]string) map[string]string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + data[k]
	}
	secret := sha256.Sum256([]byte(token))
	m := hmac.New(sha256.New, secret[:])
	m.Write([]byte(strings.Join(lines, "\n")))
	out := map[string]string{"hash": hex.EncodeToString(m.Sum(nil))}
	for k, v := range data {
		out[k] = v
	}
	return out
}

func TestVerifyTelegram(t *testing.T) {
	const tok = "123456:TEST-token"
	now := t0
	base := func() map[string]string {
		return map[string]string{"id": "42", "first_name": "Анна", "last_name": "К", "username": "anna",
			"photo_url": "https://t.me/i/a.jpg", "auth_date": strconv.FormatInt(now.Add(-time.Minute).Unix(), 10)}
	}
	cases := []struct {
		name string
		data func() map[string]string
		tok  string
		ok   bool
		err  error
	}{
		{"верная подпись", func() map[string]string { return signTelegram(tok, base()) }, tok, true, nil},
		{"без необязательных полей", func() map[string]string {
			return signTelegram(tok, map[string]string{"id": "42", "first_name": "A", "auth_date": strconv.FormatInt(now.Unix(), 10)})
		}, tok, true, nil},
		{"подпись другим токеном", func() map[string]string { return signTelegram("999:OTHER", base()) }, tok, false, ErrTelegramInvalid},
		{"подменено имя", func() map[string]string {
			d := signTelegram(tok, base())
			d["first_name"] = "Злоумышленник"
			return d
		}, tok, false, ErrTelegramInvalid},
		{"подменён id", func() map[string]string { d := signTelegram(tok, base()); d["id"] = "43"; return d }, tok, false, ErrTelegramInvalid},
		{"старая подпись (2 часа)", func() map[string]string {
			d := base()
			d["auth_date"] = strconv.FormatInt(now.Add(-2*time.Hour).Unix(), 10)
			return signTelegram(tok, d)
		}, tok, false, ErrTelegramInvalid},
		{"из будущего", func() map[string]string {
			d := base()
			d["auth_date"] = strconv.FormatInt(now.Add(time.Hour).Unix(), 10)
			return signTelegram(tok, d)
		}, tok, false, ErrTelegramInvalid},
		{"чужое поле", func() map[string]string { d := base(); d["admin"] = "1"; return signTelegram(tok, d) }, tok, false, ErrTelegramInvalid},
		{"нет hash", func() map[string]string { return base() }, tok, false, ErrTelegramInvalid},
		{"hash не hex", func() map[string]string { d := base(); d["hash"] = strings.Repeat("z", 64); return d }, tok, false, ErrTelegramInvalid},
		{"id не число", func() map[string]string { d := base(); d["id"] = "abc"; return signTelegram(tok, d) }, tok, false, ErrTelegramInvalid},
		{"auth_date не число", func() map[string]string { d := base(); d["auth_date"] = "x"; return signTelegram(tok, d) }, tok, false, ErrTelegramInvalid},
		{"бот не подключён", func() map[string]string { return signTelegram(tok, base()) }, "", false, ErrProviderDisabled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, err := VerifyTelegram(c.data(), c.tok, now, time.Hour)
			if c.ok {
				if err != nil || u.ID != "42" {
					t.Fatalf("%v %+v", err, u)
				}
				return
			}
			if !errors.Is(err, c.err) {
				t.Fatalf("ждали %v, получили %v", c.err, err)
			}
		})
	}
	u, _ := VerifyTelegram(signTelegram(tok, base()), tok, now, time.Hour)
	if u.Name != "Анна К" || u.PhotoURL != "https://t.me/i/a.jpg" {
		t.Errorf("данные: %+v", u)
	}
}

func TestLoginTelegram(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	d := signTelegram("123456:TEST-token", map[string]string{"id": "7", "first_name": "Иван", "auth_date": strconv.FormatInt(t0.Unix(), 10)})
	var ve *ValidationError
	if _, err := e.a.LoginTelegram(ctx, d, false); !errors.As(err, &ve) || ve.Fields["consent"] == "" {
		t.Fatalf("без согласия: %v", err)
	}
	res, err := e.a.LoginTelegram(ctx, d, true)
	if err != nil || !res.Created || res.User.Name != "Иван" || res.User.Email != "" {
		t.Fatalf("%v %+v", err, res)
	}
	res2, err := e.a.LoginTelegram(ctx, d, true)
	if err != nil || res2.User.ID != res.User.ID || res2.Created {
		t.Fatalf("повторный вход — тот же аккаунт: %v %+v", err, res2)
	}
	if m := e.a.Methods(); m.TelegramBotID != "123456" || !m.Email || !m.Google {
		t.Errorf("способы: %+v", m)
	}
}

func TestGoogle(t *testing.T) {
	ctx := context.Background()
	t.Run("start: state и PKCE S256", func(t *testing.T) {
		e := newEnv(t)
		var ve *ValidationError
		if _, err := e.a.StartGoogle(false); !errors.As(err, &ve) {
			t.Fatalf("без согласия: %v", err)
		}
		st, err := e.a.StartGoogle(true)
		if err != nil || st.State == "" || st.Verifier == "" || st.State == st.Verifier {
			t.Fatalf("%v %+v", err, st)
		}
		if !strings.Contains(st.URL, "code_challenge="+pkceChallenge(st.Verifier)) || !strings.Contains(st.URL, "state="+st.State) {
			t.Errorf("адрес: %s", st.URL)
		}
	})
	t.Run("подтверждённая почта Google = почта пользователя → тот же аккаунт", func(t *testing.T) {
		e := newEnv(t)
		mustStart(t, e, "anna@gmail.com", "1.1.1.1")
		first, err := e.a.VerifyEmail(ctx, "anna@gmail.com", e.mail.code("anna@gmail.com"))
		if err != nil {
			t.Fatal(err)
		}
		e.g.p = GoogleProfile{Subject: "g-1", Email: "Anna@Gmail.com", EmailVerified: true, Name: "Anna", Picture: "https://lh3/a.png"}
		res, err := e.a.LoginGoogle(ctx, "code", "verifier")
		if err != nil || res.User.ID != first.User.ID || res.Created {
			t.Fatalf("%v %+v", err, res)
		}
		if e.g.got.verifier != "verifier" {
			t.Error("verifier не передан в обмен")
		}
		if p, _ := e.repo.Providers(ctx, res.User.ID); strings.Join(p, ",") != "email,google" {
			t.Errorf("способы входа: %v", p)
		}
	})
	t.Run("неподтверждённая почта — отдельный аккаунт без почты", func(t *testing.T) {
		e := newEnv(t)
		mustStart(t, e, "anna@gmail.com", "1.1.1.1")
		first, _ := e.a.VerifyEmail(ctx, "anna@gmail.com", e.mail.code("anna@gmail.com"))
		e.g.p = GoogleProfile{Subject: "g-2", Email: "anna@gmail.com", EmailVerified: false}
		res, err := e.a.LoginGoogle(ctx, "code", "v")
		if err != nil || res.User.ID == first.User.ID || res.User.Email != "" {
			t.Fatalf("%v %+v", err, res)
		}
	})
	t.Run("после Google вход по той же почте — тот же аккаунт", func(t *testing.T) {
		e := newEnv(t)
		e.g.p = GoogleProfile{Subject: "g-3", Email: "ivan@gmail.com", EmailVerified: true}
		g, err := e.a.LoginGoogle(ctx, "c", "v")
		if err != nil {
			t.Fatal(err)
		}
		mustStart(t, e, "ivan@gmail.com", "1.1.1.1")
		res, err := e.a.VerifyEmail(ctx, "ivan@gmail.com", e.mail.code("ivan@gmail.com"))
		if err != nil || res.User.ID != g.User.ID {
			t.Fatalf("%v %+v", err, res)
		}
	})
	errCases := []struct {
		name     string
		code, vr string
		p        GoogleProfile
		exErr    error
	}{
		{"пустой код", "", "v", GoogleProfile{Subject: "x"}, nil},
		{"пустой verifier", "c", "", GoogleProfile{Subject: "x"}, nil},
		{"Google отказал", "c", "v", GoogleProfile{}, errBoom},
		{"нет sub", "c", "v", GoogleProfile{Email: "a@b.ru"}, nil},
	}
	for _, c := range errCases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.g.p, e.g.err = c.p, c.exErr
			if _, err := e.a.LoginGoogle(ctx, c.code, c.vr); !errors.Is(err, ErrGoogleFailed) {
				t.Fatal(err)
			}
		})
	}
	t.Run("Google выключен", func(t *testing.T) {
		e := newEnv(t, func(d *AuthDeps, _ *AuthConfig) { d.Google = nil })
		if _, err := e.a.StartGoogle(true); !errors.Is(err, ErrProviderDisabled) {
			t.Fatal(err)
		}
		if _, err := e.a.LoginGoogle(ctx, "c", "v"); !errors.Is(err, ErrProviderDisabled) {
			t.Fatal(err)
		}
		if e.a.Methods().Google {
			t.Error("кнопка Google должна быть скрыта")
		}
	})
}

func TestLoginRaceConflictRetries(t *testing.T) {
	// двойной клик: второй запрос создаёт пользователя одновременно → конфликт → повторный поиск находит первого
	e := newEnv(t)
	e.g.p = GoogleProfile{Subject: "g-9"}
	first, err := e.a.LoginGoogle(context.Background(), "c", "v")
	if err != nil {
		t.Fatal(err)
	}
	r := &conflictOnce{memRepo: e.repo}
	a := NewAuth(AuthDeps{Users: r, Codes: e.repo, Sessions: e.repo, Google: e.g, Clock: e.clk}, AuthConfig{})
	res, err := a.LoginGoogle(context.Background(), "c", "v")
	if err != nil || res.User.ID != first.User.ID {
		t.Fatalf("%v %+v", err, res)
	}
}

// conflictOnce — первый поиск «не находит» (как будто другой запрос ещё не записал), создание даёт конфликт.
type conflictOnce struct {
	*memRepo
	n int
}

func (c *conflictOnce) UserByIdentity(ctx context.Context, p, s string) (domain.User, error) {
	c.n++
	if c.n == 1 {
		return domain.User{}, domain.ErrNotFound
	}
	return c.memRepo.UserByIdentity(ctx, p, s)
}

func TestSessionAndLogout(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustStart(t, e, "a@mail.ru", "1.1.1.1")
	res, _ := e.a.VerifyEmail(ctx, "a@mail.ru", e.mail.code("a@mail.ru"))
	for _, tok := range []string{"", "nope", strings.Repeat("x", 200)} {
		if _, err := e.a.Session(ctx, tok); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%q: %v", tok, err)
		}
	}
	if _, ok := e.repo.sessions[string(res.Token)]; ok {
		t.Error("в базе должен быть хеш токена, а не токен")
	}
	if err := e.a.Logout(ctx, res.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := e.a.Session(ctx, res.Token); !errors.Is(err, ErrUnauthorized) {
		t.Error("после выхода сессия должна пропасть")
	}
	if err := e.a.Logout(ctx, ""); err != nil {
		t.Error(err)
	}
	// истечение сессии
	mustStart(t, e, "b@mail.ru", "1.1.1.1")
	res2, _ := e.a.VerifyEmail(ctx, "b@mail.ru", e.mail.code("b@mail.ru"))
	e.clk.add(31 * 24 * time.Hour)
	if _, err := e.a.Session(ctx, res2.Token); !errors.Is(err, ErrUnauthorized) {
		t.Error("истёкшая сессия должна быть недействительна")
	}
}

func TestCleanup(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustStart(t, e, "a@mail.ru", "1.1.1.1")
	res, _ := e.a.VerifyEmail(ctx, "a@mail.ru", e.mail.code("a@mail.ru"))
	e.clk.add(31 * 24 * time.Hour)
	if err := e.a.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.repo.codes) != 0 || len(e.repo.sessions) != 0 {
		t.Errorf("коды %d, сессии %d", len(e.repo.codes), len(e.repo.sessions))
	}
	_ = res
	e.repo.fail["DeleteCodesBefore"] = errBoom
	if err := e.a.Cleanup(ctx); !errors.Is(err, errBoom) {
		t.Error(err)
	}
	e.repo.fail["DeleteCodesBefore"] = nil
	e.repo.fail["DeleteExpiredSessions"] = errBoom
	if err := e.a.Cleanup(ctx); !errors.Is(err, errBoom) {
		t.Error(err)
	}
}

func TestRunCleanupStops(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.a.RunCleanup(ctx, time.Millisecond); close(done) }()
	time.Sleep(5 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("фоновая очистка не остановилась по контексту")
	}
}

func TestRandom(t *testing.T) {
	for i := 0; i < 200; i++ {
		c, err := randomDigits(cryptoRand, 6)
		if err != nil || !isDigits(c, 6) {
			t.Fatalf("%q %v", c, err)
		}
	}
	// байты ≥ 250 отбрасываются (без перекоса к 0–5)
	c, err := randomDigits(io.MultiReader(bytes.NewReader(bytes.Repeat([]byte{255}, 12)), fixedRand(3)), 6)
	if err != nil || c != "333333" {
		t.Fatalf("%q %v", c, err)
	}
	if _, err := randomDigits(bytes.NewReader(nil), 6); err == nil {
		t.Error("пустой источник")
	}
	if _, err := randomToken(bytes.NewReader(nil)); err == nil {
		t.Error("пустой источник токена")
	}
	if MaskEmail("anna@mail.ru") != "a***@mail.ru" || MaskEmail("x") != "***" {
		t.Error("маска почты")
	}
	if telegramBotID("123:abc") != "123" || telegramBotID("abc:1") != "" || telegramBotID("") != "" {
		t.Error("id бота")
	}
}

func TestHealth(t *testing.T) {
	if err := NewHealth(pingErr{}).Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := NewHealth(pingErr{errBoom}).Check(context.Background()); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

type pingErr struct{ err error }

func (p pingErr) Ping(context.Context) error { return p.err }

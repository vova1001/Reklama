package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

const testOrigin = "https://ohvat.test"
const testCSRF = "csrf-token-csrf-token-csrf-token-csrf-token" // 43 символа, как настоящий

// fakeAuth — подставной сервис входа: err — ошибка для любого метода, last — что передали.
type fakeAuth struct {
	mu        sync.Mutex
	err       error
	user      domain.User
	methods   service.Methods
	last      map[string]any
	botStatus service.BotLoginStatus
}

func (f *fakeAuth) remember(k string, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.last == nil {
		f.last = map[string]any{}
	}
	f.last[k] = v
}

func (f *fakeAuth) login() (service.LoginResult, error) {
	if f.err != nil {
		return service.LoginResult{}, f.err
	}
	return service.LoginResult{Token: "tok", ExpiresAt: time.Now().Add(time.Hour), User: f.user}, nil
}

func (f *fakeAuth) Methods() service.Methods { return f.methods }
func (f *fakeAuth) StartEmail(_ context.Context, email string, consent bool, ip string) (service.CodeSent, error) {
	f.remember("ip", ip)
	f.remember("consent", consent)
	if f.err != nil {
		return service.CodeSent{}, f.err
	}
	return service.CodeSent{Email: email, ResendAfter: time.Minute, ExpiresIn: 10 * time.Minute}, nil
}
func (f *fakeAuth) VerifyEmail(context.Context, string, string) (service.LoginResult, error) {
	return f.login()
}
func (f *fakeAuth) LoginTelegram(_ context.Context, d map[string]string, consent bool) (service.LoginResult, error) {
	f.remember("tg", d)
	return f.login()
}
func (f *fakeAuth) StartGoogle(consent bool) (service.GoogleStart, error) {
	if f.err != nil {
		return service.GoogleStart{}, f.err
	}
	return service.GoogleStart{URL: "https://accounts.example/auth", State: "st", Verifier: "ver"}, nil
}
func (f *fakeAuth) LoginGoogle(_ context.Context, code, verifier string) (service.LoginResult, error) {
	f.remember("google", []string{code, verifier})
	return f.login()
}
func (f *fakeAuth) Session(_ context.Context, tok string) (domain.User, error) {
	if tok != "good" {
		return domain.User{}, service.ErrUnauthorized
	}
	return f.user, nil
}
func (f *fakeAuth) Logout(_ context.Context, tok string) error {
	f.remember("logout", tok)
	return f.err
}
func (f *fakeAuth) LogoutEverywhere(_ context.Context, uid int64) (int64, error) {
	f.remember("logoutAll", uid)
	return 3, f.err
}

func (f *fakeAuth) StartTelegramBot(ctx context.Context, consent bool, ip string) (service.TelegramBotStart, error) {
	f.remember("botstart", []any{consent, ip})
	f.remember("client", clientInfoOf(ctx))
	if f.err != nil {
		return service.TelegramBotStart{}, f.err
	}
	return service.TelegramBotStart{URL: "https://t.me/reklama_auth_bot?start=login_x", BrowserSecret: "browser-secret", ExpiresIn: 5 * time.Minute}, nil
}

func (f *fakeAuth) WaitTelegramBot(ctx context.Context, secret string, wait time.Duration) (service.BotLoginStatus, service.LoginResult, error) {
	f.remember("botwait", []any{secret, wait})
	f.mu.Lock()
	st := f.botStatus
	f.mu.Unlock()
	if f.err != nil {
		return "", service.LoginResult{}, f.err
	}
	if st == service.BotLoginDone {
		res, _ := f.login()
		return st, res, nil
	}
	return service.BotLoginWaiting, service.LoginResult{}, nil
}

func newSrv(home HomeService, health HealthService) *Server {
	return New(Services{Home: home, Health: health, Auth: &fakeAuth{}, Profiles: &fakeProfiles{}}, Options{Origin: testOrigin, SecureCookies: true})
}

func authSrv(a *fakeAuth) *Server {
	return New(Services{Home: fakeHome{}, Health: fakeDB{}, Auth: a, Profiles: &fakeProfiles{}}, Options{Origin: testOrigin, SecureCookies: true, AuthLimiter: newMemLimiter(100, 100)})
}

func withCSRF(r *http.Request) {
	r.AddCookie(&http.Cookie{Name: csrfCookie, Value: testCSRF})
	r.Header.Set(csrfHeader, testCSRF)
	r.Header.Set("Origin", testOrigin)
}

func serve(s *Server, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, r)
	return rec
}

func post(s *Server, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	withCSRF(r)
	return serve(s, r)
}

func errOf(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var e errorDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("не JSON ошибки: %s", rec.Body.String())
	}
	return e.Error
}

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestAuthConfigSetsCSRF(t *testing.T) {
	a := &fakeAuth{methods: service.Methods{Email: true, TelegramBotID: "123"}}
	rec := serve(authSrv(a), httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	var cfg authConfigDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &cfg)
	c := cookie(rec, csrfCookie)
	if c == nil || c.Value != cfg.CSRFToken || len(c.Value) != 43 || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie CSRF: %+v, тело %s", c, rec.Body.String())
	}
	if !cfg.Email || cfg.Google || cfg.Telegram == nil || cfg.Telegram.BotID != "123" {
		t.Errorf("способы: %s", rec.Body.String())
	}
	// уже есть cookie — тот же токен
	r := httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)
	r.AddCookie(&http.Cookie{Name: csrfCookie, Value: testCSRF})
	rec = serve(authSrv(a), r)
	_ = json.Unmarshal(rec.Body.Bytes(), &cfg)
	if cfg.CSRFToken != testCSRF || cfg.Telegram == nil {
		t.Errorf("токен должен сохраниться: %s", rec.Body.String())
	}
	a.methods.TelegramBotID = ""
	rec = serve(authSrv(a), httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))
	if !strings.Contains(rec.Body.String(), `"telegram":null`) {
		t.Errorf("Telegram выключен → null: %s", rec.Body.String())
	}
}

func TestCSRF(t *testing.T) {
	body := `{"email":"a@mail.ru","consent":true}`
	cases := []struct {
		name string
		prep func(r *http.Request)
		code int
	}{
		{"всё верно", withCSRF, http.StatusAccepted},
		{"без заголовка", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: csrfCookie, Value: testCSRF}) }, 403},
		{"без cookie", func(r *http.Request) { r.Header.Set(csrfHeader, testCSRF) }, 403},
		{"не совпадают", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: csrfCookie, Value: testCSRF})
			r.Header.Set(csrfHeader, "other")
		}, 403},
		{"чужой Origin", func(r *http.Request) { withCSRF(r); r.Header.Set("Origin", "https://evil.example") }, 403},
		{"без Origin (старый браузер) — по токену", func(r *http.Request) { withCSRF(r); r.Header.Del("Origin") }, http.StatusAccepted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/auth/email/start", strings.NewReader(body))
			c.prep(r)
			rec := serve(authSrv(&fakeAuth{}), r)
			if rec.Code != c.code {
				t.Fatalf("ждали %d, получили %d %s", c.code, rec.Code, rec.Body.String())
			}
			if c.code == 403 && errOf(t, rec).Code != "csrf" {
				t.Error("код ошибки csrf")
			}
		})
	}
}

func TestEmailStart(t *testing.T) {
	a := &fakeAuth{}
	r := httptest.NewRequest(http.MethodPost, "/api/auth/email/start", strings.NewReader(`{"email":"a@mail.ru","consent":true}`))
	withCSRF(r)
	r.Header.Set("X-Real-IP", "203.0.113.7")
	rec := serve(authSrv(a), r)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"resendAfter":60`) || !strings.Contains(rec.Body.String(), `"expiresIn":600`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if a.last["ip"] != "203.0.113.7" || a.last["consent"] != true {
		t.Errorf("ip/согласие: %v", a.last)
	}
	// лишние поля и мусор — 400
	for _, b := range []string{`{"email":"a@mail.ru","admin":true}`, `{"email":"a@mail.ru","role":"business","consent":true}`, `not json`, `{"email":` + strings.Repeat(`"x"`, 1) + strings.Repeat(" ", maxBody) + `}`} {
		if rec := post(authSrv(a), "/api/auth/email/start", b); rec.Code != 400 || errOf(t, rec).Code != "bad_request" {
			t.Errorf("%.30q: %d", b, rec.Code)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		err        error
		status     int
		code       string
		retryAfter string
	}{
		{&service.ValidationError{Fields: map[string]string{"email": "x"}}, 400, "validation", ""},
		{&service.LimitError{Reason: "email_cooldown", RetryAfter: 42 * time.Second}, 429, "rate_limited", "42"},
		{&service.LimitError{Reason: "email_hourly", RetryAfter: 10 * time.Minute}, 429, "rate_limited", "600"},
		{&service.LimitError{Reason: "ip_hourly", RetryAfter: 0}, 429, "rate_limited", "1"},
		{&service.CodeInvalidError{Left: 3}, 400, "code_invalid", ""},
		{service.ErrCodeExpired, 400, "code_expired", ""},
		{service.ErrTooManyAttempts, 429, "too_many_attempts", ""},
		{service.ErrTelegramInvalid, 400, "telegram_invalid", ""},
		{service.ErrProviderDisabled, 503, "provider_disabled", ""},
		{service.ErrMailUnavailable, 503, "mail_unavailable", ""},
		{service.ErrUnauthorized, 401, "unauthorized", ""},
		{errors.New("pq: secret internals"), 500, "internal", ""},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			rec := post(authSrv(&fakeAuth{err: c.err}), "/api/auth/email/verify", `{"email":"a@mail.ru","code":"123456"}`)
			e := errOf(t, rec)
			if rec.Code != c.status || e.Code != c.code || e.Message == "" {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("Retry-After") != c.retryAfter {
				t.Errorf("Retry-After %q", rec.Header().Get("Retry-After"))
			}
			if strings.Contains(rec.Body.String(), "secret") {
				t.Error("внутренности наружу")
			}
		})
	}
	rec := post(authSrv(&fakeAuth{err: &service.CodeInvalidError{Left: 2}}), "/api/auth/email/verify", `{"email":"a","code":"1"}`)
	if e := errOf(t, rec); e.AttemptsLeft == nil || *e.AttemptsLeft != 2 {
		t.Errorf("attemptsLeft: %s", rec.Body.String())
	}
	rec = post(authSrv(&fakeAuth{err: &service.ValidationError{Fields: map[string]string{"code": "6 цифр"}}}), "/api/auth/email/verify", `{}`)
	if e := errOf(t, rec); e.Fields["code"] != "6 цифр" {
		t.Errorf("fields: %s", rec.Body.String())
	}
}

func TestVerifySetsSessionCookie(t *testing.T) {
	a := &fakeAuth{user: domain.User{ID: 5, ActiveMode: "creator", Name: "Анна"}}
	rec := post(authSrv(a), "/api/auth/email/verify", `{"email":"a@mail.ru","code":"123456"}`)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	c := cookie(rec, sessionCookie)
	if c == nil || c.Value != "tok" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge <= 0 {
		t.Fatalf("cookie сессии: %+v", c)
	}
	var out loginDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	// сразу в приложение — профиль не обязателен
	if out.Next != "/app" || out.User.Mode != "creator" || out.User.Email != nil || out.User.Onboarded {
		t.Errorf("ответ: %s", rec.Body.String())
	}
}

func TestTelegram(t *testing.T) {
	a := &fakeAuth{user: domain.User{ID: 1}}
	rec := post(authSrv(a), "/api/auth/telegram", `{"consent":true,"user":{"id":42,"first_name":"Анна","auth_date":1700000000,"hash":"ab"}}`)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	d := a.last["tg"].(map[string]string)
	if d["id"] != "42" || d["auth_date"] != "1700000000" || d["first_name"] != "Анна" {
		t.Errorf("данные Telegram как строки: %v", d)
	}
	for _, bad := range []string{`{"user":{"id":{"x":1}}}`, `{"user":{"id":1.5}}`, `{"user":{"id":true}}`} {
		if rec := post(authSrv(a), "/api/auth/telegram", bad); rec.Code != 400 || errOf(t, rec).Code != "telegram_invalid" {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body.String())
		}
	}
}

func TestGoogleFlow(t *testing.T) {
	a := &fakeAuth{user: domain.User{ID: 1}}
	rec := serve(authSrv(a), httptest.NewRequest(http.MethodGet, "/api/auth/google/start?consent=1", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://accounts.example/auth" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	oc := cookie(rec, oauthCookie)
	if oc == nil || oc.Value != "st.ver" || !oc.HttpOnly || !oc.Secure || oc.Path != "/api/auth/google" || oc.MaxAge != 600 {
		t.Fatalf("cookie OAuth: %+v", oc)
	}
	cb := func(q string, c *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?"+q, nil)
		if c != nil {
			r.AddCookie(c)
		}
		return serve(authSrv(a), r)
	}
	cases := []struct {
		name, q string
		c       *http.Cookie
		loc     string
	}{
		{"успех → приложение", "code=abc&state=st", oc, "/app"},
		{"cookie прежней версии (с ролью)", "code=abc&state=st", &http.Cookie{Name: oauthCookie, Value: "st.ver.business"}, "/app"},
		{"state не совпал", "code=abc&state=evil", oc, "/login?error=google_state"},
		{"нет cookie", "code=abc&state=st", nil, "/login?error=google_state"},
		{"отказ в Google", "error=access_denied&state=st", oc, "/login?error=google_cancel"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := cb(c.q, c.c)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != c.loc {
				t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
			}
			if gone := cookie(rec, oauthCookie); gone == nil || gone.MaxAge >= 0 {
				t.Error("cookie OAuth должна стираться")
			}
		})
	}
	if g := a.last["google"].([]string); g[0] != "abc" || g[1] != "ver" {
		t.Errorf("в сервис: %v", g)
	}
	if s := cookie(cb("code=abc&state=st", oc), sessionCookie); s == nil || s.Value != "tok" {
		t.Error("после Google — cookie сессии")
	}
	// ошибки
	a.err = service.ErrGoogleFailed
	if rec := cb("code=abc&state=st", oc); rec.Header().Get("Location") != "/login?error=google" {
		t.Errorf("сбой обмена: %s", rec.Header().Get("Location"))
	}
	a.err = errors.New("db")
	if rec := cb("code=abc&state=st", oc); rec.Header().Get("Location") != "/login?error=google" {
		t.Errorf("сбой базы: %s", rec.Header().Get("Location"))
	}
	for err, loc := range map[error]string{
		service.ErrProviderDisabled:                           "/login?error=google_off",
		&service.ValidationError{Fields: map[string]string{}}: "/login?error=consent",
		errors.New("rand"):                                    "/login?error=google",
	} {
		a.err = err
		if rec := serve(authSrv(a), httptest.NewRequest(http.MethodGet, "/api/auth/google/start", nil)); rec.Header().Get("Location") != loc {
			t.Errorf("%v → %s", err, rec.Header().Get("Location"))
		}
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	if clientIP(r) != "10.0.0.1" {
		t.Error(clientIP(r))
	}
	r.Header.Set("X-Real-IP", "not-an-ip")
	if clientIP(r) != "10.0.0.1" {
		t.Error("мусор в X-Real-IP игнорируется")
	}
	r.Header.Set("X-Real-IP", "2001:db8::1")
	if clientIP(r) != "2001:db8::1" {
		t.Error(clientIP(r))
	}
	r.Header.Set("X-Forwarded-For", "6.6.6.6")
	r.Header.Del("X-Real-IP")
	if clientIP(r) != "10.0.0.1" {
		t.Error("X-Forwarded-For не доверяем")
	}
	r.RemoteAddr = "bad"
	if clientIP(r) != "bad" {
		t.Error(clientIP(r))
	}
}

func TestRequestIDHeader(t *testing.T) {
	rec := serve(authSrv(&fakeAuth{}), httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("нет X-Request-Id")
	}
	_ = url.Values{}
}

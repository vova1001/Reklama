package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// clientInfoOf — что middleware положило в контекст (через сервисный помощник).
func clientInfoOf(ctx context.Context) service.ClientInfo { return service.ClientFromContext(ctx) }

func TestBotStart(t *testing.T) {
	a := &fakeAuth{}
	r := httptest.NewRequest(http.MethodPost, "/api/auth/telegram/bot/start", strings.NewReader(`{"consent":true}`))
	withCSRF(r)
	r.Header.Set("X-Real-IP", "203.0.113.5")
	r.Header.Set("User-Agent", "Firefox/131")
	rec := serve(authSrv(a), r)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var out botStartDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.URL != "https://t.me/reklama_auth_bot?start=login_x" || out.ExpiresIn != 300 {
		t.Errorf("%+v", out)
	}
	c := cookie(rec, botCookie)
	if c == nil || c.Value != "browser-secret" || c.Path != "/api/auth/telegram/bot" || !c.HttpOnly || !c.Secure || c.MaxAge != 420 {
		t.Fatalf("cookie: %+v", c)
	}
	if strings.Contains(rec.Body.String(), "browser-secret") {
		t.Error("секрет браузера не должен быть в теле ответа")
	}
	got := a.last["botstart"].([]any)
	if got[0] != true || got[1] != "203.0.113.5" {
		t.Errorf("%v", got)
	}
	// cookie устройства ставится на /api/auth/*, сведения о клиенте — в контексте сервиса
	dev := cookie(rec, deviceCookie)
	ci := a.last["client"].(service.ClientInfo)
	if dev == nil || len(dev.Value) != 43 || !dev.HttpOnly || ci.DeviceID != dev.Value || ci.IP != "203.0.113.5" || ci.UserAgent != "Firefox/131" {
		t.Errorf("устройство: %+v %+v", dev, ci)
	}
	// уже есть — не меняется
	r = httptest.NewRequest(http.MethodPost, "/api/auth/telegram/bot/start", strings.NewReader(`{"consent":true}`))
	withCSRF(r)
	r.AddCookie(&http.Cookie{Name: deviceCookie, Value: strings.Repeat("d", 43)})
	rec = serve(authSrv(a), r)
	if cookie(rec, deviceCookie) != nil || a.last["client"].(service.ClientInfo).DeviceID != strings.Repeat("d", 43) {
		t.Error("cookie устройства перевыпущена")
	}
	// без CSRF — 403; ошибки сервиса — по общей карте
	r = httptest.NewRequest(http.MethodPost, "/api/auth/telegram/bot/start", strings.NewReader(`{}`))
	if rec := serve(authSrv(a), r); rec.Code != 403 {
		t.Error(rec.Code)
	}
	for err, code := range map[error]int{
		service.ErrProviderDisabled:                                         503,
		&service.LimitError{Reason: "ip_hourly", RetryAfter: time.Minute}:   429,
		&service.ValidationError{Fields: map[string]string{"consent": "x"}}: 400,
	} {
		rec := post(authSrv(&fakeAuth{err: err}), "/api/auth/telegram/bot/start", `{"consent":true}`)
		if rec.Code != code {
			t.Errorf("%v: %d", err, rec.Code)
		}
	}
	if rec := post(authSrv(a), "/api/auth/telegram/bot/start", `{"x":1}`); rec.Code != 400 {
		t.Error("лишнее поле", rec.Code)
	}
}

func TestBotStatus(t *testing.T) {
	withBot := func(r *http.Request) {
		withCSRF(r)
		r.AddCookie(&http.Cookie{Name: botCookie, Value: "browser-secret"})
	}
	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/telegram/bot/status", nil)
		withBot(r)
		return r
	}
	// ждём
	a := &fakeAuth{}
	srv := New(Services{Home: fakeHome{}, Health: fakeDB{}, Auth: a, Profiles: &fakeProfiles{}}, Options{Origin: testOrigin, SecureCookies: true, BotWait: 3 * time.Second})
	rec := serve(srv, req())
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"status":"pending"}` || cookie(rec, sessionCookie) != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if got := a.last["botwait"].([]any); got[0] != "browser-secret" || got[1] != 3*time.Second {
		t.Errorf("%v", got)
	}
	// подтверждено — сессия, cookie входа стёрта
	now := time.Now()
	a.user = domain.User{ID: 1, Name: "Анна", ActiveMode: "business", OnboardedAt: &now}
	a.botStatus = service.BotLoginDone
	rec = serve(srv, req())
	var out botStatusDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || out.Status != "ok" || out.Next != "/app" || out.User == nil || out.User.Name != "Анна" {
		t.Fatalf("%s", rec.Body.String())
	}
	if s := cookie(rec, sessionCookie); s == nil || s.Value != "tok" || !s.HttpOnly {
		t.Error("сессия", s)
	}
	if c := cookie(rec, botCookie); c == nil || c.MaxAge >= 0 {
		t.Error("cookie входа должна стереться", c)
	}
	// нет cookie / истекла / отменена — 410
	r := httptest.NewRequest(http.MethodPost, "/api/auth/telegram/bot/status", nil)
	withCSRF(r)
	if rec := serve(srv, r); rec.Code != 410 || errOf(t, rec).Code != "login_expired" {
		t.Error(rec.Code, rec.Body.String())
	}
	for err, code := range map[error]string{service.ErrBotLoginExpired: "login_expired", service.ErrBotLoginCancelled: "login_cancelled"} {
		rec := serve(authSrv(&fakeAuth{err: err}), req())
		if rec.Code != 410 || errOf(t, rec).Code != code {
			t.Error(err, rec.Code)
		}
		if c := cookie(rec, botCookie); c == nil || c.MaxAge >= 0 {
			t.Error("cookie входа стирается", err)
		}
	}
	// закрыли страницу — ответа нет
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec = httptest.NewRecorder()
	authSrv(&fakeAuth{}).Routes().ServeHTTP(rec, req().WithContext(ctx))
	if rec.Body.Len() != 0 {
		t.Error("ответ закрытой странице", rec.Body.String())
	}
}

// Вебхук бота — вне CSRF и вне лимитов /api по адресу (у него свой); без бота маршрута нет.
func TestWebhookMount(t *testing.T) {
	var hit int
	hook := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hit++; w.WriteHeader(200) })
	srv := New(Services{Home: fakeHome{}, Health: fakeDB{}, Auth: &fakeAuth{}, Profiles: &fakeProfiles{}}, Options{Origin: testOrigin, TelegramWebhook: hook,
		APILimiter: newMemLimiter(0.001, 1), AuthLimiter: newMemLimiter(0.001, 1)})
	for i := 0; i < 5; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/telegram/webhook", strings.NewReader(`{}`))
		r.Header.Set("Origin", "https://evil.example")
		if rec := serve(srv, r); rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	if hit != 5 {
		t.Error(hit)
	}
	// остальное /api по-прежнему за CSRF
	r := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	if rec := serve(srv, r); rec.Code != 403 {
		t.Error(rec.Code)
	}
	if rec := serve(authSrv(&fakeAuth{}), httptest.NewRequest(http.MethodPost, "/api/telegram/webhook", nil)); rec.Code != 403 && rec.Code != 404 {
		t.Error("без бота", rec.Code)
	}
}

func TestAuthConfigBotFlag(t *testing.T) {
	a := &fakeAuth{methods: service.Methods{TelegramBotID: "1", TelegramBotName: "reklama_auth_bot", TelegramBot: true}}
	rec := serve(authSrv(a), httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))
	if !strings.Contains(rec.Body.String(), `"telegram":{"botId":"1","botName":"reklama_auth_bot","bot":true}`) {
		t.Error(rec.Body.String())
	}
	if cookie(rec, deviceCookie) == nil {
		t.Error("cookie устройства на /api/auth/config")
	}
}

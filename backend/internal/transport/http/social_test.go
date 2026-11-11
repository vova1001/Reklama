package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

type fakeSocial struct {
	err    error
	last   service.SocialCallback
	lastP  domain.SocialPlatform
	userID int64
}

func (f *fakeSocial) Overview(_ context.Context, u domain.User) ([]service.PlatformView, error) {
	n := int64(1200)
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.FixedZone("X", 3600))
	return []service.PlatformView{
		{Platform: domain.PlatformTelegram, Available: true, Method: domain.MethodBot, Status: domain.SocialVerified,
			Accounts: []domain.SocialAccount{{ID: 5, Kind: domain.SocialGroup, Title: "Чат", Status: domain.SocialVerified,
				Stats: domain.SocialStats{Followers: &n}, RefreshedAt: &at}}},
		{Platform: domain.PlatformVK, Method: domain.MethodOAuth, Status: domain.SocialUnverified},
	}, f.err
}

func (f *fakeSocial) Start(_ context.Context, u domain.User, p domain.SocialPlatform) (service.SocialStart, error) {
	f.lastP, f.userID = p, u.ID
	if f.err != nil {
		return service.SocialStart{}, f.err
	}
	if p == domain.PlatformTelegram {
		return service.SocialStart{URL: "https://t.me/bot?start=ch_x"}, nil
	}
	return service.SocialStart{URL: "https://accounts.google.com/x", State: "STATE"}, nil
}

func (f *fakeSocial) Callback(_ context.Context, u domain.User, p domain.SocialPlatform, in service.SocialCallback) ([]domain.SocialAccount, error) {
	f.last, f.lastP, f.userID = in, p, u.ID
	return nil, f.err
}

func (f *fakeSocial) RefreshNow(_ context.Context, u domain.User, id int64) (domain.SocialAccount, error) {
	return domain.SocialAccount{ID: id, Status: domain.SocialVerified}, f.err
}

func (f *fakeSocial) Unlink(_ context.Context, u domain.User, id int64) error { return f.err }

func socialSrv(f *fakeSocial) *Server {
	return New(Services{Home: fakeHome{}, Health: fakeDB{}, Auth: &fakeAuth{user: domain.User{ID: 3}}, Profiles: &fakeProfiles{}, Social: f},
		Options{Origin: testOrigin, SecureCookies: true})
}

func TestSocialEndpoints(t *testing.T) {
	f := &fakeSocial{}
	s := socialSrv(f)
	rec := serve(s, meReq(http.MethodGet, "/api/me/social", ""))
	var out socialOut
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || len(out.Platforms) != 2 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if a := out.Platforms[0].Accounts[0]; a.Kind != "group" || *a.Followers != 1200 || a.RefreshedAt.Location() != time.UTC ||
		out.Platforms[1].Available || out.Platforms[1].Accounts == nil {
		t.Errorf("%+v", out)
	}
	if rec.Header().Get("Cache-Control") == "" {
		t.Error("no-store")
	}
	// старт OAuth — state в httpOnly-cookie на /api/social
	rec = serve(s, meReq(http.MethodPost, "/api/me/social/youtube/start", ""))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "accounts.google.com") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	c := rec.Result().Cookies()
	if len(c) != 1 || c[0].Name != socialCookie || c[0].Value != "STATE" || !c[0].HttpOnly || !c[0].Secure || c[0].Path != "/api/social" {
		t.Errorf("cookie: %+v", c)
	}
	// Telegram — без cookie
	rec = serve(s, meReq(http.MethodPost, "/api/me/social/telegram/start", ""))
	if rec.Code != 200 || len(rec.Result().Cookies()) != 0 || f.userID != 3 {
		t.Errorf("%d %+v", rec.Code, rec.Result().Cookies())
	}
	if rec := serve(s, meReq(http.MethodPost, "/api/me/social/myspace/start", "")); rec.Code != 404 {
		t.Errorf("неизвестная площадка: %d", rec.Code)
	}
	// без CSRF — нельзя
	r := httptest.NewRequest(http.MethodPost, "/api/me/social/youtube/start", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "good"})
	if rec := serve(s, r); rec.Code != 403 {
		t.Errorf("CSRF: %d", rec.Code)
	}
	// ошибки сервиса → коды
	for err, code := range map[error]int{
		service.ErrSocialDisabled:    503,
		service.ErrSocialTooMany:     409,
		service.ErrSocialUnavailable: 503,
		&service.LimitError{Reason: "social_hourly", RetryAfter: time.Minute}: 429,
	} {
		f.err = err
		rec := serve(s, meReq(http.MethodPost, "/api/me/social/vk/start", ""))
		if rec.Code != code {
			t.Errorf("%v: %d", err, rec.Code)
		}
	}
	f.err = &service.LimitError{Reason: "social_refresh", RetryAfter: 5 * time.Minute}
	if rec := serve(s, meReq(http.MethodPost, "/api/me/social/accounts/5/refresh", "")); rec.Code != 429 || !strings.Contains(errOf(t, rec).Message, "только что обновлены") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	f.err = nil
	if rec := serve(s, meReq(http.MethodPost, "/api/me/social/accounts/5/refresh", "")); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":5`) {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(s, meReq(http.MethodDelete, "/api/me/social/accounts/5", "")); rec.Code != 204 {
		t.Errorf("отвязка: %d", rec.Code)
	}
	for _, p := range []string{"/api/me/social/accounts/x", "/api/me/social/accounts/0"} {
		if rec := serve(s, meReq(http.MethodDelete, p, "")); rec.Code != 404 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	f.err = domain.ErrNotFound
	if rec := serve(s, meReq(http.MethodDelete, "/api/me/social/accounts/9", "")); rec.Code != 404 {
		t.Errorf("чужой: %d", rec.Code)
	}
	// без входа
	if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/me/social", nil)); rec.Code != 401 {
		t.Errorf("без входа: %d", rec.Code)
	}
	// выключено — маршрутов нет
	off := New(Services{Home: fakeHome{}, Health: fakeDB{}, Auth: &fakeAuth{}, Profiles: &fakeProfiles{}}, Options{Origin: testOrigin})
	if rec := serve(off, meReq(http.MethodGet, "/api/me/social", "")); rec.Code != 404 {
		t.Errorf("выключено: %d", rec.Code)
	}
}

func callbackReq(path, cookieState string, session bool) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if cookieState != "" {
		r.AddCookie(&http.Cookie{Name: socialCookie, Value: cookieState})
	}
	if session {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "good"})
	}
	return r
}

func TestSocialCallback(t *testing.T) {
	f := &fakeSocial{}
	s := socialSrv(f)
	loc := func(rec *httptest.ResponseRecorder) string { return rec.Header().Get("Location") }

	rec := serve(s, callbackReq("/api/social/youtube/callback?state=STATE&code=CODE", "STATE", true))
	if rec.Code != 303 || loc(rec) != "/app/card?result=ok&social=youtube#social" || f.last.Code != "CODE" || f.last.State != "STATE" || f.userID != 3 {
		t.Fatalf("%d %s %+v", rec.Code, loc(rec), f.last)
	}
	// cookie state стирается
	if c := rec.Result().Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Errorf("cookie: %+v", c)
	}
	// state не совпал / нет cookie — сервис не вызывается
	f.last = service.SocialCallback{}
	for _, r := range []*http.Request{
		callbackReq("/api/social/youtube/callback?state=OTHER&code=C", "STATE", true),
		callbackReq("/api/social/youtube/callback?state=STATE&code=C", "", true),
		callbackReq("/api/social/youtube/callback?code=C", "STATE", true),
	} {
		rec := serve(s, r)
		if loc(rec) != "/app/card?result=state&social=youtube#social" || f.last.Code != "" {
			t.Errorf("%s: %s", r.URL, loc(rec))
		}
	}
	// без входа — на вход
	if rec := serve(s, callbackReq("/api/social/youtube/callback?state=STATE&code=C", "STATE", false)); loc(rec) != "/login" {
		t.Errorf("без входа: %s", loc(rec))
	}
	bad := callbackReq("/api/social/youtube/callback?state=STATE&code=C", "STATE", false)
	bad.AddCookie(&http.Cookie{Name: sessionCookie, Value: "bad"})
	if rec := serve(s, bad); loc(rec) != "/login" {
		t.Errorf("сессия истекла: %s", loc(rec))
	}
	if rec := serve(s, callbackReq("/api/social/myspace/callback", "", true)); rec.Code != 404 {
		t.Errorf("площадка: %d", rec.Code)
	}
	// ошибки площадки → понятный итог; отказ автора и device_id доходят до сервиса
	for err, want := range map[error]string{
		service.ErrSocialDenied:      "denied",
		service.ErrSocialState:       "state",
		service.ErrSocialNoChannel:   "no_channel",
		service.ErrSocialTooMany:     "too_many",
		service.ErrSocialDisabled:    "disabled",
		service.ErrSocialUnavailable: "unavailable",
		&service.LimitError{}:        "limit",
		fmt.Errorf("база"):           "error",
	} {
		f.err = err
		rec := serve(s, callbackReq("/api/social/youtube/callback?state=STATE&error=access_denied&device_id=D", "STATE", true))
		if loc(rec) != "/app/card?result="+want+"&social=youtube#social" {
			t.Errorf("%v: %s", err, loc(rec))
		}
	}
	if f.last.Error != "access_denied" || f.last.DeviceID != "D" {
		t.Errorf("%+v", f.last)
	}
}

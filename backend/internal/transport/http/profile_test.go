package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/ratelimit"
	"ohvat/backend/internal/service"
)

// fakeProfiles — подставной сервис профилей: err — ошибка любого метода, last — что передали.
type fakeProfiles struct {
	mu   sync.Mutex
	err  error
	biz  *domain.BusinessProfile
	cre  *domain.CreatorProfile
	last map[string]any
}

func (f *fakeProfiles) remember(k string, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.last == nil {
		f.last = map[string]any{}
	}
	f.last[k] = v
}

func (f *fakeProfiles) Overview(_ context.Context, u domain.User) (service.Overview, error) {
	if f.err != nil {
		return service.Overview{}, f.err
	}
	return service.Overview{User: u, Providers: []string{"email"}, Profiles: domain.Profiles{Business: f.biz, Creator: f.cre},
		Checklist: []service.Step{{Key: service.StepBasics, Done: true}, {Key: service.StepProfile}, {Key: service.StepTelegram}}, Completion: 33}, nil
}

func (f *fakeProfiles) SwitchMode(ctx context.Context, u domain.User, mode string) (service.Overview, error) {
	f.remember("mode", mode)
	if mode != "business" && mode != "creator" {
		return service.Overview{}, &service.ValidationError{Fields: map[string]string{"mode": "x"}}
	}
	u.ActiveMode = mode
	return f.Overview(ctx, u)
}

func (f *fakeProfiles) SaveBasics(_ context.Context, u domain.User, b domain.Basics) (domain.User, error) {
	f.remember("basics", b)
	if f.err != nil {
		return domain.User{}, f.err
	}
	now := time.Now()
	u.Name, u.City, u.OnboardedAt = b.Name, b.City, &now
	return u, nil
}

func (f *fakeProfiles) Business(context.Context, domain.User) (*domain.BusinessProfile, error) {
	return f.biz, f.err
}

func (f *fakeProfiles) SaveBusiness(_ context.Context, _ domain.User, p domain.BusinessProfile) (domain.BusinessProfile, error) {
	f.remember("business", p)
	if f.err != nil {
		return domain.BusinessProfile{}, f.err
	}
	return p, nil
}

func (f *fakeProfiles) Creator(context.Context, domain.User) (*domain.CreatorProfile, error) {
	return f.cre, f.err
}

func (f *fakeProfiles) SaveCreator(_ context.Context, _ domain.User, p domain.CreatorProfile) (domain.CreatorProfile, error) {
	f.remember("creator", p)
	if f.err != nil {
		return domain.CreatorProfile{}, f.err
	}
	return p, nil
}

func newMemLimiter(rps float64, burst int) Limiter {
	return ratelimit.NewMemory(ratelimit.Rate{RPS: rps, Burst: burst})
}

func profSrv(a *fakeAuth, p *fakeProfiles, opt Options) *Server {
	opt.Origin, opt.SecureCookies = testOrigin, true
	return New(fakeHome{}, fakeDB{}, a, p, opt)
}

// meReq — запрос вошедшего (cookie сессии «good» и CSRF).
func meReq(method, path, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	withCSRF(r)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "good"})
	return r
}

func TestMe(t *testing.T) {
	a := &fakeAuth{user: domain.User{ID: 3, Name: "Алина", Email: "a@mail.ru", ActiveMode: "creator"}}
	p := &fakeProfiles{biz: &domain.BusinessProfile{Name: "Кофейня"}}
	s := profSrv(a, p, Options{})
	if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/me", nil)); rec.Code != 401 {
		t.Fatalf("без cookie: %d", rec.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "bad"})
	if rec := serve(s, r); rec.Code != 401 || cookie(rec, sessionCookie) == nil {
		t.Fatalf("плохая сессия: %d, cookie должна стираться", rec.Code)
	}
	rec := serve(s, meReq(http.MethodGet, "/api/me", ""))
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var me meDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.User.Name != "Алина" || *me.User.Email != "a@mail.ru" || me.User.Mode != "creator" || me.Providers[0] != "email" ||
		!me.Profiles.Business || me.Profiles.Creator || me.Completion != 33 || len(me.Checklist) != 3 || me.Checklist[0].Key != "basics" {
		t.Errorf("%s", rec.Body.String())
	}
	// режим по умолчанию — бизнес
	if d := toUserDTO(domain.User{ID: 1}); d.Mode != "business" {
		t.Errorf("режим по умолчанию: %+v", d)
	}
	p.err = errors.New("db")
	if rec := serve(s, meReq(http.MethodGet, "/api/me", "")); rec.Code != 500 || strings.Contains(rec.Body.String(), "db") {
		t.Errorf("сбой: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPatchMode(t *testing.T) {
	a := &fakeAuth{user: domain.User{ID: 3, ActiveMode: "business"}}
	p := &fakeProfiles{}
	s := profSrv(a, p, Options{})
	rec := serve(s, meReq(http.MethodPatch, "/api/me/mode", `{"mode":"creator"}`))
	var me meDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if rec.Code != 200 || me.User.Mode != "creator" || p.last["mode"] != "creator" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(s, meReq(http.MethodPatch, "/api/me/mode", `{"mode":"admin"}`)); rec.Code != 400 || errOf(t, rec).Fields["mode"] == "" {
		t.Errorf("плохой режим: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(s, meReq(http.MethodPatch, "/api/me/mode", `{"mode":"creator","x":1}`)); rec.Code != 400 {
		t.Errorf("лишнее поле: %d", rec.Code)
	}
	// без CSRF
	r := httptest.NewRequest(http.MethodPatch, "/api/me/mode", strings.NewReader(`{"mode":"creator"}`))
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "good"})
	if rec := serve(s, r); rec.Code != 403 {
		t.Errorf("без CSRF: %d", rec.Code)
	}
}

func TestProfileEndpoints(t *testing.T) {
	a := &fakeAuth{user: domain.User{ID: 3}}
	p := &fakeProfiles{}
	s := profSrv(a, p, Options{})
	// не заполнены — profile: null
	for _, path := range []string{"/api/me/profile/business", "/api/me/profile/creator"} {
		rec := serve(s, meReq(http.MethodGet, path, ""))
		if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"profile":null}` {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	// общие данные
	rec := serve(s, meReq(http.MethodPut, "/api/me/profile", `{"name":"Анна","city":"Казань"}`))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"onboarded":true`) || p.last["basics"] != (domain.Basics{Name: "Анна", City: "Казань"}) {
		t.Fatalf("общие: %d %s", rec.Code, rec.Body.String())
	}
	// бизнес
	rec = serve(s, meReq(http.MethodPut, "/api/me/profile/business", `{"name":"Кофейня","city":"Казань","spheres":["food"]}`))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"spheres":["food"]`) {
		t.Fatalf("бизнес: %d %s", rec.Code, rec.Body.String())
	}
	if got := p.last["business"].(domain.BusinessProfile); got.Name != "Кофейня" || got.Spheres[0] != "food" {
		t.Errorf("в сервис: %+v", got)
	}
	// автор
	rec = serve(s, meReq(http.MethodPut, "/api/me/profile/creator", `{"name":"Алина","city":"Москва","spheres":["beauty"],"channels":["@a"]}`))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"channels":["@a"]`) {
		t.Fatalf("автор: %d %s", rec.Code, rec.Body.String())
	}
	p.biz = &domain.BusinessProfile{Name: "Кофейня", City: "Казань"}
	p.cre = &domain.CreatorProfile{Name: "Алина", City: "Москва"}
	rec = serve(s, meReq(http.MethodGet, "/api/me/profile/business", ""))
	if !strings.Contains(rec.Body.String(), `"name":"Кофейня"`) || !strings.Contains(rec.Body.String(), `"spheres":[]`) {
		t.Errorf("бизнес: %s", rec.Body.String())
	}
	rec = serve(s, meReq(http.MethodGet, "/api/me/profile/creator", ""))
	if !strings.Contains(rec.Body.String(), `"name":"Алина"`) || !strings.Contains(rec.Body.String(), `"channels":[]`) {
		t.Errorf("автор: %s", rec.Body.String())
	}
	// ошибки полей и сбои
	p.err = &service.ValidationError{Fields: map[string]string{"spheres": "Выберите"}}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/me/profile", `{"name":"","city":""}`},
		{http.MethodPut, "/api/me/profile/business", `{}`},
		{http.MethodPut, "/api/me/profile/creator", `{}`},
	} {
		if rec := serve(s, meReq(c.method, c.path, c.body)); rec.Code != 400 || errOf(t, rec).Code != "validation" {
			t.Errorf("%s: %d %s", c.path, rec.Code, rec.Body.String())
		}
	}
	p.err = errors.New("db")
	for _, path := range []string{"/api/me/profile/business", "/api/me/profile/creator"} {
		if rec := serve(s, meReq(http.MethodGet, path, "")); rec.Code != 500 {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
	for _, path := range []string{"/api/me/profile", "/api/me/profile/business", "/api/me/profile/creator", "/api/me/mode"} {
		if rec := serve(s, meReq(http.MethodPut, path, `not json`)); rec.Code != 400 && rec.Code != 405 {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
}

func TestLogoutAll(t *testing.T) {
	a := &fakeAuth{user: domain.User{ID: 7}}
	s := profSrv(a, &fakeProfiles{}, Options{})
	rec := serve(s, meReq(http.MethodPost, "/api/me/logout-all", ""))
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"sessions":3}` || a.last["logoutAll"] != int64(7) {
		t.Fatalf("%d %s %v", rec.Code, rec.Body.String(), a.last)
	}
	if c := cookie(rec, sessionCookie); c == nil || c.MaxAge >= 0 {
		t.Error("cookie сессии должна стираться")
	}
	a.err = errors.New("db")
	if rec := serve(s, meReq(http.MethodPost, "/api/me/logout-all", "")); rec.Code != 500 {
		t.Errorf("сбой: %d", rec.Code)
	}
}

func TestLogout(t *testing.T) {
	a := &fakeAuth{user: domain.User{ID: 3}}
	s := profSrv(a, &fakeProfiles{}, Options{})
	rec := serve(s, meReq(http.MethodPost, "/api/auth/logout", ""))
	if rec.Code != 204 || a.last["logout"] != "good" {
		t.Fatalf("выход: %d %v", rec.Code, a.last)
	}
	if c := cookie(rec, sessionCookie); c == nil || c.MaxAge >= 0 {
		t.Error("cookie сессии должна стираться")
	}
	a.err = errors.New("db")
	if rec := serve(s, meReq(http.MethodPost, "/api/auth/logout", "")); rec.Code != 500 {
		t.Errorf("ошибка выхода: %d", rec.Code)
	}
}

// failLimiter — хранилище лимитов недоступно.
type failLimiter struct{}

func (failLimiter) Allow(context.Context, string) (bool, time.Duration, error) {
	return false, 0, errors.New("redis down")
}

func TestLimits(t *testing.T) {
	t.Run("весь /api — по адресу", func(t *testing.T) {
		s := profSrv(&fakeAuth{}, &fakeProfiles{}, Options{APILimiter: newMemLimiter(0.01, 2)})
		var last *httptest.ResponseRecorder
		for i := 0; i < 3; i++ {
			r := httptest.NewRequest(http.MethodGet, "/api/home", nil)
			r.Header.Set("X-Real-IP", "198.51.100.1")
			last = serve(s, r)
		}
		if last.Code != 429 || last.Header().Get("Retry-After") != "100" || errOf(t, last).RetryAfter != 100 || errOf(t, last).Code != "rate_limited" {
			t.Fatalf("429: %d %v %s", last.Code, last.Header(), last.Body.String())
		}
		r := httptest.NewRequest(http.MethodGet, "/api/home", nil)
		r.Header.Set("X-Real-IP", "198.51.100.2")
		if rec := serve(s, r); rec.Code != 200 {
			t.Errorf("другой адрес — свой бакет: %d", rec.Code)
		}
		if rec := serve(s, httptest.NewRequest(http.MethodGet, "/healthz", nil)); rec.Code != 200 {
			t.Errorf("/healthz без лимита: %d", rec.Code)
		}
	})
	t.Run("вход — строже, config не ограничен", func(t *testing.T) {
		s := profSrv(&fakeAuth{}, &fakeProfiles{}, Options{AuthLimiter: newMemLimiter(0.01, 2)})
		var last *httptest.ResponseRecorder
		for i := 0; i < 3; i++ {
			last = post(s, "/api/auth/email/start", `{"email":"a@mail.ru","consent":true}`)
		}
		if last.Code != 429 {
			t.Fatalf("%d", last.Code)
		}
		if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)); rec.Code != 200 {
			t.Errorf("config: %d", rec.Code)
		}
	})
	t.Run("/api/me — по пользователю, не по адресу", func(t *testing.T) {
		a := &fakeAuth{user: domain.User{ID: 9}}
		s := profSrv(a, &fakeProfiles{}, Options{UserLimiter: newMemLimiter(0.01, 2)})
		codes := []int{}
		for i := 0; i < 3; i++ {
			r := meReq(http.MethodGet, "/api/me", "")
			r.Header.Set("X-Real-IP", "203.0.113."+string(rune('1'+i)))
			codes = append(codes, serve(s, r).Code)
		}
		if codes[0] != 200 || codes[1] != 200 || codes[2] != 429 {
			t.Fatalf("%v", codes)
		}
		a.user.ID = 10
		if rec := serve(s, meReq(http.MethodGet, "/api/me", "")); rec.Code != 200 {
			t.Errorf("другой пользователь — свой бакет: %d", rec.Code)
		}
	})
	t.Run("хранилище лимитов недоступно — запрос пропускается", func(t *testing.T) {
		s := profSrv(&fakeAuth{}, &fakeProfiles{}, Options{APILimiter: failLimiter{}})
		if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/home", nil)); rec.Code != 200 {
			t.Errorf("%d", rec.Code)
		}
	})
	t.Run("ключ пустой — без лимита", func(t *testing.T) {
		h := limit(newMemLimiter(0.01, 1), func(*http.Request) string { return "" })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
		for i := 0; i < 3; i++ {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			if rec.Code != 204 {
				t.Fatal(rec.Code)
			}
		}
	})
}

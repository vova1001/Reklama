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

// mapDrafts — хранилище черновиков в памяти (для проверки транспорта вместе с сервисом).
type mapDrafts map[string]domain.FormDraft

func (m mapDrafts) GetDraft(_ context.Context, _ int64, form string) (domain.FormDraft, error) {
	d, ok := m[form]
	if !ok {
		return domain.FormDraft{}, domain.ErrNotFound
	}
	return d, nil
}
func (m mapDrafts) PutDraft(_ context.Context, _ int64, d domain.FormDraft, _ time.Duration) error {
	m[d.Form] = d
	return nil
}
func (m mapDrafts) DeleteDraft(_ context.Context, _ int64, form string) error {
	delete(m, form)
	return nil
}

func draftSrv(opt Options) *Server {
	opt.Origin, opt.SecureCookies = testOrigin, true
	return New(Services{
		Home: fakeHome{}, Health: fakeDB{}, Auth: &fakeAuth{user: domain.User{ID: 3}}, Profiles: &fakeProfiles{},
		Drafts: service.NewDrafts(mapDrafts{}, service.DraftConfig{MaxBytes: 200}, nil),
	}, opt)
}

func TestDraftEndpoints(t *testing.T) {
	s := draftSrv(Options{DraftLimiter: newMemLimiter(0.001, 4)})
	if rec := serve(s, meReq(http.MethodGet, "/api/me/drafts/card", "")); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"draft":null}` {
		t.Fatalf("нет: %d %s", rec.Code, rec.Body.String())
	}
	body := `{"fields":{"name":"Алина"},"blocks":[{"kind":"text","title":"О себе","items":["Привет"]}]}`
	rec := serve(s, meReq(http.MethodPut, "/api/me/drafts/card", body))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "savedAt") {
		t.Fatalf("сохранение: %d %s", rec.Code, rec.Body.String())
	}
	rec = serve(s, meReq(http.MethodGet, "/api/me/drafts/card", ""))
	var out struct {
		Draft struct {
			Form    string          `json:"form"`
			Data    json.RawMessage `json:"data"`
			SavedAt time.Time       `json:"savedAt"`
		} `json:"draft"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Draft.Form != "card" || string(out.Draft.Data) != body || out.Draft.SavedAt.IsZero() {
		t.Fatalf("чтение: %v %s", err, rec.Body.String())
	}
	// больше предела — 413; не объект — 400; неизвестная форма — 404
	if rec := serve(s, meReq(http.MethodPut, "/api/me/drafts/card", `{"x":"`+strings.Repeat("a", 300)+`"}`)); rec.Code != 413 {
		t.Errorf("размер: %d", rec.Code)
	}
	if rec := serve(s, meReq(http.MethodPut, "/api/me/drafts/card", `[1]`)); rec.Code != 400 {
		t.Errorf("не объект: %d", rec.Code)
	}
	// лимит частоты сохранений (4 подряд — уже потрачены 3)
	if rec := serve(s, meReq(http.MethodPut, "/api/me/drafts/task-new", `{}`)); rec.Code != 200 {
		t.Errorf("4-е: %d", rec.Code)
	}
	if rec := serve(s, meReq(http.MethodPut, "/api/me/drafts/task-new", `{}`)); rec.Code != 429 {
		t.Errorf("лимит: %d", rec.Code)
	}
	for _, p := range []string{"/api/me/drafts/other", "/api/me/drafts/task-0"} {
		if rec := serve(s, meReq(http.MethodGet, p, "")); rec.Code != 404 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	if rec := serve(s, meReq(http.MethodDelete, "/api/me/drafts/card", "")); rec.Code != 204 {
		t.Errorf("удаление: %d", rec.Code)
	}
	if rec := serve(s, meReq(http.MethodGet, "/api/me/drafts/card", "")); strings.TrimSpace(rec.Body.String()) != `{"draft":null}` {
		t.Errorf("после удаления: %s", rec.Body.String())
	}
	// без входа и без CSRF — нельзя
	if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/me/drafts/card", nil)); rec.Code != 401 {
		t.Errorf("без входа: %d", rec.Code)
	}
	r := httptest.NewRequest(http.MethodPut, "/api/me/drafts/card", strings.NewReader(`{}`))
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "good"})
	if rec := serve(s, r); rec.Code != 403 {
		t.Errorf("без CSRF: %d", rec.Code)
	}
}

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

type fakeHome struct {
	v   service.HomeView
	err error
}

func (f fakeHome) Get(context.Context) (service.HomeView, error) { return f.v, f.err }

type fakeDB struct{ err error }

func (f fakeDB) Check(context.Context) error { return f.err }

func do(t *testing.T, s *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestGetHome(t *testing.T) {
	v := service.HomeView{
		Categories: []domain.Category{{Slug: "food", Title: "Еда и кафе", Icon: "coffee"}},
		BusinessPlans: []service.PlanView{{
			Plan:      domain.Plan{Audience: "business", Slug: "pro", Name: "Про", PriceKopecks: 300000, Period: "month", Recommended: true},
			PriceText: "3 000 ₽", CTA: "Выбрать Про",
		}},
		CreatorPlans: []service.PlanView{},
		FAQ:          []domain.Question{{ID: 1, Question: "В?", Answer: "О."}},
	}
	rec := do(t, newSrv(fakeHome{v: v}, fakeDB{}), http.MethodGet, "/api/home")
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type %q", ct)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"categories", "businessPlans", "creatorPlans", "faq"} {
		if _, ok := got[k]; !ok {
			t.Errorf("нет поля %s", k)
		}
	}
	var plans []map[string]any
	_ = json.Unmarshal(got["businessPlans"], &plans)
	p := plans[0]
	for _, k := range []string{"slug", "audience", "name", "subtitle", "priceKopecks", "priceText", "period", "recommended", "features", "cta"} {
		if _, ok := p[k]; !ok {
			t.Errorf("в тарифе нет поля %s", k)
		}
	}
	if p["features"] == nil {
		t.Error("features должен быть массивом, не null")
	}
	if string(got["creatorPlans"]) != "[]" {
		t.Errorf("пустой список — [], получили %s", got["creatorPlans"])
	}
}

func TestGetHomeError(t *testing.T) {
	rec := do(t, newSrv(fakeHome{err: errors.New("db down: secret details")}, fakeDB{}), http.MethodGet, "/api/home")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("код %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Error("внутренности ошибки не должны уходить наружу")
	}
	var e errorDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Error.Code != "internal" || e.Error.Message == "" {
		t.Errorf("формат ошибки: %s", rec.Body.String())
	}
}

func TestHealth(t *testing.T) {
	if rec := do(t, newSrv(fakeHome{}, fakeDB{}), http.MethodGet, "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("живой: код %d", rec.Code)
	}
	rec := do(t, newSrv(fakeHome{}, fakeDB{err: errors.New("down")}), http.MethodGet, "/healthz")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "db_unavailable") {
		t.Errorf("база недоступна: %d %s", rec.Code, rec.Body.String())
	}
}

func TestNotFoundAndMethod(t *testing.T) {
	s := newSrv(fakeHome{}, fakeDB{})
	if rec := do(t, s, http.MethodGet, "/api/nope"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "not_found") {
		t.Errorf("404: %d %s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "/api/home", nil)
	withCSRF(req)
	if rec := serve(s, req); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("405: %d", rec.Code)
	}
}

type panicHome struct{}

func (panicHome) Get(context.Context) (service.HomeView, error) { panic("boom") }

func TestRecover(t *testing.T) {
	rec := do(t, newSrv(panicHome{}, fakeDB{}), http.MethodGet, "/api/home")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("паника → 500, получили %d", rec.Code)
	}
}

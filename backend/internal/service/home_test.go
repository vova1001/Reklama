package service

import (
	"context"
	"errors"
	"testing"

	"ohvat/backend/internal/domain"
)

type fakeRepo struct {
	cats  []domain.Category
	plans []domain.Plan
	faq   []domain.Question
	err   map[string]error
}

func (f fakeRepo) Categories(context.Context) ([]domain.Category, error) {
	return f.cats, f.err["cats"]
}
func (f fakeRepo) Plans(context.Context) ([]domain.Plan, error)   { return f.plans, f.err["plans"] }
func (f fakeRepo) FAQ(context.Context) ([]domain.Question, error) { return f.faq, f.err["faq"] }

func TestFormatRub(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 ₽"},
		{100, "1 ₽"},
		{99900, "999 ₽"},
		{300000, "3 000 ₽"},
		{1000000, "10 000 ₽"},
		{123456789, "1 234 567,89 ₽"},
		{9950, "99,50 ₽"},
		{105, "1,05 ₽"},
		{-300000, "−3 000 ₽"},
	}
	for _, c := range cases {
		if got := FormatRub(c.in); got != c.want {
			t.Errorf("FormatRub(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPlanCTA(t *testing.T) {
	cases := []struct {
		plan domain.Plan
		want string
	}{
		{domain.Plan{Name: "Бесплатный", PriceKopecks: 0}, "Начать бесплатно"},
		{domain.Plan{Name: "Про", PriceKopecks: 300000}, "Выбрать Про"},
		{domain.Plan{Name: "Топ", PriceKopecks: 1000000}, "Выбрать Топ"},
	}
	for _, c := range cases {
		if got := PlanCTA(c.plan); got != c.want {
			t.Errorf("PlanCTA(%s) = %q, want %q", c.plan.Name, got, c.want)
		}
	}
}

func TestHomeGet(t *testing.T) {
	repo := fakeRepo{
		cats: []domain.Category{{Slug: "food", Title: "Еда и кафе", Icon: "coffee"}},
		plans: []domain.Plan{
			{Audience: domain.AudienceBusiness, Slug: "free", Name: "Бесплатный", PriceKopecks: 0},
			{Audience: domain.AudienceBusiness, Slug: "pro", Name: "Про", PriceKopecks: 300000, Recommended: true},
			{Audience: domain.AudienceCreator, Slug: "top", Name: "Топ", PriceKopecks: 1000000},
			{Audience: "unknown", Slug: "x", Name: "X"},
		},
		faq: []domain.Question{
			{ID: 1, Question: "Вопрос?", Answer: "Ответ."},
			{ID: 2, Question: "Без ответа?", Answer: "  "},
			{ID: 3, Question: "", Answer: "Ответ без вопроса"},
		},
	}
	v, err := NewHome(repo).Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Categories) != 1 || v.Categories[0].Slug != "food" {
		t.Errorf("темы: %+v", v.Categories)
	}
	if len(v.BusinessPlans) != 2 || len(v.CreatorPlans) != 1 {
		t.Fatalf("тарифы по аудиториям: бизнес %d, авторы %d", len(v.BusinessPlans), len(v.CreatorPlans))
	}
	if p := v.BusinessPlans[1]; p.PriceText != "3 000 ₽" || p.CTA != "Выбрать Про" || !p.Recommended {
		t.Errorf("тариф Про: %+v", p)
	}
	if p := v.BusinessPlans[0]; p.CTA != "Начать бесплатно" {
		t.Errorf("бесплатный тариф: %+v", p)
	}
	if len(v.FAQ) != 1 || v.FAQ[0].ID != 1 {
		t.Errorf("вопросы без ответа должны скрываться: %+v", v.FAQ)
	}
}

func TestHomeGetEmpty(t *testing.T) {
	v, err := NewHome(fakeRepo{}).Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.Categories == nil || v.BusinessPlans == nil || v.CreatorPlans == nil || v.FAQ == nil {
		t.Errorf("пустые списки должны быть [], а не nil: %+v", v)
	}
}

func TestHomeGetErrors(t *testing.T) {
	boom := errors.New("boom")
	for _, key := range []string{"cats", "plans", "faq"} {
		_, err := NewHome(fakeRepo{err: map[string]error{key: boom}}).Get(context.Background())
		if !errors.Is(err, boom) {
			t.Errorf("%s: ошибка репозитория должна вернуться, получили %v", key, err)
		}
	}
}

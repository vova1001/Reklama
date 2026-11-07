// Package service — бизнес-логика площадки: без знания об HTTP и SQL.
package service

import (
	"context"
	"strconv"
	"strings"

	"ohvat/backend/internal/domain"
)

// HomeRepository — откуда сервис берёт данные главной.
type HomeRepository interface {
	Categories(ctx context.Context) ([]domain.Category, error)
	Plans(ctx context.Context) ([]domain.Plan, error)
	FAQ(ctx context.Context) ([]domain.Question, error)
}

// PlanView — тариф, готовый к показу: цена текстом и подпись кнопки.
type PlanView struct {
	domain.Plan
	PriceText string
	CTA       string
}

// HomeView — главная, готовая к показу.
type HomeView struct {
	Categories    []domain.Category
	BusinessPlans []PlanView
	CreatorPlans  []PlanView
	FAQ           []domain.Question
}

// Home собирает главную.
type Home struct {
	repo HomeRepository
}

// NewHome создаёт сервис главной.
func NewHome(repo HomeRepository) *Home { return &Home{repo: repo} }

// Get возвращает данные главной: темы, тарифы по аудиториям (по возрастанию цены), вопросы с ответами.
func (s *Home) Get(ctx context.Context) (HomeView, error) {
	cats, err := s.repo.Categories(ctx)
	if err != nil {
		return HomeView{}, err
	}
	plans, err := s.repo.Plans(ctx)
	if err != nil {
		return HomeView{}, err
	}
	faq, err := s.repo.FAQ(ctx)
	if err != nil {
		return HomeView{}, err
	}
	v := HomeView{
		Categories:    nonNil(cats),
		BusinessPlans: []PlanView{},
		CreatorPlans:  []PlanView{},
		FAQ:           []domain.Question{},
	}
	for _, p := range plans {
		pv := PlanView{Plan: p, PriceText: FormatRub(p.PriceKopecks), CTA: PlanCTA(p)}
		switch p.Audience {
		case domain.AudienceBusiness:
			v.BusinessPlans = append(v.BusinessPlans, pv)
		case domain.AudienceCreator:
			v.CreatorPlans = append(v.CreatorPlans, pv)
		}
	}
	// Вопрос без ответа не показываем: раскрыть его нечем.
	for _, q := range faq {
		if strings.TrimSpace(q.Question) != "" && strings.TrimSpace(q.Answer) != "" {
			v.FAQ = append(v.FAQ, q)
		}
	}
	return v, nil
}

// PlanCTA — подпись кнопки тарифа: бесплатный — «Начать бесплатно», платный — «Выбрать <название>».
func PlanCTA(p domain.Plan) string {
	if p.PriceKopecks == 0 {
		return "Начать бесплатно"
	}
	return "Выбрать " + p.Name
}

// FormatRub — цена в рублях для показа: «3 000 ₽», разряды и знак рубля — через неразрывный пробел.
// Копейки показываются, только если они есть: «99,50 ₽».
func FormatRub(kopecks int64) string {
	const nbsp = " "
	neg := kopecks < 0
	if neg {
		kopecks = -kopecks
	}
	rub, kop := kopecks/100, kopecks%100
	digits := strconv.FormatInt(rub, 10)
	var b strings.Builder
	if neg {
		b.WriteString("−")
	}
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteString(nbsp)
		}
		b.WriteRune(d)
	}
	if kop > 0 {
		b.WriteString("," + pad2(kop))
	}
	b.WriteString(nbsp + "₽")
	return b.String()
}

func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

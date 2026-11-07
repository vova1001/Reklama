// Package repository — доступ к данным в PostgreSQL. SQL — только в queries/ (sqlc), здесь — сборка сущностей.
package repository

import (
	"context"
	"fmt"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// Home читает данные главной из PostgreSQL.
type Home struct {
	q *store.Queries
}

// NewHome создаёт репозиторий главной поверх подключения (пул или транзакция).
func NewHome(db store.DBTX) *Home { return &Home{q: store.New(db)} }

// Categories — активные темы по порядку.
func (r *Home) Categories(ctx context.Context) ([]domain.Category, error) {
	rows, err := r.q.ListActiveCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("темы: %w", err)
	}
	out := make([]domain.Category, 0, len(rows))
	for _, c := range rows {
		out = append(out, domain.Category{Slug: c.Slug, Title: c.Title, Icon: c.Icon})
	}
	return out, nil
}

// Plans — активные тарифы обеих аудиторий с пунктами, по аудитории и порядку.
func (r *Home) Plans(ctx context.Context) ([]domain.Plan, error) {
	rows, err := r.q.ListActivePlans(ctx)
	if err != nil {
		return nil, fmt.Errorf("тарифы: %w", err)
	}
	feats, err := r.q.ListPlanFeatures(ctx)
	if err != nil {
		return nil, fmt.Errorf("пункты тарифов: %w", err)
	}
	byPlan := make(map[int64][]string, len(rows))
	for _, f := range feats {
		byPlan[f.PlanID] = append(byPlan[f.PlanID], f.Text)
	}
	out := make([]domain.Plan, 0, len(rows))
	for _, p := range rows {
		features := byPlan[p.ID]
		if features == nil {
			features = []string{}
		}
		out = append(out, domain.Plan{
			ID: p.ID, Audience: p.Audience, Slug: p.Slug, Name: p.Name, Subtitle: p.Subtitle,
			PriceKopecks: p.PriceKopecks, Period: p.Period, Recommended: p.Recommended, Features: features,
		})
	}
	return out, nil
}

// FAQ — опубликованные вопросы по порядку.
func (r *Home) FAQ(ctx context.Context) ([]domain.Question, error) {
	rows, err := r.q.ListPublishedFAQ(ctx)
	if err != nil {
		return nil, fmt.Errorf("вопросы: %w", err)
	}
	out := make([]domain.Question, 0, len(rows))
	for _, q := range rows {
		out = append(out, domain.Question{ID: q.ID, Question: q.Question, Answer: q.Answer})
	}
	return out, nil
}

package httpapi

import "ohvat/backend/internal/service"

// Ответы — строго по api/openapi.yaml.

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorDTO struct {
	Error errorBody `json:"error"`
}

type categoryDTO struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Icon  string `json:"icon"`
}

type planDTO struct {
	Slug         string   `json:"slug"`
	Audience     string   `json:"audience"`
	Name         string   `json:"name"`
	Subtitle     string   `json:"subtitle"`
	PriceKopecks int64    `json:"priceKopecks"`
	PriceText    string   `json:"priceText"`
	Period       string   `json:"period"`
	Recommended  bool     `json:"recommended"`
	Features     []string `json:"features"`
	CTA          string   `json:"cta"`
}

type questionDTO struct {
	ID       int64  `json:"id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type homeDTO struct {
	Categories    []categoryDTO `json:"categories"`
	BusinessPlans []planDTO     `json:"businessPlans"`
	CreatorPlans  []planDTO     `json:"creatorPlans"`
	FAQ           []questionDTO `json:"faq"`
}

func toHomeDTO(v service.HomeView) homeDTO {
	out := homeDTO{
		Categories:    make([]categoryDTO, 0, len(v.Categories)),
		BusinessPlans: plansDTO(v.BusinessPlans),
		CreatorPlans:  plansDTO(v.CreatorPlans),
		FAQ:           make([]questionDTO, 0, len(v.FAQ)),
	}
	for _, c := range v.Categories {
		out.Categories = append(out.Categories, categoryDTO{Slug: c.Slug, Title: c.Title, Icon: c.Icon})
	}
	for _, q := range v.FAQ {
		out.FAQ = append(out.FAQ, questionDTO{ID: q.ID, Question: q.Question, Answer: q.Answer})
	}
	return out
}

func plansDTO(ps []service.PlanView) []planDTO {
	out := make([]planDTO, 0, len(ps))
	for _, p := range ps {
		f := p.Features
		if f == nil {
			f = []string{}
		}
		out = append(out, planDTO{
			Slug: p.Slug, Audience: p.Audience, Name: p.Name, Subtitle: p.Subtitle,
			PriceKopecks: p.PriceKopecks, PriceText: p.PriceText, Period: p.Period,
			Recommended: p.Recommended, Features: f, CTA: p.CTA,
		})
	}
	return out
}

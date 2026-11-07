// Package domain — сущности площадки без знания об HTTP и SQL.
package domain

// Аудитории тарифов.
const (
	AudienceBusiness = "business"
	AudienceCreator  = "creator"
)

// Category — тема заданий.
type Category struct {
	Slug  string
	Title string
	Icon  string // имя иконки Lucide
}

// Plan — тариф бизнеса или автора. Цена — в копейках за период.
type Plan struct {
	ID           int64
	Audience     string
	Slug         string
	Name         string
	Subtitle     string
	PriceKopecks int64
	Period       string
	Recommended  bool
	Features     []string
}

// Question — частый вопрос с ответом.
type Question struct {
	ID       int64
	Question string
	Answer   string
}

// Home — данные главной страницы.
type Home struct {
	Categories    []Category
	BusinessPlans []Plan
	CreatorPlans  []Plan
	FAQ           []Question
}

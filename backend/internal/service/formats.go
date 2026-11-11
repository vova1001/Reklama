package service

import (
	"context"
	"sort"
	"unicode"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// FormatRepository — свои форматы бизнеса (из его заданий), чаще используемые сверху.
type FormatRepository interface {
	OwnFormats(ctx context.Context, ownerID int64) ([]string, error)
}

// Formats — подсказки формата: из списка, известные (domain.FormatHints) и свои уже использованные.
type Formats struct{ repo FormatRepository }

// NewFormats создаёт сервис подсказок форматов.
func NewFormats(repo FormatRepository) *Formats { return &Formats{repo: repo} }

// Similar — до 6 похожих на запрос (как у сфер: точные, начинающиеся с запроса, содержащие, с опечаткой).
func (s *Formats) Similar(ctx context.Context, u domain.User, q string) ([]domain.Format, error) {
	nq := NormCategory(q)
	if nq == "" {
		return []domain.Format{}, nil
	}
	own, err := s.repo.OwnFormats(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	all := make([]domain.Format, 0, len(domain.BuiltinFormats)+len(domain.FormatHints)+len(own))
	all = append(all, domain.BuiltinFormats...)
	for _, h := range domain.FormatHints {
		all = append(all, domain.Format{ID: h, Title: h})
	}
	for _, o := range own {
		all = append(all, domain.Format{ID: o, Title: o, Own: true})
	}
	type cand struct {
		f     domain.Format
		score int
	}
	var cs []cand
	seen := map[string]bool{}
	for _, f := range all {
		n := NormCategory(f.Title)
		if seen[n] {
			continue
		}
		seen[n] = true
		if sc := similarity(nq, n); sc >= 0 {
			cs = append(cs, cand{f, sc})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].score < cs[j].score })
	out := make([]domain.Format, 0, min(len(cs), maxSimilar))
	for i := 0; i < len(cs) && i < maxSimilar; i++ {
		out = append(out, cs[i].f)
	}
	return out, nil
}

// builtinFormat — формат списка по slug или названию (без учёта регистра, ё и знаков).
func builtinFormat(s string) (domain.Format, bool) {
	n := NormCategory(s)
	for _, f := range domain.BuiltinFormats {
		if s == f.ID || n == NormCategory(f.Title) || n == NormCategory(f.ID) {
			return f, true
		}
	}
	return domain.Format{}, false
}

// normFormat — формат к хранению: из списка — его slug; известный — его написание; свой — без лишних пробелов,
// с заглавной, 2–30 символов, буквы, без ссылок и мата. "" и пустое сообщение — пусто (пропустить).
func normFormat(raw string) (string, string) {
	t := squash(raw)
	if t == "" {
		return "", ""
	}
	if f, ok := builtinFormat(t); ok {
		return f.ID, ""
	}
	n := NormCategory(t)
	for _, h := range domain.FormatHints {
		if n == NormCategory(h) {
			return h, ""
		}
	}
	if msg := checkFormatTitle(t); msg != "" {
		return "", "Свой формат: " + lowerFirst(msg)
	}
	return upperFirst(t), ""
}

// checkFormatTitle — 2–30 символов, есть буквы, без ссылок и мата; "" — годится.
func checkFormatTitle(t string) string {
	if n := utf8.RuneCountInString(t); n < 2 || n > 30 {
		return "От 2 до 30 символов"
	}
	return checkCategoryTitle(t) // остальные правила — как у своей категории (длина там шире)
}

func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToLower(r)) + s[n:]
}

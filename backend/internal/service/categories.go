package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// CategoryRepository — справочник и свои категории.
type CategoryRepository interface {
	Common(ctx context.Context) ([]domain.CategoryInfo, error)
	ByAuthor(ctx context.Context, userID int64) ([]domain.CategoryInfo, error)
	// FindByNorm — общая с таким названием, иначе своя на проверке у автора; нет — domain.ErrNotFound.
	FindByNorm(ctx context.Context, userID int64, norm string) (domain.CategoryInfo, error)
	CountPending(ctx context.Context, userID int64) (int, error)
	// CreateCustom — своя «на проверке»; такая уже есть — domain.ErrConflict.
	CreateCustom(ctx context.Context, userID int64, slug, title, norm string) (domain.CategoryInfo, error)
}

// Лимиты своих категорий.
const (
	maxCustomPerItem = 3  // своих тем в одной анкете
	maxCustomPending = 10 // своих на проверке у пользователя
	maxSimilar       = 6  // подсказок
)

// Categories — общий справочник и свои категории: подсказки похожих, создание без дублей.
type Categories struct {
	repo CategoryRepository
	rnd  io.Reader
}

// NewCategories создаёт сервис категорий.
func NewCategories(repo CategoryRepository) *Categories {
	return &Categories{repo: repo, rnd: rand.Reader}
}

// CategoryList — общие и свои категории пользователя.
type CategoryList struct {
	Common []domain.CategoryInfo
	Own    []domain.CategoryInfo
}

// List — общие и свои (на проверке и отклонённые).
func (s *Categories) List(ctx context.Context, u domain.User) (CategoryList, error) {
	common, err := s.repo.Common(ctx)
	if err != nil {
		return CategoryList{}, err
	}
	own, err := s.repo.ByAuthor(ctx, u.ID)
	if err != nil {
		return CategoryList{}, err
	}
	return CategoryList{Common: common, Own: own}, nil
}

// Similar — до 6 похожих на запрос: общие и свои на проверке; сначала точные, потом начинающиеся с запроса,
// содержащие его и близкие по написанию (опечатка в 1–2 буквы).
func (s *Categories) Similar(ctx context.Context, u domain.User, q string) ([]domain.CategoryInfo, error) {
	nq := NormCategory(q)
	if nq == "" {
		return []domain.CategoryInfo{}, nil
	}
	l, err := s.List(ctx, u)
	if err != nil {
		return nil, err
	}
	type cand struct {
		c     domain.CategoryInfo
		score int
	}
	var cs []cand
	for _, c := range append(l.Common, l.Own...) {
		if c.Status == domain.CategoryRejected {
			continue
		}
		if sc := similarity(nq, NormCategory(c.Title)); sc >= 0 {
			cs = append(cs, cand{c, sc})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].score < cs[j].score })
	out := make([]domain.CategoryInfo, 0, min(len(cs), maxSimilar))
	for i := 0; i < len(cs) && i < maxSimilar; i++ {
		out = append(out, cs[i].c)
	}
	return out, nil
}

// Create — своя категория. Такая уже есть (общая или своя, без учёта регистра, ё и пробелов) — вернёт её
// (created = false). Новая — «на проверке», видна автору сразу, общей станет после модерации.
func (s *Categories) Create(ctx context.Context, u domain.User, title string) (domain.CategoryInfo, bool, error) {
	title = squash(title)
	if msg := checkCategoryTitle(title); msg != "" {
		return domain.CategoryInfo{}, false, &ValidationError{Fields: map[string]string{"title": msg}}
	}
	norm := NormCategory(title)
	c, err := s.repo.FindByNorm(ctx, u.ID, norm)
	if err == nil {
		return c, false, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.CategoryInfo{}, false, err
	}
	n, err := s.repo.CountPending(ctx, u.ID)
	if err != nil {
		return domain.CategoryInfo{}, false, err
	}
	if n >= maxCustomPending {
		return domain.CategoryInfo{}, false, &ValidationError{Fields: map[string]string{"title": fmt.Sprintf("Своих категорий на проверке — не больше %d", maxCustomPending)}}
	}
	b := make([]byte, 6)
	if _, err := io.ReadFull(s.rnd, b); err != nil {
		return domain.CategoryInfo{}, false, err
	}
	c, err = s.repo.CreateCustom(ctx, u.ID, "u-"+hex.EncodeToString(b), upperFirst(title), norm)
	if errors.Is(err, domain.ErrConflict) { // создали параллельно — вернуть ту
		c, err = s.repo.FindByNorm(ctx, u.ID, norm)
		return c, false, err
	}
	return c, err == nil, err
}

// NormCategory — название для сравнения: нижний регистр, ё → е, только буквы, цифры и одиночные пробелы.
func NormCategory(s string) string {
	s = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, "Ё", "Е"), "ё", "е"))
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

var (
	reLinkish = regexp.MustCompile(`(?i)(https?:|www\.|://|@|\.(ru|com|рф|net|org|io|me)\b)`)
	// простая проверка на мат: корни в начале слова (без «словаря» — модератор увидит остальное)
	reRude = regexp.MustCompile(`(?i)(^|[^а-яё])(х[уy][йеёяю]|п[иi]зд|[её]б[аеиоуы]|бл[яe][дт]|су[кч][аи]|муд[аио]|п[иi]д[оа]р|залуп|шлюх|г[ао]вн)`)
)

// checkCategoryTitle — 2–40 символов, есть буквы, без ссылок и мата; "" — годится.
func checkCategoryTitle(t string) string {
	n := utf8.RuneCountInString(t)
	switch {
	case n < 2 || n > 40:
		return "От 2 до 40 символов"
	case !strings.ContainsFunc(t, unicode.IsLetter):
		return "Нужны буквы"
	case reLinkish.MatchString(t):
		return "Без ссылок"
	case reRude.MatchString(t):
		return "Выберите другое название"
	}
	return ""
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// similarity — насколько похожи: 0 — совпадают, 1 — начинается с запроса, 2 — содержит, 3+ — близко по
// написанию; -1 — не похожи.
func similarity(q, t string) int {
	switch {
	case q == t:
		return 0
	case strings.HasPrefix(t, q):
		return 1
	case strings.Contains(t, q):
		return 2
	}
	for _, w := range append([]string{t}, strings.Fields(t)...) {
		lq := utf8.RuneCountInString(q)
		if lq < 4 {
			continue
		}
		allow := 1
		if lq >= 7 {
			allow = 2
		}
		prefix := w
		if utf8.RuneCountInString(w) > lq+allow { // опечатка в начале длинного слова
			prefix = string([]rune(w)[:lq])
		}
		if d := levenshtein(q, prefix); d <= allow {
			return 2 + d
		}
	}
	return -1
}

// levenshtein — расстояние правки с перестановкой соседних букв (опечатка «спрот» → «спорт» — одна правка).
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}

package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"ohvat/backend/internal/domain"
)

// ===== публичные страницы: витрина бизнеса /b/<slug> и профиль автора /c/<slug> =====

// PageRepository — страницы, их адреса и проверка.
type PageRepository interface {
	// Page — страница пользователя этого вида; нет — domain.ErrNotFound.
	Page(ctx context.Context, userID int64, kind domain.PageKind) (domain.Page, error)
	PageByID(ctx context.Context, id int64) (domain.Page, error)
	// CreatePage — новая страница-черновик с адресом p.Slug; адрес занят — domain.ErrConflict.
	CreatePage(ctx context.Context, p domain.Page) (domain.Page, error)
	// SavePage — содержимое (только черновик, отклонённая, на доработке; иначе domain.ErrConflict).
	SavePage(ctx context.Context, p domain.Page) (domain.Page, error)
	// SetPageSlug — новый адрес (прежние ведут на него); занят — domain.ErrConflict, много — domain.ErrSlugLimit.
	SetPageSlug(ctx context.Context, id int64, kind domain.PageKind, slug string, maxSlugs int) (domain.Page, error)
	SlugTaken(ctx context.Context, kind domain.PageKind, slug string) (bool, error)
	SetPageStatus(ctx context.Context, chain ...domain.StatusChange) (domain.Page, error)
	SavePageRevision(ctx context.Context, id int64, rev domain.Page) (domain.Page, error)
	SetPageRevision(ctx context.Context, ch domain.StatusChange) (domain.Page, error)
	DropPageRevision(ctx context.Context, id int64) (domain.Page, error)
	ApplyPageRevision(ctx context.Context, id int64, revStatus domain.Status, log domain.StatusChange, chain ...domain.StatusChange) (domain.Page, error)
	// PublishedPage — опубликованная по адресу (нынешнему или прежнему); иначе domain.ErrNotFound.
	PublishedPage(ctx context.Context, kind domain.PageKind, slug string) (domain.Page, error)
	PublishedSlugs(ctx context.Context, kind domain.PageKind, userIDs []int64) (map[int64]string, error)
	OpenTasks(ctx context.Context, ownerID int64, limit int) ([]domain.Task, error)
	// CategoryTitles — названия сфер и тем по slug.
	CategoryTitles(ctx context.Context, slugs []string) (map[string]string, error)
}

// PublicCards — опубликованная анкета автора (блок «Анкета» в профиле).
type PublicCards interface {
	Public(ctx context.Context, userID int64) (domain.CreatorCard, error)
}

// PagesDeps — от чего зависят страницы: то же, что у заданий и анкет (сферы, фото, режим проверки, лимит
// отправок, подтверждённые соцсети) + анкета автора.
type PagesDeps struct {
	ListingDeps
	Cards PublicCards // nil — блок «Анкета» пустой
}

// Pages — страница бизнеса и профиль автора: черновик → проверка (в режиме auto — сразу) → опубликовано; правка
// опубликованного — новая версия на проверку, прежняя видна; адрес — уникальный, прежние ведут на новый.
type Pages struct {
	repo  PageRepository
	cards PublicCards
	ListingDeps
}

// NewPages создаёт сервис страниц.
func NewPages(repo PageRepository, d PagesDeps) *Pages {
	d.defaults()
	return &Pages{repo: repo, cards: d.Cards, ListingDeps: d.ListingDeps}
}

// Пределы страниц.
const (
	maxPageSlugs     = 10 // адресов у страницы за всё время (нынешний + прежние)
	openTasksOnPage  = 12 // открытых заданий на странице бизнеса
	slugSuggestTries = 8
)

// PageInput — страница из редактора. Черновик может быть неполным; полноту проверяет Submit.
type PageInput struct {
	Slug    string // только для новой страницы (дальше — SetSlug); "" — из названия
	Title   string
	City    string
	Spheres []string
	Logo    string
	Cover   string
	Blocks  []PageBlockInput
}

// PageBlockInput — блок из редактора; видео — файлом (Media, Poster) или ссылкой (URL).
type PageBlockInput struct {
	Type     string
	Title    string
	Text     string
	Items    []string
	Photos   []string
	Videos   []VideoInput
	Links    []string
	Contacts *domain.PageContacts
}

// VideoInput — видео в блоке: загруженный файл (Media + Poster по желанию) или ссылка YouTube / VK (URL).
type VideoInput struct {
	Media  string
	Poster string
	URL    string
}

// PageView — своя страница для редактора: nil — ещё нет; SuggestedSlug — адрес, который получит новая.
type PageView struct {
	Page          *domain.Page
	SuggestedSlug string
}

// PublicPage — опубликованная страница для посетителя. MovedTo — адрес сменился (прежний ведёт на новый).
type PublicPage struct {
	Page      domain.Page
	MovedTo   string
	Tasks     []domain.Task       // бизнес: открытые задания
	Platforms []domain.Platform   // автор: подтверждённые соцсети
	Card      *domain.CreatorCard // автор: опубликованная анкета
}

func checkKind(kind domain.PageKind) error {
	if !domain.ValidPageKind(kind) {
		return domain.ErrNotFound
	}
	return nil
}

// Get — своя страница этого вида (nil — ещё нет) и адрес для новой (из названия профиля или имени).
func (s *Pages) Get(ctx context.Context, u domain.User, kind domain.PageKind, name string) (PageView, error) {
	if err := checkKind(kind); err != nil {
		return PageView{}, err
	}
	p, err := s.repo.Page(ctx, u.ID, kind)
	switch {
	case err == nil:
		return PageView{Page: &p, SuggestedSlug: p.Slug}, nil
	case !errors.Is(err, domain.ErrNotFound):
		return PageView{}, err
	}
	slug, err := s.suggest(ctx, u, kind, firstNonEmpty(squash(name), u.Name))
	return PageView{SuggestedSlug: slug}, err
}

// suggest — свободный адрес из названия: «name», «name-2»…; не вышло — «b<id>» / «c<id>».
func (s *Pages) suggest(ctx context.Context, u domain.User, kind domain.PageKind, name string) (string, error) {
	base := domain.SlugFrom(name)
	if len(base) > domain.SlugMax-3 {
		base = strings.TrimRight(base[:domain.SlugMax-3], "-")
	}
	fallback := string(kind[:1]) + strconv.FormatInt(u.ID, 10)
	if !domain.ValidPageSlug(base) {
		base = fallback
	}
	for i := 1; i <= slugSuggestTries; i++ {
		cand := base
		if i > 1 {
			cand = base + "-" + strconv.Itoa(i)
		}
		taken, err := s.repo.SlugTaken(ctx, kind, cand)
		if err != nil {
			return "", err
		}
		if !taken {
			return cand, nil
		}
	}
	return fallback + "-" + strconv.FormatInt(s.Clock.Now().Unix()%100000, 10), nil
}

// Save — сохранить черновик (или правку опубликованной страницы; статус не меняется). Новая страница получает
// адрес in.Slug (если свободен) или из названия.
func (s *Pages) Save(ctx context.Context, u domain.User, kind domain.PageKind, in PageInput) (domain.Page, error) {
	if err := checkKind(kind); err != nil {
		return domain.Page{}, err
	}
	cur, err := s.repo.Page(ctx, u.ID, kind)
	exists := err == nil
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.Page{}, err
	}
	revision := exists && cur.Status == domain.StatusPublished
	if exists && !cur.Status.Editable() && !(revision && domain.RevisionEditable(cur.RevisionStatus)) {
		return domain.Page{}, ErrWrongStatus
	}
	p, err := s.shape(ctx, u.ID, kind, cur.ID, in)
	if err != nil {
		return domain.Page{}, err
	}
	p.UserID, p.Kind = u.ID, kind
	switch {
	case revision:
		out, err := s.repo.SavePageRevision(ctx, cur.ID, p)
		return out, conflictToStatus(err)
	case exists:
		p.ID = cur.ID
		out, err := s.repo.SavePage(ctx, p)
		return out, conflictToStatus(err)
	}
	return s.create(ctx, u, kind, p, strings.ToLower(strings.TrimSpace(in.Slug)))
}

// create — новая страница: адрес из запроса (если годится и свободен) или из названия; гонка за адрес — ещё раз.
func (s *Pages) create(ctx context.Context, u domain.User, kind domain.PageKind, p domain.Page, want string) (domain.Page, error) {
	for try := 0; try < 3; try++ {
		slug := want
		if try > 0 || !domain.ValidPageSlug(slug) {
			var err error
			if slug, err = s.suggest(ctx, u, kind, firstNonEmpty(p.Title, u.Name)); err != nil {
				return domain.Page{}, err
			}
		}
		p.Slug = slug
		out, err := s.repo.CreatePage(ctx, p)
		if !errors.Is(err, domain.ErrConflict) {
			return out, err
		}
		// адрес заняли или страницу создали параллельно (второй запрос) — тогда сохраняем в неё
		if cur, gerr := s.repo.Page(ctx, u.ID, kind); gerr == nil {
			if !cur.Status.Editable() {
				return domain.Page{}, ErrWrongStatus
			}
			p.ID = cur.ID
			out, err := s.repo.SavePage(ctx, p)
			return out, conflictToStatus(err)
		}
	}
	return domain.Page{}, ErrWrongStatus
}

// SetSlug — сменить адрес страницы: латиница, цифры, дефис; занятый — ошибка поля; прежний ведёт на новый.
func (s *Pages) SetSlug(ctx context.Context, u domain.User, kind domain.PageKind, slug string) (domain.Page, error) {
	if err := checkKind(kind); err != nil {
		return domain.Page{}, err
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !domain.ValidPageSlug(slug) {
		return domain.Page{}, &ValidationError{Fields: map[string]string{"slug": "Адрес — от 3 до 40 символов: латиница, цифры и дефис"}}
	}
	cur, err := s.repo.Page(ctx, u.ID, kind)
	if err != nil {
		return domain.Page{}, err
	}
	if cur.Slug == slug {
		return cur, nil
	}
	out, err := s.repo.SetPageSlug(ctx, cur.ID, kind, slug, maxPageSlugs)
	switch {
	case errors.Is(err, domain.ErrConflict):
		return domain.Page{}, &ValidationError{Fields: map[string]string{"slug": "Этот адрес уже занят — выберите другой"}}
	case errors.Is(err, domain.ErrSlugLimit):
		return domain.Page{}, &ValidationError{Fields: map[string]string{"slug": "Адрес можно менять не больше 10 раз"}}
	}
	return out, err
}

// Submit — на проверку (страница заполнена, лимит отправок за сутки не исчерпан); в режиме auto — сразу
// публикуется. У опубликованной с правкой — на проверку уходит правка.
func (s *Pages) Submit(ctx context.Context, u domain.User, kind domain.PageKind) (domain.Page, error) {
	if err := checkKind(kind); err != nil {
		return domain.Page{}, err
	}
	cur, err := s.repo.Page(ctx, u.ID, kind)
	if err != nil {
		return domain.Page{}, err
	}
	if cur.Status == domain.StatusPublished && cur.Revision != nil {
		return s.submitRevision(ctx, u, cur)
	}
	if !domain.CanTransition(cur.Status, domain.StatusPending, domain.ActorOwner) {
		return domain.Page{}, ErrWrongStatus
	}
	if err := pageComplete(cur); err != nil {
		return domain.Page{}, err
	}
	if err := checkSubmitQuota(ctx, s.Submits, s.Clock, u.ID, s.Limits.SubmitsPerDay); err != nil {
		return domain.Page{}, err
	}
	out, err := s.repo.SetPageStatus(ctx, domain.SubmitChain(s.mode(ctx), domain.SubjectPage, cur.ID, cur.Status, u.ID)...)
	return out, conflictToStatus(err)
}

func (s *Pages) submitRevision(ctx context.Context, u domain.User, cur domain.Page) (domain.Page, error) {
	if !domain.RevisionTransition(cur.RevisionStatus, domain.StatusPending, domain.ActorOwner) {
		return domain.Page{}, ErrWrongStatus
	}
	if err := pageComplete(*cur.Revision); err != nil {
		return domain.Page{}, err
	}
	if err := checkSubmitQuota(ctx, s.Submits, s.Clock, u.ID, s.Limits.SubmitsPerDay); err != nil {
		return domain.Page{}, err
	}
	out, err := s.repo.SetPageRevision(ctx, domain.StatusChange{Subject: domain.SubjectPage, SubjectID: cur.ID, From: cur.RevisionStatus, To: domain.StatusPending, ActorID: u.ID})
	if err == nil && s.mode(ctx) == domain.ModerationAuto {
		out, err = s.repo.ApplyPageRevision(ctx, cur.ID, domain.StatusPending,
			domain.StatusChange{Subject: domain.SubjectPage, SubjectID: cur.ID, From: domain.StatusPending, To: domain.StatusPublished, Reason: domain.AutoApproveNote, Auto: true})
	}
	return out, conflictToStatus(err)
}

// Withdraw — в черновик: с проверки или снять с публикации (неотправленная правка становится черновиком).
func (s *Pages) Withdraw(ctx context.Context, u domain.User, kind domain.PageKind) (domain.Page, error) {
	if err := checkKind(kind); err != nil {
		return domain.Page{}, err
	}
	cur, err := s.repo.Page(ctx, u.ID, kind)
	if err != nil {
		return domain.Page{}, err
	}
	if !domain.CanTransition(cur.Status, domain.StatusDraft, domain.ActorOwner) {
		return domain.Page{}, ErrWrongStatus
	}
	ch := domain.StatusChange{Subject: domain.SubjectPage, SubjectID: cur.ID, From: cur.Status, To: domain.StatusDraft, ActorID: u.ID}
	var out domain.Page
	if cur.Status == domain.StatusPublished && cur.Revision != nil {
		out, err = s.repo.ApplyPageRevision(ctx, cur.ID, cur.RevisionStatus,
			domain.StatusChange{Subject: domain.SubjectPage, SubjectID: cur.ID, From: cur.RevisionStatus, To: domain.StatusDraft, ActorID: u.ID}, ch)
	} else {
		out, err = s.repo.SetPageStatus(ctx, ch)
	}
	return out, conflictToStatus(err)
}

// DiscardRevision — удалить правку опубликованной страницы.
func (s *Pages) DiscardRevision(ctx context.Context, u domain.User, kind domain.PageKind) (domain.Page, error) {
	if err := checkKind(kind); err != nil {
		return domain.Page{}, err
	}
	cur, err := s.repo.Page(ctx, u.ID, kind)
	if err != nil {
		return domain.Page{}, err
	}
	out, err := s.repo.DropPageRevision(ctx, cur.ID)
	return out, conflictToStatus(err)
}

// Public — опубликованная страница по адресу: у бизнеса — открытые задания, у автора — подтверждённые соцсети и
// анкета. Прежний адрес — MovedTo (нынешний). Нет, не опубликована, владелец заблокирован — domain.ErrNotFound.
func (s *Pages) Public(ctx context.Context, kind domain.PageKind, slug string) (PublicPage, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if checkKind(kind) != nil || len(slug) > domain.SlugMax || slug == "" {
		return PublicPage{}, domain.ErrNotFound
	}
	p, err := s.repo.PublishedPage(ctx, kind, slug)
	if err != nil {
		return PublicPage{}, err
	}
	out := PublicPage{Page: p}
	if p.Slug != slug {
		out.MovedTo = p.Slug
		return out, nil
	}
	if err := s.topicTitles(ctx, out.Page.Blocks); err != nil {
		return out, err
	}
	if kind == domain.PageBusiness {
		if out.Tasks, err = s.repo.OpenTasks(ctx, p.UserID, openTasksOnPage); err != nil {
			return out, err
		}
		for i := range out.Tasks {
			out.Tasks[i].Brand = p.Title
		}
		return out, nil
	}
	out.Platforms = []domain.Platform{}
	if s.Platforms != nil {
		m, err := s.Platforms.CardPlatforms(ctx, []int64{p.UserID})
		if err != nil {
			return out, err
		}
		if ps := m[p.UserID]; ps != nil {
			out.Platforms = ps
		}
	}
	if s.cards != nil && hasBlock(p.Blocks, domain.PBCard) {
		c, err := s.cards.Public(ctx, p.UserID)
		switch {
		case err == nil:
			out.Card = &c
		case !errors.Is(err, domain.ErrNotFound):
			return out, err
		}
	}
	return out, nil
}

// PublishedSlugs — адреса опубликованных страниц пользователей (ссылки «Профиль» из ленты и страницы задания).
func (s *Pages) PublishedSlugs(ctx context.Context, kind domain.PageKind, userIDs []int64) (map[int64]string, error) {
	return s.repo.PublishedSlugs(ctx, kind, uniqIDs(userIDs))
}

// topicTitles — названия тем в блоках «Что могу рекламировать» (неизвестная — как есть).
func (s *Pages) topicTitles(ctx context.Context, bs []domain.PageBlock) error {
	var slugs []string
	for _, b := range bs {
		if b.Type == domain.PBTopics {
			slugs = append(slugs, b.Items...)
		}
	}
	if len(slugs) == 0 {
		return nil
	}
	m, err := s.repo.CategoryTitles(ctx, uniq(slugs))
	if err != nil {
		return err
	}
	for i := range bs {
		if bs[i].Type == domain.PBTopics {
			bs[i].ItemTitles = mapTitles(m, bs[i].Items)
		}
	}
	return nil
}

func uniqIDs(in []int64) []int64 {
	seen := make(map[int64]bool, len(in))
	out := make([]int64, 0, len(in))
	for _, id := range in {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func hasBlock(bs []domain.PageBlock, t string) bool {
	for _, b := range bs {
		if b.Type == t {
			return true
		}
	}
	return false
}

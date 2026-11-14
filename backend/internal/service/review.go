package service

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// ===== проверка: очередь, аренда, карточка, решения с отменой, свои категории и форматы =====

// ReviewRepository — очередь, аренда, решения и журнал.
type ReviewRepository interface {
	Queue(ctx context.Context, f domain.QueueFilter, me int64, now time.Time) ([]domain.QueueItem, int, *time.Time, error)
	QueueItem(ctx context.Context, kind domain.ReviewKind, itemID string) (domain.QueueItem, error)
	// ClaimLease — взять в работу (своё — продлить); в работе у другого — domain.ErrConflict.
	ClaimLease(ctx context.Context, kind domain.ReviewKind, itemID string, moderatorID int64, now, until time.Time) (domain.Lease, error)
	Lease(ctx context.Context, kind domain.ReviewKind, itemID string) (domain.Lease, error)
	ReleaseLease(ctx context.Context, kind domain.ReviewKind, itemID string, moderatorID int64) error
	DeleteExpiredLeases(ctx context.Context, before time.Time) (int64, error)
	// CreateDecision — решение, продление аренды до применения и журнал — атомарно; ждущее уже есть — ErrConflict.
	CreateDecision(ctx context.Context, d domain.ReviewDecision, leaseUntil time.Time, e domain.AuditEntry) (domain.ReviewDecision, error)
	// UndoDecision — своё ждущее до применения; иначе domain.ErrNotFound.
	UndoDecision(ctx context.Context, id, moderatorID int64, now time.Time, e domain.AuditEntry) (domain.ReviewDecision, error)
	ClaimDueDecision(ctx context.Context, now, lockUntil time.Time) (domain.ReviewDecision, error)
	// FinishDecision — итог решения; n != nil — уведомление владельцу, пишется в той же транзакции (только applied).
	FinishDecision(ctx context.Context, d domain.ReviewDecision, state, errText string, at time.Time, e domain.AuditEntry, n *domain.Notice) error
	// DeleteOldNotifications — убрать прочитанные уведомления старше порога (подметание исполнителя).
	DeleteOldNotifications(ctx context.Context, before time.Time) error
	NextDecisionAt(ctx context.Context) (*time.Time, error)
	OwnerDecisions(ctx context.Context, ownerID int64, limit int) ([]domain.ReviewDecision, error)
	ComplaintsAgainst(ctx context.Context, userID int64, limit int) ([]domain.Complaint, error)
	UserFull(ctx context.Context, id int64) (domain.UserSummary, error)
	CategoryUsage(ctx context.Context, slug string) (tasks, cards int, err error)
	ApprovedCategory(ctx context.Context, slug string) (domain.CategoryInfo, error)
	MergeCategory(ctx context.Context, ch domain.StatusChange, fromSlug, intoSlug string, e domain.AuditEntry) error
	FormatUsage(ctx context.Context, norm string) (int, string, error)
	DecideFormat(ctx context.Context, norm, title, status, into string, by int64, e domain.AuditEntry) error
	ApprovedFormats(ctx context.Context) ([]string, error)
	Audit(ctx context.Context, e domain.AuditEntry) error
}

// ReviewContent — содержимое на проверке: задания, анкеты, категории, журнал статусов.
type ReviewContent interface {
	Task(ctx context.Context, id int64) (domain.Task, error)
	Card(ctx context.Context, userID int64) (domain.CreatorCard, error)
	Category(ctx context.Context, id int64) (domain.CategoryInfo, error)
	Common(ctx context.Context) ([]domain.CategoryInfo, error)
	ByAuthor(ctx context.Context, userID int64) ([]domain.CategoryInfo, error)
	History(ctx context.Context, subject domain.Subject, id int64) ([]domain.StatusChange, error)
}

// PageContent — страница на проверке (по id).
type PageContent interface {
	PageByID(ctx context.Context, id int64) (domain.Page, error)
}

// DecisionNotice — что сообщить владельцу о решении.
type DecisionNotice struct {
	Kind   domain.ReviewKind
	Title  string
	Action domain.ReviewAction
	Reason string
	Notes  map[string]string
	Path   string // где посмотреть: /app/tasks/12, /app/card
}

// DecisionNotifier — уведомление владельца о решении (почта + Telegram). Ошибки не мешают решению.
type DecisionNotifier interface {
	NotifyDecision(ctx context.Context, ownerID int64, n DecisionNotice) error
}

// ReviewConfig — аренда и окно отмены.
type ReviewConfig struct {
	LeaseTTL   time.Duration // элемент в работе у модератора, 10 мин
	UndoWindow time.Duration // отмена решения, 10 с
	ApplyLock  time.Duration // аренда исполнителя на одно решение, 1 мин
}

func (c *ReviewConfig) defaults() {
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 10 * time.Minute
	}
	if c.UndoWindow <= 0 {
		c.UndoWindow = 10 * time.Second
	}
	if c.ApplyLock <= 0 {
		c.ApplyLock = time.Minute
	}
}

// ReviewDeps — зависимости проверки.
type ReviewDeps struct {
	Repo      ReviewRepository
	Content   ReviewContent
	Moderator Moderator
	Platforms PlatformSource   // подтверждённые соцсети в анкете
	Notifier  DecisionNotifier // nil — без уведомлений
	Pages     PageContent      // страницы бизнеса и авторов; nil — их нет в проверке
	Clock     Clock
	Log       *slog.Logger
}

// Review — очередь проверки и решения модераторов.
type Review struct {
	d    ReviewDeps
	cfg  ReviewConfig
	wake chan struct{}
}

// NewReview создаёт сервис проверки.
func NewReview(d ReviewDeps, cfg ReviewConfig) *Review {
	cfg.defaults()
	if d.Clock == nil {
		d.Clock = SystemClock{}
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Review{d: d, cfg: cfg, wake: make(chan struct{}, 1)}
}

// UndoWindow — сколько можно отменить решение (для интерфейса).
func (s *Review) UndoWindow() time.Duration { return s.cfg.UndoWindow }

// QueuePage — страница очереди.
type QueuePage struct {
	Items  []domain.QueueItem
	Total  int
	Oldest *time.Time // самое старое по отбору — «ждёт N»
}

// checkQueueFilter — виды из списка, сферы — slug, город и поиск — до 100 символов, страница до 100.
func checkQueueFilter(f *domain.QueueFilter) error {
	ve := &ValidationError{}
	for _, k := range f.Kinds {
		if !domain.ValidReviewKind(k) {
			ve.add("type", "Неизвестный тип")
		}
	}
	f.Categories = checkSlugs(ve, "category", nonEmptyAll(f.Categories), "в отборе", "Выберите сферу из списка")
	f.City, f.Query = squash(f.City), squash(f.Query)
	maxLen(ve, "city", f.City, 100, "Город — до 100 символов")
	maxLen(ve, "q", f.Query, 100, "Поиск — до 100 символов")
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	if f.Offset < 0 || f.Offset > 10000 {
		f.Offset = 0
	}
	return ve.orNil()
}

// Queue — очередь: ждущие проверки без взятых другими, старые первыми (или новые), счётчик и самое старое.
func (s *Review) Queue(ctx context.Context, u domain.User, f domain.QueueFilter) (QueuePage, error) {
	if err := authorize(u, domain.PermReview); err != nil {
		return QueuePage{}, err
	}
	if err := checkQueueFilter(&f); err != nil {
		return QueuePage{}, err
	}
	items, total, oldest, err := s.d.Repo.Queue(ctx, f, u.ID, s.d.Clock.Now())
	if err != nil {
		return QueuePage{}, err
	}
	return QueuePage{Items: items, Total: total, Oldest: oldest}, nil
}

// Claim — взять элемент в работу на 10 минут (своё — продлить). Уже проверен — domain.ErrNotFound; в работе у
// другого — ErrLeaseHeld.
func (s *Review) Claim(ctx context.Context, u domain.User, kind domain.ReviewKind, id string) (domain.Lease, error) {
	if err := authorize(u, domain.PermReview); err != nil {
		return domain.Lease{}, err
	}
	if !domain.ValidReviewKind(kind) {
		return domain.Lease{}, domain.ErrNotFound
	}
	if _, err := s.d.Repo.QueueItem(ctx, kind, id); err != nil {
		return domain.Lease{}, err
	}
	now := s.d.Clock.Now()
	l, err := s.d.Repo.ClaimLease(ctx, kind, id, u.ID, now, now.Add(s.cfg.LeaseTTL))
	if errors.Is(err, domain.ErrConflict) {
		return domain.Lease{}, ErrLeaseHeld
	}
	return l, err
}

// Release — отпустить элемент (другие снова видят его в очереди).
func (s *Review) Release(ctx context.Context, u domain.User, kind domain.ReviewKind, id string) error {
	if err := authorize(u, domain.PermReview); err != nil {
		return err
	}
	return s.d.Repo.ReleaseLease(ctx, kind, id, u.ID)
}

// Next — следующий в очереди по отбору: берётся в работу сразу (кто первым взял — того и элемент). skip — текущий
// (отпускается). Очередь пуста — domain.ErrNotFound.
func (s *Review) Next(ctx context.Context, u domain.User, f domain.QueueFilter, skipKind domain.ReviewKind, skipID string) (domain.QueueItem, error) {
	if err := authorize(u, domain.PermReview); err != nil {
		return domain.QueueItem{}, err
	}
	if skipID != "" {
		if err := s.d.Repo.ReleaseLease(ctx, skipKind, skipID, u.ID); err != nil {
			return domain.QueueItem{}, err
		}
	}
	f.Offset, f.Limit = 0, 25
	if err := checkQueueFilter(&f); err != nil {
		return domain.QueueItem{}, err
	}
	now := s.d.Clock.Now()
	items, _, _, err := s.d.Repo.Queue(ctx, f, u.ID, now)
	if err != nil {
		return domain.QueueItem{}, err
	}
	for _, it := range items {
		if it.Kind == skipKind && it.ID == skipID {
			continue
		}
		if _, err := s.d.Repo.ClaimLease(ctx, it.Kind, it.ID, u.ID, now, now.Add(s.cfg.LeaseTTL)); err == nil {
			return it, nil
		} else if !errors.Is(err, domain.ErrConflict) {
			return domain.QueueItem{}, err
		}
	}
	return domain.QueueItem{}, domain.ErrNotFound
}

// ReviewItem — карточка проверки: содержимое (как увидят пользователи), владелец, его история, кто держит.
type ReviewItem struct {
	Kind         domain.ReviewKind
	ID           string
	InQueue      bool       // ещё ждёт проверки
	WaitingSince *time.Time // с какого момента ждёт
	Lease        *domain.Lease
	Task         *domain.Task        // на проверке (для правки — новая версия)
	LiveTask     *domain.Task        // правка: опубликованная сейчас
	Card         *domain.CreatorCard // на проверке (для правки — новая версия)
	LiveCard     *domain.CreatorCard
	Page         *domain.Page // на проверке (для правки — новая версия)
	LivePage     *domain.Page
	Category     *domain.CategoryInfo
	Format       *FormatReview
	Usage        [2]int // категория: в заданиях и анкетах
	Targets      []domain.CategoryInfo
	Formats      []string // во что объединить формат: из списка и одобренные
	Owner        *domain.UserSummary
	History      []domain.StatusChange
	Decisions    []domain.ReviewDecision
	Complaints   []domain.Complaint
}

// FormatReview — свой формат на проверке.
type FormatReview struct {
	Norm  string
	Title string
	Tasks int
}

// Item — карточка проверки (только чтение; взять в работу — Claim).
func (s *Review) Item(ctx context.Context, u domain.User, kind domain.ReviewKind, id string) (ReviewItem, error) {
	if err := authorize(u, domain.PermReview); err != nil {
		return ReviewItem{}, err
	}
	if !domain.ValidReviewKind(kind) || id == "" || len(id) > 120 {
		return ReviewItem{}, domain.ErrNotFound
	}
	it := ReviewItem{Kind: kind, ID: id}
	if q, err := s.d.Repo.QueueItem(ctx, kind, id); err == nil {
		it.InQueue, it.WaitingSince = true, &q.WaitingSince
	} else if !errors.Is(err, domain.ErrNotFound) {
		return it, err
	}
	if l, err := s.d.Repo.Lease(ctx, kind, id); err == nil && l.ExpiresAt.After(s.d.Clock.Now()) {
		it.Lease = &l
	} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return it, err
	}
	var ownerID int64
	switch kind {
	case domain.KindTask, domain.KindTaskRevision:
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return it, domain.ErrNotFound
		}
		t, err := s.d.Content.Task(ctx, n)
		if err != nil {
			return it, err
		}
		ownerID = t.OwnerID
		if err := s.titles(ctx, ownerID, &t); err != nil {
			return it, err
		}
		it.Task = &t
		if kind == domain.KindTaskRevision {
			if t.Revision == nil {
				return it, domain.ErrNotFound
			}
			rev := *t.Revision
			rev.ID, rev.OwnerID, rev.Status = t.ID, t.OwnerID, t.RevisionStatus
			if err := s.titles(ctx, ownerID, &rev); err != nil {
				return it, err
			}
			live := t
			live.Revision = nil
			it.Task, it.LiveTask = &rev, &live
		}
		if it.History, err = s.d.Content.History(ctx, domain.SubjectTask, n); err != nil {
			return it, err
		}
	case domain.KindCard, domain.KindCardRevision:
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return it, domain.ErrNotFound
		}
		c, err := s.d.Content.Card(ctx, n)
		if err != nil {
			return it, err
		}
		ownerID = c.UserID
		if s.d.Platforms != nil {
			m, err := s.d.Platforms.CardPlatforms(ctx, []int64{c.UserID})
			if err != nil {
				return it, err
			}
			c.Platforms = m[c.UserID]
		}
		titles, err := s.categoryTitles(ctx, ownerID)
		if err != nil {
			return it, err
		}
		c.TopicTitles = mapTitles(titles, c.Topics)
		it.Card = &c
		if kind == domain.KindCardRevision {
			if c.Revision == nil {
				return it, domain.ErrNotFound
			}
			rev := *c.Revision
			rev.UserID, rev.Status, rev.Platforms = c.UserID, c.RevisionStatus, c.Platforms
			rev.TopicTitles = mapTitles(titles, rev.Topics)
			live := c
			live.Revision = nil
			it.Card, it.LiveCard = &rev, &live
		}
		if it.History, err = s.d.Content.History(ctx, domain.SubjectCard, n); err != nil {
			return it, err
		}
	case domain.KindPage, domain.KindPageRevision:
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || s.d.Pages == nil {
			return it, domain.ErrNotFound
		}
		p, err := s.d.Pages.PageByID(ctx, n)
		if err != nil {
			return it, err
		}
		ownerID = p.UserID
		titles, err := s.categoryTitles(ctx, ownerID)
		if err != nil {
			return it, err
		}
		p.SphereTitles = mapTitles(titles, p.Spheres)
		pageTopicTitles(titles, p.Blocks)
		it.Page = &p
		if kind == domain.KindPageRevision {
			if p.Revision == nil {
				return it, domain.ErrNotFound
			}
			rev := *p.Revision
			rev.ID, rev.UserID, rev.Kind, rev.Slug, rev.Status = p.ID, p.UserID, p.Kind, p.Slug, p.RevisionStatus
			rev.SphereTitles = mapTitles(titles, rev.Spheres)
			pageTopicTitles(titles, rev.Blocks)
			live := p
			live.Revision = nil
			it.Page, it.LivePage = &rev, &live
		}
		if it.History, err = s.d.Content.History(ctx, domain.SubjectPage, n); err != nil {
			return it, err
		}
	case domain.KindCategory:
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return it, domain.ErrNotFound
		}
		c, err := s.d.Content.Category(ctx, n)
		if err != nil {
			return it, err
		}
		it.Category, ownerID = &c, c.AuthorID
		t, cs, err := s.d.Repo.CategoryUsage(ctx, c.Slug)
		if err != nil {
			return it, err
		}
		it.Usage = [2]int{t, cs}
		if it.Targets, err = s.d.Content.Common(ctx); err != nil {
			return it, err
		}
	case domain.KindFormat:
		n, title, err := s.d.Repo.FormatUsage(ctx, id)
		if err != nil {
			return it, err
		}
		if n == 0 && !it.InQueue {
			return it, domain.ErrNotFound
		}
		it.Format = &FormatReview{Norm: id, Title: title, Tasks: n}
		if it.Formats, err = s.formatTargets(ctx); err != nil {
			return it, err
		}
	}
	if ownerID != 0 {
		o, err := s.d.Repo.UserFull(ctx, ownerID)
		if err == nil {
			it.Owner = &o
			if it.Task != nil && it.Task.Brand == "" {
				it.Task.Brand = firstNonEmpty(o.BusinessName, o.Name)
			}
		} else if !errors.Is(err, domain.ErrNotFound) {
			return it, err
		}
		if it.Decisions, err = s.d.Repo.OwnerDecisions(ctx, ownerID, 20); err != nil {
			return it, err
		}
		if it.Complaints, err = s.d.Repo.ComplaintsAgainst(ctx, ownerID, 20); err != nil {
			return it, err
		}
	}
	return it, nil
}

func pageTopicTitles(titles map[string]string, bs []domain.PageBlock) {
	for i := range bs {
		if bs[i].Type == domain.PBTopics {
			bs[i].ItemTitles = mapTitles(titles, bs[i].Items)
		}
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// categoryTitles — названия общих сфер и своих сфер владельца (своя — «на проверке» — тоже видна модератору).
func (s *Review) categoryTitles(ctx context.Context, ownerID int64) (map[string]string, error) {
	common, err := s.d.Content.Common(ctx)
	if err != nil {
		return nil, err
	}
	own, err := s.d.Content.ByAuthor(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(common)+len(own))
	for _, c := range append(common, own...) {
		m[c.Slug] = c.Title
	}
	return m, nil
}

func mapTitles(m map[string]string, slugs []string) []string {
	out := make([]string, 0, len(slugs))
	for _, sl := range slugs {
		if t := m[sl]; t != "" {
			out = append(out, t)
		} else {
			out = append(out, sl)
		}
	}
	return out
}

func (s *Review) titles(ctx context.Context, ownerID int64, t *domain.Task) error {
	m, err := s.categoryTitles(ctx, ownerID)
	if err != nil {
		return err
	}
	t.CategoryTitles = mapTitles(m, t.Categories)
	return nil
}

func (s *Review) formatTargets(ctx context.Context) ([]string, error) {
	out := make([]string, 0, len(domain.BuiltinFormats)+len(domain.FormatHints))
	for _, f := range domain.BuiltinFormats {
		out = append(out, f.ID)
	}
	out = append(out, domain.FormatHints...)
	approved, err := s.d.Repo.ApprovedFormats(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range approved {
		if !contains(out, a) {
			out = append(out, a)
		}
	}
	return out, nil
}

// DecisionInput — решение из карточки проверки.
type DecisionInput struct {
	Action domain.ReviewAction
	Reason string
	Notes  map[string]string
}

func subjectOf(k domain.ReviewKind) domain.Subject {
	switch k {
	case domain.KindCard, domain.KindCardRevision:
		return domain.SubjectCard
	case domain.KindPage, domain.KindPageRevision:
		return domain.SubjectPage
	}
	return domain.SubjectTask
}

// Decide — решение по заданию или анкете (или их правке): применится через 10 с (до этого — Undo). Элемент должен
// быть в работе у этого модератора (иначе ErrLeaseLost) и ещё ждать проверки.
func (s *Review) Decide(ctx context.Context, u domain.User, kind domain.ReviewKind, id string, in DecisionInput) (domain.ReviewDecision, error) {
	if err := authorize(u, domain.PermReview); err != nil {
		return domain.ReviewDecision{}, err
	}
	if !kind.Listing() {
		return domain.ReviewDecision{}, domain.ErrNotFound
	}
	itemID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return domain.ReviewDecision{}, domain.ErrNotFound
	}
	d := Decision{Action: in.Action, Reason: in.Reason, Notes: in.Notes}
	if err := checkDecision(&d, subjectOf(kind)); err != nil {
		return domain.ReviewDecision{}, err
	}
	now := s.d.Clock.Now()
	l, err := s.d.Repo.Lease(ctx, kind, id)
	if errors.Is(err, domain.ErrNotFound) || err == nil && (l.ModeratorID != u.ID || !l.ExpiresAt.After(now)) {
		if err == nil && l.ModeratorID != u.ID && l.ExpiresAt.After(now) {
			return domain.ReviewDecision{}, ErrLeaseHeld
		}
		return domain.ReviewDecision{}, ErrLeaseLost
	}
	if err != nil {
		return domain.ReviewDecision{}, err
	}
	q, err := s.d.Repo.QueueItem(ctx, kind, id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ReviewDecision{}, ErrWrongStatus
	}
	if err != nil {
		return domain.ReviewDecision{}, err
	}
	dec := domain.ReviewDecision{
		Kind: kind, ItemID: itemID, OwnerID: q.OwnerID, ModeratorID: u.ID, Action: d.action(), Reason: d.Reason,
		Notes: d.Notes, WaitingSince: &q.WaitingSince, ApplyAt: now.Add(s.cfg.UndoWindow), CreatedAt: now,
	}
	e := auditFor(ctx, u, domain.AuditDecision, string(kind), id)
	e.Reason = d.Reason
	out, err := s.d.Repo.CreateDecision(ctx, dec, dec.ApplyAt.Add(s.cfg.ApplyLock), e)
	if errors.Is(err, domain.ErrConflict) {
		return domain.ReviewDecision{}, ErrWrongStatus
	}
	if err != nil {
		return domain.ReviewDecision{}, err
	}
	s.signal()
	return out, nil
}

// Undo — отменить своё решение в течение 10 с: элемент снова в работе у модератора.
func (s *Review) Undo(ctx context.Context, u domain.User, decisionID int64) (domain.ReviewDecision, error) {
	if err := authorize(u, domain.PermReview); err != nil {
		return domain.ReviewDecision{}, err
	}
	now := s.d.Clock.Now()
	e := auditFor(ctx, u, domain.AuditDecisionUndo, "", "")
	d, err := s.d.Repo.UndoDecision(ctx, decisionID, u.ID, now, e)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ReviewDecision{}, ErrUndoExpired
	}
	if err != nil {
		return domain.ReviewDecision{}, err
	}
	id := strconv.FormatInt(d.ItemID, 10)
	if _, err := s.d.Repo.ClaimLease(ctx, d.Kind, id, u.ID, now, now.Add(s.cfg.LeaseTTL)); err != nil && !errors.Is(err, domain.ErrConflict) {
		return d, err
	}
	return d, nil
}

func (s *Review) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// ApplyDue — применить наступившие решения (каждое — под арендой исполнителя): статус, журнал, уведомление.
// Сколько применено.
func (s *Review) ApplyDue(ctx context.Context) (int, error) {
	n := 0
	for ctx.Err() == nil {
		now := s.d.Clock.Now()
		d, err := s.d.Repo.ClaimDueDecision(ctx, now, now.Add(s.cfg.ApplyLock))
		if errors.Is(err, domain.ErrNotFound) {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		if err := s.apply(ctx, d); err != nil {
			return n, err
		}
		n++
	}
	return n, ctx.Err()
}

func (s *Review) apply(ctx context.Context, d domain.ReviewDecision) error {
	subject := subjectOf(d.Kind)
	_, derr := s.d.Moderator.Decide(ctx, Decision{Subject: subject, ID: d.ItemID, ModeratorID: d.ModeratorID, Action: d.Action,
		Reason: d.Reason, Notes: d.Notes, Revision: d.Kind.Revision()})
	state, errText := domain.DecisionApplied, ""
	if derr != nil {
		var ve *ValidationError
		if !errors.Is(derr, ErrWrongStatus) && !errors.Is(derr, domain.ErrNotFound) && !errors.As(derr, &ve) {
			return derr // временный сбой базы — решение останется ждущим, исполнитель повторит после аренды
		}
		state, errText = domain.DecisionFailed, "статус уже изменился"
		s.d.Log.WarnContext(ctx, "решение не применено", "decision", d.ID, "err", derr)
	}
	var n *domain.Notice
	if state == domain.DecisionApplied {
		title, path := s.noticeTarget(ctx, d)
		n = &domain.Notice{Kind: d.Kind, ItemID: d.ItemID, Action: d.Action, Title: title, Reason: d.Reason, Path: path}
	}
	mod := domain.User{ID: d.ModeratorID, Name: "исполнитель решений"}
	e := auditFor(ctx, mod, domain.AuditDecisionApplied, string(d.Kind), strconv.FormatInt(d.ItemID, 10))
	e.ActorLabel, e.ActorRole = "система (решение модератора)", ""
	e.After = map[string]any{"decision": d.ID, "action": d.Action, "state": state}
	e.Reason = errText
	if err := s.d.Repo.FinishDecision(ctx, d, state, errText, s.d.Clock.Now(), e, n); err != nil {
		return err
	}
	if state == domain.DecisionApplied && s.d.Notifier != nil {
		s.notify(ctx, d)
	}
	return nil
}

// noticeTarget — название и адрес для уведомления владельца о решении.
func (s *Review) noticeTarget(ctx context.Context, d domain.ReviewDecision) (title, path string) {
	switch subjectOf(d.Kind) {
	case domain.SubjectTask:
		path = "/app/tasks/" + strconv.FormatInt(d.ItemID, 10)
		if t, err := s.d.Content.Task(ctx, d.ItemID); err == nil {
			title = t.Title
		}
	case domain.SubjectPage:
		path = "/app/page"
		if s.d.Pages != nil {
			if p, err := s.d.Pages.PageByID(ctx, d.ItemID); err == nil {
				title = p.Title
				path += "?kind=" + string(p.Kind)
			}
		}
	default:
		path = "/app/card"
		if c, err := s.d.Content.Card(ctx, d.ItemID); err == nil {
			title = c.Name
		}
	}
	return title, path
}

func (s *Review) notify(ctx context.Context, d domain.ReviewDecision) {
	title, path := s.noticeTarget(ctx, d)
	n := DecisionNotice{Kind: d.Kind, Action: d.Action, Reason: d.Reason, Notes: d.Notes, Title: title, Path: path}
	if err := s.d.Notifier.NotifyDecision(ctx, d.OwnerID, n); err != nil {
		s.d.Log.WarnContext(ctx, "уведомление о решении не отправлено", "decision", d.ID, "err", err)
	}
}

// Run — исполнитель решений: ждёт ближайшего (или сигнала о новом решении, не дольше 30 с) и применяет;
// раз в час — убирает истёкшие аренды.
func (s *Review) Run(ctx context.Context) {
	lastSweep := time.Time{}
	for {
		if _, err := s.ApplyDue(ctx); err != nil && ctx.Err() == nil {
			s.d.Log.ErrorContext(ctx, "решения: применение", "err", err)
		}
		now := s.d.Clock.Now()
		if now.Sub(lastSweep) > time.Hour {
			if _, err := s.d.Repo.DeleteExpiredLeases(ctx, now.Add(-time.Hour)); err != nil && ctx.Err() == nil {
				s.d.Log.WarnContext(ctx, "аренды: очистка", "err", err)
			}
			if err := s.d.Repo.DeleteOldNotifications(ctx, now.Add(-30*24*time.Hour)); err != nil && ctx.Err() == nil {
				s.d.Log.WarnContext(ctx, "уведомления: очистка", "err", err)
			}
			lastSweep = now
		}
		wait := 30 * time.Second
		if next, err := s.d.Repo.NextDecisionAt(ctx); err == nil && next != nil {
			if d := next.Sub(s.d.Clock.Now()) + 50*time.Millisecond; d < wait {
				wait = max(d, 100*time.Millisecond)
			}
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-s.wake:
			t.Stop()
		case <-t.C:
		}
	}
}

// ===== свои категории и форматы =====

// DecideCategory — своя категория: approve — в общие, merge — объединить с общей (into — её slug), reject —
// отклонить (причина). Сразу, с записью в журнал.
func (s *Review) DecideCategory(ctx context.Context, u domain.User, id int64, action, into, reason string) error {
	if err := authorize(u, domain.PermReview); err != nil {
		return err
	}
	c, err := s.d.Content.Category(ctx, id)
	if err != nil {
		return err
	}
	if c.Status != domain.CategoryPending {
		return ErrWrongStatus
	}
	e := auditFor(ctx, u, domain.AuditCategory, "category", strconv.FormatInt(id, 10))
	e.Before = map[string]string{"title": c.Title, "status": c.Status}
	switch action {
	case "approve", "reject":
		if _, err := s.d.Moderator.Decide(ctx, Decision{Subject: domain.SubjectCategory, ID: id, ModeratorID: u.ID,
			Approve: action == "approve", Reason: reason}); err != nil {
			return err
		}
		e.After, e.Reason = map[string]string{"status": map[string]string{"approve": domain.CategoryApproved, "reject": domain.CategoryRejected}[action]}, squash(reason)
		return s.d.Repo.Audit(ctx, e)
	case "merge":
		target, err := s.d.Repo.ApprovedCategory(ctx, into)
		if errors.Is(err, domain.ErrNotFound) {
			return &ValidationError{Fields: map[string]string{"into": "Выберите общую сферу из списка"}}
		}
		if err != nil {
			return err
		}
		e.After = map[string]string{"status": "merged", "into": target.Slug}
		ch := domain.StatusChange{Subject: domain.SubjectCategory, SubjectID: id, From: domain.Status(c.Status), Reason: "объединена с «" + target.Title + "»", ActorID: u.ID}
		return conflictToStatus(s.d.Repo.MergeCategory(ctx, ch, c.Slug, target.Slug, e))
	}
	return &ValidationError{Fields: map[string]string{"action": "Решение — одобрить, объединить или отклонить"}}
}

// DecideFormat — свой формат: approve — в общие подсказки, merge — заменить в заданиях на into (из списка или
// одобренный), reject — больше не принимается.
func (s *Review) DecideFormat(ctx context.Context, u domain.User, norm, action, into string) error {
	if err := authorize(u, domain.PermReview); err != nil {
		return err
	}
	norm = strings.TrimSpace(norm)
	if norm == "" || utf8.RuneCountInString(norm) > 60 {
		return domain.ErrNotFound
	}
	n, title, err := s.d.Repo.FormatUsage(ctx, norm)
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	status := map[string]string{"approve": "approved", "merge": "merged", "reject": "rejected"}[action]
	if status == "" {
		return &ValidationError{Fields: map[string]string{"action": "Решение — одобрить, объединить или отклонить"}}
	}
	if status == "merged" {
		targets, err := s.formatTargets(ctx)
		if err != nil {
			return err
		}
		if !contains(targets, into) || FormatNorm(into) == norm {
			return &ValidationError{Fields: map[string]string{"into": "Выберите формат из списка"}}
		}
	} else {
		into = ""
	}
	e := auditFor(ctx, u, domain.AuditFormat, "format", norm)
	e.Before = map[string]any{"title": title, "tasks": n}
	e.After = map[string]string{"status": status, "into": into}
	err = s.d.Repo.DecideFormat(ctx, norm, title, status, into, u.ID, e)
	return conflictToStatus(err)
}

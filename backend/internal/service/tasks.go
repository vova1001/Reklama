package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// ===== зависимости (только интерфейсы) =====

// TaskRepository — задания бизнеса.
type TaskRepository interface {
	CreateTask(ctx context.Context, t domain.Task) (domain.Task, error)
	// OwnTask — задание владельца; нет или чужое — domain.ErrNotFound.
	OwnTask(ctx context.Context, ownerID, id int64) (domain.Task, error)
	OwnTasks(ctx context.Context, ownerID int64, limit int) ([]domain.Task, error)
	CountTasks(ctx context.Context, ownerID int64) (int, error)
	// UpdateTask / DeleteTask — только черновик или отклонённое, иначе domain.ErrConflict.
	UpdateTask(ctx context.Context, t domain.Task) (domain.Task, error)
	DeleteTask(ctx context.Context, ownerID, id int64) error
	// SetTaskStatus — смены статуса по цепочке (каждая — из ch.From, иначе domain.ErrConflict и ничего не меняется)
	// и записи в журнал — атомарно.
	SetTaskStatus(ctx context.Context, chain ...domain.StatusChange) (domain.Task, error)
	// PublishedTasks — опубликованные по отбору, новые сверху, после курсора (nil — с начала).
	PublishedTasks(ctx context.Context, f domain.FeedFilter, after *domain.FeedCursor, limit int) ([]domain.Task, error)
	// PublishedTask — опубликованное задание; нет или не опубликовано — domain.ErrNotFound.
	PublishedTask(ctx context.Context, id int64) (domain.Task, error)
	// ExpireTasks — опубликованные со сроком не позже now — в «завершено» (с журналом); id завершённых.
	ExpireTasks(ctx context.Context, now time.Time) ([]int64, error)
	// Правка опубликованного: сохранить, сменить статус, удалить, заменить ею опубликованное (с цепочкой смен).
	SaveTaskRevision(ctx context.Context, ownerID, id int64, rev domain.Task) (domain.Task, error)
	SetTaskRevision(ctx context.Context, ch domain.StatusChange) (domain.Task, error)
	DropTaskRevision(ctx context.Context, ownerID, id int64) (domain.Task, error)
	ApplyTaskRevision(ctx context.Context, id int64, revStatus domain.Status, log domain.StatusChange, chain ...domain.StatusChange) (domain.Task, error)
}

// ModeSource — режим проверки из настроек админки (в базе); переменная окружения — значение по умолчанию.
type ModeSource interface {
	ModerationMode(ctx context.Context) domain.ModerationMode
}

// FormatPolicy — решения модератора по своим форматам: rejected — не принимается, merged — заменяется на into.
type FormatPolicy interface {
	FormatStatus(ctx context.Context, norm string) (status, into string, err error)
}

// SubmitCounter — сколько раз пользователь отправлял на проверку с момента since и когда впервые.
type SubmitCounter interface {
	SubmitStats(ctx context.Context, actorID int64, since time.Time) (int, time.Time, error)
}

// CategoryChecker — сколько из тем годится пользователю (общие или его свои на проверке) и сколько из них свои.
type CategoryChecker interface {
	Usable(ctx context.Context, userID int64, slugs []string) (usable, custom int, err error)
}

// ===== ошибки =====

var (
	// ErrWrongStatus — действие недоступно в текущем статусе (или статус уже сменился).
	ErrWrongStatus = errors.New("недоступно в этом статусе")
	// ErrTooManyTasks — достигнут предел заданий на пользователя.
	ErrTooManyTasks = errors.New("слишком много заданий")
)

// ===== сервис =====

// ListingLimits — лимиты на создание и отправку на проверку (заданий и анкет вместе).
type ListingLimits struct {
	MaxTasks      int // заданий на пользователя (все статусы), 50
	SubmitsPerDay int // отправок на проверку за сутки, 10
}

func (l *ListingLimits) defaults() {
	if l.MaxTasks <= 0 {
		l.MaxTasks = 50
	}
	if l.SubmitsPerDay <= 0 {
		l.SubmitsPerDay = 10
	}
}

// TaskInput — задание из формы. Черновик может быть неполным; полноту проверяет Submit.
type TaskInput struct {
	Title         string
	City          string
	Nationwide    bool
	Categories    []string // slug сфер, до 5
	Formats       []string // slug из списка или свои названия, до 3
	Access        string
	MinViews      int64
	RewardKopecks int64
	PublishBy     string // ГГГГ-ММ-ДД или ""
	PublishTime   string // ЧЧ:ММ по Москве или "" (до конца дня)
	Blocks        []domain.Block
	Cover         string   // media.id обложки
	Photos        []string // media.id примеров, по порядку
}

// ListingDeps — от чего зависят задания и анкеты.
type ListingDeps struct {
	Categories CategoryChecker
	Submits    SubmitCounter
	Media      MediaLookup
	Platforms  PlatformSource // подтверждённые соцсети в анкетах; nil — без них
	Clock      Clock          // nil — системное время
	Limits     ListingLimits
	// Mode — проверка отправленного: auto — сразу публикуется (журнал: «одобрено автоматически»),
	// manual (и пусто) — ждёт модератора. Modes (если задан) — текущий режим из админки, Mode — по умолчанию.
	Mode  domain.ModerationMode
	Modes ModeSource
	// FormatRules — решения по своим форматам (nil — свои форматы без проверки).
	FormatRules FormatPolicy
	// PageLinks — адреса опубликованных страниц владельцев (ссылка «Профиль» из ленты и страницы); nil — без них.
	PageLinks PageLinker
}

// PageLinker — адреса опубликованных страниц пользователей вида kind (нет страницы — нет в ответе).
type PageLinker interface {
	PublishedSlugs(ctx context.Context, kind domain.PageKind, userIDs []int64) (map[int64]string, error)
}

// pageSlugs — адреса страниц владельцев; без PageLinks — пусто.
func (d *ListingDeps) pageSlugs(ctx context.Context, kind domain.PageKind, ids []int64) (map[int64]string, error) {
	if d.PageLinks == nil || len(ids) == 0 {
		return map[int64]string{}, nil
	}
	return d.PageLinks.PublishedSlugs(ctx, kind, uniqIDs(ids))
}

// mode — текущий режим проверки.
func (d *ListingDeps) mode(ctx context.Context) domain.ModerationMode {
	if d.Modes != nil {
		if m := d.Modes.ModerationMode(ctx); m == domain.ModerationAuto || m == domain.ModerationManual {
			return m
		}
	}
	return d.Mode
}

// applyFormatRules — свои форматы по решениям модератора: отклонённый — ошибка поля, объединённый — заменяется.
func (d *ListingDeps) applyFormatRules(ctx context.Context, formats []string) ([]string, error) {
	if d.FormatRules == nil {
		return formats, nil
	}
	out := make([]string, 0, len(formats))
	for _, f := range formats {
		if _, ok := builtinFormat(f); ok {
			out = append(out, f)
			continue
		}
		st, into, err := d.FormatRules.FormatStatus(ctx, FormatNorm(f))
		if err != nil {
			return nil, err
		}
		switch st {
		case "rejected":
			return nil, &ValidationError{Fields: map[string]string{"formats": "Формат «" + f + "» не подходит — выберите другой"}}
		case "merged":
			f = into
		}
		if !contains(out, f) {
			out = append(out, f)
		}
	}
	return out, nil
}

// FormatNorm — ключ своего формата для решений модератора: нижний регистр, ё → е (так же считает база).
func FormatNorm(f string) string { return strings.ReplaceAll(strings.ToLower(f), "ё", "е") }

func (d *ListingDeps) defaults() {
	if d.Clock == nil {
		d.Clock = SystemClock{}
	}
	if d.Mode != domain.ModerationAuto {
		d.Mode = domain.ModerationManual
	}
	d.Limits.defaults()
}

// Сколько фото: в задании — обложка и до 6 примеров, в анкете — аватар и до 9 работ.
const (
	maxTaskPhotos = 6
	maxCardWorks  = 9
)

// Tasks — задания бизнеса: черновик → на проверку (лимиты) → решение модератора; лента опубликованных.
type Tasks struct {
	repo TaskRepository
	ListingDeps
}

// NewTasks создаёт сервис заданий.
func NewTasks(repo TaskRepository, d ListingDeps) *Tasks {
	d.defaults()
	return &Tasks{repo: repo, ListingDeps: d}
}

const ownTasksLimit = 100

// List — свои задания (все статусы), недавно изменённые сверху.
func (s *Tasks) List(ctx context.Context, u domain.User) ([]domain.Task, error) {
	return s.repo.OwnTasks(ctx, u.ID, ownTasksLimit)
}

// Get — своё задание; чужое или нет — domain.ErrNotFound.
func (s *Tasks) Get(ctx context.Context, u domain.User, id int64) (domain.Task, error) {
	return s.repo.OwnTask(ctx, u.ID, id)
}

// Create — новый черновик (в пределах лимита заданий).
func (s *Tasks) Create(ctx context.Context, u domain.User, in TaskInput) (domain.Task, error) {
	t, err := s.shape(ctx, u.ID, 0, in)
	if err != nil {
		return domain.Task{}, err
	}
	n, err := s.repo.CountTasks(ctx, u.ID)
	if err != nil {
		return domain.Task{}, err
	}
	if n >= s.Limits.MaxTasks {
		return domain.Task{}, ErrTooManyTasks
	}
	t.OwnerID = u.ID
	return s.repo.CreateTask(ctx, t)
}

// Update — правка черновика или отклонённого задания (статус не меняется).
func (s *Tasks) Update(ctx context.Context, u domain.User, id int64, in TaskInput) (domain.Task, error) {
	cur, err := s.repo.OwnTask(ctx, u.ID, id)
	if err != nil {
		return domain.Task{}, err
	}
	revision := cur.Status == domain.StatusPublished
	if !cur.Status.Editable() && !(revision && domain.RevisionEditable(cur.RevisionStatus)) {
		return domain.Task{}, ErrWrongStatus
	}
	t, err := s.shape(ctx, u.ID, id, in)
	if err != nil {
		return domain.Task{}, err
	}
	if revision { // правка опубликованного: новая версия ждёт проверки, опубликованная видна всем
		out, err := s.repo.SaveTaskRevision(ctx, u.ID, id, t)
		return out, conflictToStatus(err)
	}
	t.ID, t.OwnerID = id, u.ID
	out, err := s.repo.UpdateTask(ctx, t)
	return out, conflictToStatus(err)
}

// DiscardRevision — удалить правку опубликованного (опубликованное остаётся как было).
func (s *Tasks) DiscardRevision(ctx context.Context, u domain.User, id int64) (domain.Task, error) {
	out, err := s.repo.DropTaskRevision(ctx, u.ID, id)
	if errors.Is(err, domain.ErrConflict) {
		if _, gerr := s.repo.OwnTask(ctx, u.ID, id); gerr != nil {
			return domain.Task{}, gerr
		}
	}
	return out, conflictToStatus(err)
}

// Delete — удалить черновик, отклонённое или завершённое задание.
func (s *Tasks) Delete(ctx context.Context, u domain.User, id int64) error {
	cur, err := s.repo.OwnTask(ctx, u.ID, id)
	if err != nil {
		return err
	}
	if !cur.Status.Editable() && cur.Status != domain.StatusExpired {
		return ErrWrongStatus
	}
	return conflictToStatus(s.repo.DeleteTask(ctx, u.ID, id))
}

// Submit — отправить на проверку: задание заполнено полностью, лимит отправок за сутки не исчерпан.
// В автоматическом режиме задание публикуется сразу (в той же транзакции).
func (s *Tasks) Submit(ctx context.Context, u domain.User, id int64) (domain.Task, error) {
	cur, err := s.repo.OwnTask(ctx, u.ID, id)
	if err != nil {
		return domain.Task{}, err
	}
	if cur.Status == domain.StatusPublished && cur.Revision != nil {
		return s.submitRevision(ctx, u, cur)
	}
	if !domain.CanTransition(cur.Status, domain.StatusPending, domain.ActorOwner) {
		return domain.Task{}, ErrWrongStatus
	}
	if err := s.complete(cur); err != nil {
		return domain.Task{}, err
	}
	if err := checkSubmitQuota(ctx, s.Submits, s.Clock, u.ID, s.Limits.SubmitsPerDay); err != nil {
		return domain.Task{}, err
	}
	out, err := s.repo.SetTaskStatus(ctx, domain.SubmitChain(s.mode(ctx), domain.SubjectTask, id, cur.Status, u.ID)...)
	return out, conflictToStatus(err)
}

// submitRevision — правку опубликованного — на проверку; в автоматическом режиме — сразу заменяет опубликованное.
func (s *Tasks) submitRevision(ctx context.Context, u domain.User, cur domain.Task) (domain.Task, error) {
	if !domain.RevisionTransition(cur.RevisionStatus, domain.StatusPending, domain.ActorOwner) {
		return domain.Task{}, ErrWrongStatus
	}
	if err := s.complete(*cur.Revision); err != nil {
		return domain.Task{}, err
	}
	if err := checkSubmitQuota(ctx, s.Submits, s.Clock, u.ID, s.Limits.SubmitsPerDay); err != nil {
		return domain.Task{}, err
	}
	out, err := s.repo.SetTaskRevision(ctx, domain.StatusChange{Subject: domain.SubjectTask, SubjectID: cur.ID, From: cur.RevisionStatus, To: domain.StatusPending, ActorID: u.ID})
	if err != nil || s.mode(ctx) != domain.ModerationAuto {
		return out, conflictToStatus(err)
	}
	out, err = s.repo.ApplyTaskRevision(ctx, cur.ID, domain.StatusPending,
		domain.StatusChange{Subject: domain.SubjectTask, SubjectID: cur.ID, From: domain.StatusPending, To: domain.StatusPublished, Reason: domain.AutoApproveNote, Auto: true})
	return out, conflictToStatus(err)
}

// Withdraw — вернуть в черновик (чтобы поправить): с проверки или снять с публикации.
func (s *Tasks) Withdraw(ctx context.Context, u domain.User, id int64) (domain.Task, error) {
	cur, err := s.repo.OwnTask(ctx, u.ID, id)
	if err != nil {
		return domain.Task{}, err
	}
	if !domain.CanTransition(cur.Status, domain.StatusDraft, domain.ActorOwner) {
		return domain.Task{}, ErrWrongStatus
	}
	ch := domain.StatusChange{Subject: domain.SubjectTask, SubjectID: id, From: cur.Status, To: domain.StatusDraft, ActorID: u.ID}
	if cur.Status == domain.StatusPublished && cur.Revision != nil {
		// снять с публикации с правкой: черновиком становится новая версия (её больше никто не видит)
		out, err := s.repo.ApplyTaskRevision(ctx, id, cur.RevisionStatus,
			domain.StatusChange{Subject: domain.SubjectTask, SubjectID: id, From: cur.RevisionStatus, To: domain.StatusDraft, ActorID: u.ID}, ch)
		return out, conflictToStatus(err)
	}
	out, err := s.repo.SetTaskStatus(ctx, ch)
	return out, conflictToStatus(err)
}

// FeedQuery — запрос страницы ленты: отбор, курсор (nil — с начала), размер (1–50, иначе 20).
type FeedQuery struct {
	Filter domain.FeedFilter
	After  *domain.FeedCursor
	Limit  int
}

// FeedPage — страница ленты заданий и курсор следующей (nil — дальше нет).
type FeedPage struct {
	Tasks []domain.Task
	Next  *domain.FeedCursor
}

// Feed — опубликованные задания по отбору, новые сверху.
func (s *Tasks) Feed(ctx context.Context, q FeedQuery) (FeedPage, error) {
	f, limit, err := feedFilter(q, true)
	if err != nil {
		return FeedPage{}, err
	}
	ts, err := s.repo.PublishedTasks(ctx, f, q.After, limit+1)
	if err != nil {
		return FeedPage{}, err
	}
	page := FeedPage{Tasks: ts}
	if len(ts) > limit {
		page.Tasks = ts[:limit]
		page.Next = cursorOf(page.Tasks[limit-1].PublishedAt, page.Tasks[limit-1].ID)
	}
	return page, s.withBrandLinks(ctx, page.Tasks)
}

// Public — опубликованное задание (страница задания для всех); иначе domain.ErrNotFound.
func (s *Tasks) Public(ctx context.Context, id int64) (domain.Task, error) {
	t, err := s.repo.PublishedTask(ctx, id)
	if err != nil {
		return t, err
	}
	ts := []domain.Task{t}
	return ts[0], s.withBrandLinks(ctx, ts)
}

// withBrandLinks — адрес страницы бизнеса у каждого задания (одним запросом на страницу ленты).
func (s *Tasks) withBrandLinks(ctx context.Context, ts []domain.Task) error {
	ids := make([]int64, 0, len(ts))
	for _, t := range ts {
		ids = append(ids, t.OwnerID)
	}
	m, err := s.pageSlugs(ctx, domain.PageBusiness, ids)
	if err != nil {
		return err
	}
	for i := range ts {
		ts[i].BrandSlug = m[ts[i].OwnerID]
	}
	return nil
}

// feedFilter — проверка отбора ленты: сферы — slug (до 5), город — до 80 символов, форматы — из списка
// (до 6, только задания). Пустые значения отбрасываются.
func feedFilter(q FeedQuery, tasks bool) (domain.FeedFilter, int, error) {
	ve := &ValidationError{}
	f := domain.FeedFilter{City: squash(q.Filter.City)}
	field, pick := "topic", "Выберите тему из списка"
	if tasks {
		field, pick = "category", "Выберите сферу из списка"
	}
	f.Categories = checkSlugs(ve, field, nonEmptyAll(q.Filter.Categories), "в отборе", pick)
	maxLen(ve, "city", f.City, 80, "Город — до 80 символов")
	for _, raw := range nonEmptyAll(q.Filter.Formats) {
		bf, ok := builtinFormat(raw)
		if !tasks || !ok || raw != bf.ID {
			ve.add("format", "Выберите формат из списка")
			break
		}
		if !contains(f.Formats, bf.ID) {
			f.Formats = append(f.Formats, bf.ID)
		}
	}
	limit := q.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return f, limit, ve.orNil()
}

func cursorOf(at *time.Time, id int64) *domain.FeedCursor {
	if at == nil {
		return nil
	}
	return &domain.FeedCursor{PublishedAt: *at, ID: id}
}

// shape — проверка формы (любое сохранение): длины, перечни, числа, дата, блоки; сфера — общая или своя;
// фото — свои, нужного назначения, не больше предела. id — 0 для нового задания.
func (s *Tasks) shape(ctx context.Context, userID, id int64, in TaskInput) (domain.Task, error) {
	ve := &ValidationError{}
	t := domain.Task{
		Title: squash(in.Title), City: squash(in.City), Nationwide: in.Nationwide,
		Access: in.Access, MinViews: in.MinViews, RewardKopecks: in.RewardKopecks,
	}
	maxLen(ve, "title", t.Title, 100, "Название — до 100 символов")
	if t.Nationwide {
		t.City = ""
	}
	maxLen(ve, "city", t.City, 80, "Город — до 80 символов")
	t.Categories = checkSlugs(ve, "categories", in.Categories, "сфер", "Выберите сферу из списка")
	t.Formats = checkFormats(ve, in.Formats)
	if len(ve.Fields) == 0 {
		f, err := s.applyFormatRules(ctx, t.Formats)
		var fe *ValidationError
		switch {
		case errors.As(err, &fe):
			ve.add("formats", fe.Fields["formats"])
		case err != nil:
			return domain.Task{}, err
		default:
			t.Formats = f
		}
	}
	switch t.Access {
	case "":
		t.Access = domain.AccessOpen
	case domain.AccessOpen, domain.AccessApproval:
	default:
		ve.add("access", "Тип — открытое или с одобрением")
	}
	checkCount(ve, "minViews", t.MinViews)
	if t.RewardKopecks < 0 || t.RewardKopecks > maxRewardKop {
		ve.add("reward", "Оплата — до 1 000 000 ₽")
	}
	t.PublishBy = parseDate(ve, "publishBy", in.PublishBy)
	t.PublishTime = parseClock(ve, "publishTime", in.PublishTime)
	t.DeadlineAt = deadline(t.PublishBy, t.PublishTime)
	t.Blocks = checkBlocks(ve, in.Blocks)
	cover, err := checkMedia(ctx, s.Media, ve, userID, nonEmpty(in.Cover), mediaSpec{"cover", domain.PurposeTaskCover, 1, domain.SubjectTask, id})
	if err != nil {
		return domain.Task{}, err
	}
	if len(cover) == 1 {
		t.Cover = cover[0]
	}
	if t.Photos, err = checkMedia(ctx, s.Media, ve, userID, in.Photos, mediaSpec{"photos", domain.PurposeTaskPhoto, maxTaskPhotos, domain.SubjectTask, id}); err != nil {
		return domain.Task{}, err
	}
	if err := ve.orNil(); err != nil {
		return domain.Task{}, err
	}
	if err := checkUsable(ctx, s.Categories, userID, "categories", t.Categories, "Выберите сферу из списка", "Своих сфер — не больше %d"); err != nil {
		return domain.Task{}, err
	}
	return t, nil
}

// complete — к отправке на проверку: всё обязательное заполнено, срок — от завтра до 90 дней (по Москве).
func (s *Tasks) complete(t domain.Task) error {
	ve := &ValidationError{}
	if n := utf8.RuneCountInString(t.Title); n < 5 || n > 100 {
		ve.add("title", "Название — от 5 до 100 символов")
	}
	if !t.Nationwide && utf8.RuneCountInString(t.City) < 2 {
		ve.add("city", "Укажите город или «Вся Россия»")
	}
	if len(t.Categories) == 0 {
		ve.add("categories", "Выберите сферу")
	}
	if len(t.Formats) == 0 {
		ve.add("formats", "Выберите хотя бы один формат")
	}
	if t.MinViews <= 0 {
		ve.add("minViews", "Укажите число просмотров")
	}
	if t.RewardKopecks < minRewardKop {
		ve.add("reward", "Оплата — от 100 ₽")
	}
	day := today(s.Clock.Now())
	switch {
	case t.PublishBy == nil:
		ve.add("publishBy", "Укажите срок публикации")
	case !t.PublishBy.After(day) || t.PublishBy.After(day.AddDate(0, 0, maxDeadlineDay)):
		ve.add("publishBy", "Срок — от завтра до 90 дней")
	}
	if !blocksFilled(t.Blocks) {
		ve.add("blocks", "Опишите задание: добавьте описание или требования")
	}
	return ve.orNil()
}

// checkSubmitQuota — не больше perDay отправок на проверку за последние сутки.
func checkSubmitQuota(ctx context.Context, subs SubmitCounter, clock Clock, userID int64, perDay int) error {
	now := clock.Now()
	n, first, err := subs.SubmitStats(ctx, userID, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if n >= perDay {
		wait := first.Add(24 * time.Hour).Sub(now)
		if wait < time.Minute {
			wait = time.Minute
		}
		return &LimitError{Reason: "submits_daily", RetryAfter: wait}
	}
	return nil
}

// nonEmptyAll — без пустых строк (после squash).
func nonEmptyAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = squash(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func conflictToStatus(err error) error {
	if errors.Is(err, domain.ErrConflict) {
		return fmt.Errorf("%w: статус сменился", ErrWrongStatus)
	}
	return err
}

// Expire — опубликованные задания с прошедшим сроком — в «завершено»; сколько завершено.
func (s *Tasks) Expire(ctx context.Context) (int, error) {
	ids, err := s.repo.ExpireTasks(ctx, s.Clock.Now())
	return len(ids), err
}

// RunExpiry — раз в every завершать просроченные, пока не отменён ctx (из ленты они пропадают сразу — по сроку).
func (s *Tasks) RunExpiry(ctx context.Context, every time.Duration, log *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if n, err := s.Expire(ctx); err != nil {
			log.Error("задания: завершение просроченных", "err", err)
		} else if n > 0 {
			log.Info("задания: завершены просроченные", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

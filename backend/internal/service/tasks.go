package service

import (
	"context"
	"errors"
	"fmt"
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
	Category      string
	Formats       []string
	Access        string
	MinViews      int64
	MinLikes      int64
	RewardKopecks int64
	PublishBy     string // ГГГГ-ММ-ДД или ""
	Blocks        []domain.Block
	Cover         string   // media.id обложки
	Photos        []string // media.id примеров, по порядку
}

// ListingDeps — от чего зависят задания и анкеты.
type ListingDeps struct {
	Categories CategoryChecker
	Submits    SubmitCounter
	Media      MediaLookup
	Clock      Clock // nil — системное время
	Limits     ListingLimits
	// Mode — проверка отправленного: auto — сразу публикуется (журнал: «одобрено автоматически»),
	// manual (и пусто) — ждёт модератора.
	Mode domain.ModerationMode
}

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
	if !cur.Status.Editable() {
		return domain.Task{}, ErrWrongStatus
	}
	t, err := s.shape(ctx, u.ID, id, in)
	if err != nil {
		return domain.Task{}, err
	}
	t.ID, t.OwnerID = id, u.ID
	out, err := s.repo.UpdateTask(ctx, t)
	return out, conflictToStatus(err)
}

// Delete — удалить черновик или отклонённое задание.
func (s *Tasks) Delete(ctx context.Context, u domain.User, id int64) error {
	cur, err := s.repo.OwnTask(ctx, u.ID, id)
	if err != nil {
		return err
	}
	if !cur.Status.Editable() {
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
	if !domain.CanTransition(cur.Status, domain.StatusPending, domain.ActorOwner) {
		return domain.Task{}, ErrWrongStatus
	}
	if err := s.complete(cur); err != nil {
		return domain.Task{}, err
	}
	if err := checkSubmitQuota(ctx, s.Submits, s.Clock, u.ID, s.Limits.SubmitsPerDay); err != nil {
		return domain.Task{}, err
	}
	out, err := s.repo.SetTaskStatus(ctx, domain.SubmitChain(s.Mode, domain.SubjectTask, id, cur.Status, u.ID)...)
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
	out, err := s.repo.SetTaskStatus(ctx, domain.StatusChange{Subject: domain.SubjectTask, SubjectID: id, From: cur.Status, To: domain.StatusDraft, ActorID: u.ID})
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
	return page, nil
}

// Public — опубликованное задание (страница задания для всех); иначе domain.ErrNotFound.
func (s *Tasks) Public(ctx context.Context, id int64) (domain.Task, error) {
	return s.repo.PublishedTask(ctx, id)
}

// feedFilter — проверка отбора ленты: сфера — slug, город — до 80 символов, формат — из списка (только задания).
func feedFilter(q FeedQuery, tasks bool) (domain.FeedFilter, int, error) {
	f := domain.FeedFilter{Category: squash(q.Filter.Category), City: squash(q.Filter.City), Format: squash(q.Filter.Format)}
	ve := &ValidationError{}
	if f.Category != "" && !reSlug.MatchString(f.Category) {
		ve.add("category", "Выберите сферу из списка")
	}
	maxLen(ve, "city", f.City, 80, "Город — до 80 символов")
	if f.Format != "" && (!tasks || !contains(Formats, f.Format)) {
		ve.add("format", "Выберите формат из списка")
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
		Title: squash(in.Title), City: squash(in.City), Nationwide: in.Nationwide, Category: squash(in.Category),
		Access: in.Access, MinViews: in.MinViews, MinLikes: in.MinLikes, RewardKopecks: in.RewardKopecks,
	}
	maxLen(ve, "title", t.Title, 100, "Название — до 100 символов")
	if t.Nationwide {
		t.City = ""
	}
	maxLen(ve, "city", t.City, 80, "Город — до 80 символов")
	if t.Category != "" && !reSlug.MatchString(t.Category) {
		ve.add("category", "Выберите сферу из списка")
	}
	t.Formats = checkFormats(ve, in.Formats)
	switch t.Access {
	case "":
		t.Access = domain.AccessOpen
	case domain.AccessOpen, domain.AccessApproval:
	default:
		ve.add("access", "Тип — открытое или с одобрением")
	}
	checkCount(ve, "minViews", t.MinViews)
	checkCount(ve, "minLikes", t.MinLikes)
	if t.RewardKopecks < 0 || t.RewardKopecks > maxRewardKop {
		ve.add("reward", "Оплата — до 1 000 000 ₽")
	}
	t.PublishBy = parseDate(ve, "publishBy", in.PublishBy)
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
	if t.Category != "" {
		n, _, err := s.Categories.Usable(ctx, userID, []string{t.Category})
		if err != nil {
			return domain.Task{}, err
		}
		if n != 1 {
			return domain.Task{}, &ValidationError{Fields: map[string]string{"category": "Выберите сферу из списка"}}
		}
	}
	return t, nil
}

// complete — к отправке на проверку: всё обязательное заполнено, срок — от завтра до полугода.
func (s *Tasks) complete(t domain.Task) error {
	ve := &ValidationError{}
	if n := utf8.RuneCountInString(t.Title); n < 5 || n > 100 {
		ve.add("title", "Название — от 5 до 100 символов")
	}
	if !t.Nationwide && utf8.RuneCountInString(t.City) < 2 {
		ve.add("city", "Укажите город или «Вся Россия»")
	}
	if t.Category == "" {
		ve.add("category", "Выберите сферу")
	}
	if len(t.Formats) == 0 {
		ve.add("formats", "Выберите хотя бы один формат")
	}
	if t.MinViews == 0 && t.MinLikes == 0 {
		ve.add("minViews", "Укажите просмотры или лайки")
	}
	if t.RewardKopecks < minRewardKop {
		ve.add("reward", "Оплата — от 100 ₽")
	}
	day := today(s.Clock.Now())
	switch {
	case t.PublishBy == nil:
		ve.add("publishBy", "Укажите срок публикации")
	case !t.PublishBy.After(day) || t.PublishBy.After(day.AddDate(0, 0, maxDeadlineDay)):
		ve.add("publishBy", "Срок — от завтра до полугода")
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

package service

import (
	"context"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// ===== жалобы: «Пожаловаться» у пользователей, очередь и решения в админке =====

// ComplaintRepository — жалобы.
type ComplaintRepository interface {
	CreateComplaint(ctx context.Context, c domain.Complaint) (bool, error)
	ComplaintsSince(ctx context.Context, reporterID int64, since time.Time) (int, error)
	ComplaintGroups(ctx context.Context, subject string, limit, offset int) ([]domain.ComplaintGroup, int, error)
	ComplaintsFor(ctx context.Context, subject string, id int64) ([]domain.Complaint, error)
	ResolveComplaints(ctx context.Context, subject string, id int64, status, action, resolution string, by int64, now time.Time, e domain.AuditEntry) (int, error)
}

// ComplaintTargets — на что жалуются: опубликованное задание, анкета, профиль (только существующие и видимые).
type ComplaintTargets interface {
	PublishedTask(ctx context.Context, id int64) (domain.Task, error)
	PublishedCard(ctx context.Context, userID int64) (domain.CreatorCard, error)
	UserFull(ctx context.Context, id int64) (domain.UserSummary, error)
}

// UserBlocker — блокировка пользователя (решение по жалобе «заблокировать»).
type UserBlocker interface {
	Block(ctx context.Context, actor domain.User, id int64, reason string) (domain.User, error)
}

// ComplaintsConfig — лимит жалоб на пользователя в сутки (20).
type ComplaintsConfig struct{ PerDay int }

// Complaints — жалобы пользователей и решения модераторов.
type Complaints struct {
	repo    ComplaintRepository
	targets ComplaintTargets
	mod     Moderator
	users   UserBlocker
	clock   Clock
	cfg     ComplaintsConfig
}

// NewComplaints создаёт сервис жалоб.
func NewComplaints(repo ComplaintRepository, targets ComplaintTargets, mod Moderator, users UserBlocker, clock Clock, cfg ComplaintsConfig) *Complaints {
	if clock == nil {
		clock = SystemClock{}
	}
	if cfg.PerDay <= 0 {
		cfg.PerDay = 20
	}
	return &Complaints{repo: repo, targets: targets, mod: mod, users: users, clock: clock, cfg: cfg}
}

// ComplaintInput — жалоба из формы.
type ComplaintInput struct {
	Subject string // task | creator_card | user
	ID      int64
	Reason  string
	Comment string
}

// Create — пожаловаться: причина из списка, комментарий до 1000 символов (для «другое» — от 5), не на себя,
// не больше 20 в сутки. Повтор той же открытой жалобы — без новой записи (created=false).
func (s *Complaints) Create(ctx context.Context, u domain.User, in ComplaintInput) (bool, error) {
	ve := &ValidationError{}
	in.Comment = squash(in.Comment)
	if !contains(domain.ComplaintReasons, in.Reason) {
		ve.add("reason", "Выберите причину")
	}
	if n := utf8.RuneCountInString(in.Comment); n > 1000 {
		ve.add("comment", "Комментарий — до 1000 символов")
	} else if in.Reason == "other" && n < 5 {
		ve.add("comment", "Опишите, что не так (от 5 символов)")
	}
	if err := ve.orNil(); err != nil {
		return false, err
	}
	var target int64
	switch in.Subject {
	case string(domain.SubjectTask):
		t, err := s.targets.PublishedTask(ctx, in.ID)
		if err != nil {
			return false, err
		}
		target = t.OwnerID
	case string(domain.SubjectCard):
		c, err := s.targets.PublishedCard(ctx, in.ID)
		if err != nil {
			return false, err
		}
		target = c.UserID
	case "user":
		t, err := s.targets.UserFull(ctx, in.ID)
		if err != nil {
			return false, err
		}
		if t.Blocked() {
			return false, domain.ErrNotFound
		}
		target = t.ID
	default:
		return false, domain.ErrNotFound
	}
	if target == u.ID {
		return false, ErrSelfAction
	}
	now := s.clock.Now()
	n, err := s.repo.ComplaintsSince(ctx, u.ID, now.Add(-24*time.Hour))
	if err != nil {
		return false, err
	}
	if n >= s.cfg.PerDay {
		return false, &LimitError{Reason: "complaints_daily", RetryAfter: time.Hour}
	}
	return s.repo.CreateComplaint(ctx, domain.Complaint{ReporterID: u.ID, Subject: in.Subject, SubjectID: in.ID, TargetUserID: target,
		Reason: in.Reason, Comment: in.Comment})
}

// Groups — очередь жалоб: предметы с открытыми жалобами, старые первыми.
func (s *Complaints) Groups(ctx context.Context, u domain.User, subject string, limit, offset int) ([]domain.ComplaintGroup, int, error) {
	if err := authorize(u, domain.PermComplaints); err != nil {
		return nil, 0, err
	}
	if subject != "" && subject != string(domain.SubjectTask) && subject != string(domain.SubjectCard) && subject != "user" {
		return nil, 0, &ValidationError{Fields: map[string]string{"subject": "Неизвестный тип"}}
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.ComplaintGroups(ctx, subject, limit, offset)
}

// ComplaintCase — жалобы на предмет и сам предмет (что видят пользователи).
type ComplaintCase struct {
	Subject    string
	ID         int64
	Complaints []domain.Complaint
	Task       *domain.Task
	Card       *domain.CreatorCard
	Target     *domain.UserSummary
}

// Case — жалобы на предмет с содержимым.
func (s *Complaints) Case(ctx context.Context, u domain.User, subject string, id int64) (ComplaintCase, error) {
	if err := authorize(u, domain.PermComplaints); err != nil {
		return ComplaintCase{}, err
	}
	cs, err := s.repo.ComplaintsFor(ctx, subject, id)
	if err != nil {
		return ComplaintCase{}, err
	}
	if len(cs) == 0 {
		return ComplaintCase{}, domain.ErrNotFound
	}
	out := ComplaintCase{Subject: subject, ID: id, Complaints: cs}
	switch subject {
	case string(domain.SubjectTask):
		if t, err := s.targets.PublishedTask(ctx, id); err == nil {
			out.Task = &t
		} else if !errors.Is(err, domain.ErrNotFound) {
			return out, err
		}
	case string(domain.SubjectCard):
		if c, err := s.targets.PublishedCard(ctx, id); err == nil {
			out.Card = &c
		} else if !errors.Is(err, domain.ErrNotFound) {
			return out, err
		}
	}
	if t, err := s.targets.UserFull(ctx, cs[0].TargetUserID); err == nil {
		out.Target = &t
	} else if !errors.Is(err, domain.ErrNotFound) {
		return out, err
	}
	return out, nil
}

// Resolve — решение по жалобам на предмет: dismiss — не подтвердилась; hide — снять с публикации (причина видна
// владельцу); block — заблокировать владельца. Закрываются все открытые жалобы на предмет; в журнал.
func (s *Complaints) Resolve(ctx context.Context, u domain.User, subject string, id int64, action, note string) (int, error) {
	if err := authorize(u, domain.PermComplaints); err != nil {
		return 0, err
	}
	note = squash(note)
	if n := utf8.RuneCountInString(note); n > 500 || (action != domain.ComplaintDismiss && n < 5) {
		return 0, &ValidationError{Fields: map[string]string{"note": "Причина — от 5 до 500 символов"}}
	}
	cs, err := s.repo.ComplaintsFor(ctx, subject, id)
	if err != nil {
		return 0, err
	}
	if len(cs) == 0 {
		return 0, domain.ErrNotFound
	}
	status := "resolved"
	switch action {
	case domain.ComplaintDismiss:
		status = "dismissed"
	case domain.ComplaintHide:
		var subj domain.Subject
		switch subject {
		case string(domain.SubjectTask):
			subj = domain.SubjectTask
		case string(domain.SubjectCard):
			subj = domain.SubjectCard
		default:
			return 0, &ValidationError{Fields: map[string]string{"action": "Профиль не снимается — заблокируйте пользователя"}}
		}
		if _, err := s.mod.Decide(ctx, Decision{Subject: subj, ID: id, ModeratorID: u.ID, Action: domain.ActionReject, Reason: note}); err != nil && !errors.Is(err, ErrWrongStatus) {
			return 0, err
		}
	case domain.ComplaintBlock:
		if _, err := s.users.Block(ctx, u, cs[0].TargetUserID, note); err != nil && !errors.Is(err, ErrWrongStatus) {
			return 0, err
		}
	default:
		return 0, &ValidationError{Fields: map[string]string{"action": "Решение — отклонить жалобу, снять или заблокировать"}}
	}
	e := auditFor(ctx, u, domain.AuditComplaint, subject, strconv.FormatInt(id, 10))
	e.Reason = note
	n, err := s.repo.ResolveComplaints(ctx, subject, id, status, action, note, u.ID, s.clock.Now(), e)
	if errors.Is(err, domain.ErrNotFound) {
		return 0, ErrWrongStatus
	}
	return n, err
}

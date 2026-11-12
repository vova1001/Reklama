package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// Moderator — решение модератора по заданию, анкете (или их правке) и своей категории; журнал смен статуса.
// Транспорт админки зависит от этого интерфейса (через сервис Review), а не от реализации.
type Moderator interface {
	Decide(ctx context.Context, d Decision) (domain.Status, error)
	History(ctx context.Context, subject domain.Subject, id int64) ([]domain.StatusChange, error)
}

// ModerationRepository — что модерации нужно от хранилища.
type ModerationRepository interface {
	Task(ctx context.Context, id int64) (domain.Task, error)
	SetTaskStatus(ctx context.Context, chain ...domain.StatusChange) (domain.Task, error)
	Card(ctx context.Context, userID int64) (domain.CreatorCard, error)
	SetCardStatus(ctx context.Context, chain ...domain.StatusChange) (domain.CreatorCard, error)
	History(ctx context.Context, subject domain.Subject, id int64) ([]domain.StatusChange, error)
	// Category / SetCategoryStatus — своя категория: «на проверке» → общая или отклонённая.
	Category(ctx context.Context, id int64) (domain.CategoryInfo, error)
	SetCategoryStatus(ctx context.Context, ch domain.StatusChange) (domain.CategoryInfo, error)
	// Правки опубликованного: статус правки и замена опубликованного содержимого правкой.
	SetTaskRevision(ctx context.Context, ch domain.StatusChange) (domain.Task, error)
	ApplyTaskRevision(ctx context.Context, id int64, revStatus domain.Status, log domain.StatusChange, chain ...domain.StatusChange) (domain.Task, error)
	SetCardRevision(ctx context.Context, ch domain.StatusChange) (domain.CreatorCard, error)
	ApplyCardRevision(ctx context.Context, userID int64, revStatus domain.Status, log domain.StatusChange, chain ...domain.StatusChange) (domain.CreatorCard, error)
}

// Decision — решение: опубликовать, отклонить с причиной или вернуть на доработку с замечаниями по полям.
type Decision struct {
	Subject     domain.Subject
	ID          int64 // tasks.id, user_id анкеты или categories.id
	ModeratorID int64
	Action      domain.ReviewAction // пусто — по Approve (опубликовать или отклонить)
	Approve     bool
	Reason      string            // причина отказа (её увидит владелец)
	Notes       map[string]string // замечания «на доработку»: поле → текст
	Revision    bool              // решение по правке опубликованного
}

func (d Decision) action() domain.ReviewAction {
	if d.Action != "" {
		return d.Action
	}
	if d.Approve {
		return domain.ActionApprove
	}
	return domain.ActionReject
}

// Moderation — переходы «на проверке → опубликовано | отклонено | на доработке» (свои категории — «→ общая |
// отклонена»), правки опубликованного и снятие опубликованного по жалобе — с записью в журнал.
type Moderation struct{ repo ModerationRepository }

var _ Moderator = (*Moderation)(nil)

// NewModeration создаёт сервис модерации.
func NewModeration(repo ModerationRepository) *Moderation { return &Moderation{repo: repo} }

// checkDecision — причина отказа 5–500 символов; замечания — хотя бы одно, поля из списка, каждое 3–300.
func checkDecision(d *Decision, subject domain.Subject) error {
	switch d.action() {
	case domain.ActionApprove:
		d.Reason, d.Notes = "", nil
	case domain.ActionReject:
		d.Reason = squash(d.Reason)
		if n := utf8.RuneCountInString(d.Reason); n < 5 || n > 500 {
			return &ValidationError{Fields: map[string]string{"reason": "Причина отказа — от 5 до 500 символов"}}
		}
		d.Notes = nil
	case domain.ActionRework:
		kind := domain.KindTask
		if subject == domain.SubjectCard {
			kind = domain.KindCard
		}
		ve := &ValidationError{}
		clean := map[string]string{}
		for k, v := range d.Notes {
			v = squash(v)
			if v == "" {
				continue
			}
			if k != "general" && !contains(domain.ReviewFields[kind], k) {
				ve.add("notes", "Замечание к неизвестному полю")
				continue
			}
			if n := utf8.RuneCountInString(v); n < 3 || n > 300 {
				ve.add("notes."+k, "Замечание — от 3 до 300 символов")
				continue
			}
			clean[k] = v
		}
		if len(clean) == 0 && len(ve.Fields) == 0 {
			ve.add("notes", "Напишите, что поправить")
		}
		if err := ve.orNil(); err != nil {
			return err
		}
		d.Notes = clean
		d.Reason = notesSummary(clean)
	default:
		return &ValidationError{Fields: map[string]string{"action": "Решение — опубликовать, отклонить или на доработку"}}
	}
	return nil
}

// notesSummary — замечания одной строкой (в журнал и причину), поля по порядку.
func notesSummary(n map[string]string) string {
	keys := make([]string, 0, len(n))
	for k := range n {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, n[k])
	}
	s := strings.Join(parts, "; ")
	if utf8.RuneCountInString(s) > 500 {
		s = string([]rune(s)[:497]) + "…"
	}
	return s
}

func targetStatus(a domain.ReviewAction) domain.Status {
	switch a {
	case domain.ActionApprove:
		return domain.StatusPublished
	case domain.ActionRework:
		return domain.StatusChangesRequested
	}
	return domain.StatusRejected
}

// Decide — решение; задание и анкета — только из «на проверке» (отказ — ещё и из «опубликовано»: снять по
// жалобе), правка — только из «на проверке». Возвращает новый статус (правки — статус правки).
func (m *Moderation) Decide(ctx context.Context, d Decision) (domain.Status, error) {
	if d.Subject != domain.SubjectCategory {
		if err := checkDecision(&d, d.Subject); err != nil {
			return "", err
		}
	} else if !d.Approve && d.Action != domain.ActionApprove {
		d.Reason = squash(d.Reason)
		if n := utf8.RuneCountInString(d.Reason); n < 5 || n > 500 {
			return "", &ValidationError{Fields: map[string]string{"reason": "Причина отказа — от 5 до 500 символов"}}
		}
	}
	if d.ModeratorID == 0 {
		return "", fmt.Errorf("модерация: не указан модератор")
	}
	to := targetStatus(d.action())
	ch := domain.StatusChange{Subject: d.Subject, SubjectID: d.ID, To: to, Reason: d.Reason, ActorID: d.ModeratorID, Notes: d.Notes}
	switch d.Subject {
	case domain.SubjectTask:
		t, err := m.repo.Task(ctx, d.ID)
		if err != nil {
			return "", err
		}
		if d.Revision {
			return m.decideTaskRevision(ctx, t, ch)
		}
		ch.From = t.Status
		if !domain.CanTransition(ch.From, to, domain.ActorModerator) {
			return "", ErrWrongStatus
		}
		out, err := m.repo.SetTaskStatus(ctx, ch)
		return out.Status, conflictToStatus(err)
	case domain.SubjectCard:
		c, err := m.repo.Card(ctx, d.ID)
		if err != nil {
			return "", err
		}
		if d.Revision {
			return m.decideCardRevision(ctx, c, ch)
		}
		ch.From = c.Status
		if !domain.CanTransition(ch.From, to, domain.ActorModerator) {
			return "", ErrWrongStatus
		}
		out, err := m.repo.SetCardStatus(ctx, ch)
		return out.Status, conflictToStatus(err)
	case domain.SubjectCategory:
		// своя категория: одобренная становится общей; общая с таким названием уже есть — ErrWrongStatus
		c, err := m.repo.Category(ctx, d.ID)
		if err != nil {
			return "", err
		}
		if c.Status != domain.CategoryPending {
			return "", ErrWrongStatus
		}
		ch.From = domain.Status(c.Status)
		ch.Notes = nil
		if d.action() == domain.ActionApprove {
			ch.To = domain.CategoryApproved
		} else {
			ch.To = domain.CategoryRejected
		}
		out, err := m.repo.SetCategoryStatus(ctx, ch)
		return domain.Status(out.Status), conflictToStatus(err)
	}
	return "", fmt.Errorf("модерация: неизвестный предмет %q", d.Subject)
}

func (m *Moderation) decideTaskRevision(ctx context.Context, t domain.Task, ch domain.StatusChange) (domain.Status, error) {
	if t.Status != domain.StatusPublished || t.Revision == nil || !domain.RevisionTransition(t.RevisionStatus, ch.To, domain.ActorModerator) {
		return "", ErrWrongStatus
	}
	ch.From = t.RevisionStatus
	if ch.To == domain.StatusPublished {
		_, err := m.repo.ApplyTaskRevision(ctx, t.ID, t.RevisionStatus, ch)
		return domain.StatusPublished, conflictToStatus(err)
	}
	out, err := m.repo.SetTaskRevision(ctx, ch)
	return out.RevisionStatus, conflictToStatus(err)
}

func (m *Moderation) decideCardRevision(ctx context.Context, c domain.CreatorCard, ch domain.StatusChange) (domain.Status, error) {
	if c.Status != domain.StatusPublished || c.Revision == nil || !domain.RevisionTransition(c.RevisionStatus, ch.To, domain.ActorModerator) {
		return "", ErrWrongStatus
	}
	ch.From = c.RevisionStatus
	if ch.To == domain.StatusPublished {
		_, err := m.repo.ApplyCardRevision(ctx, c.UserID, c.RevisionStatus, ch)
		return domain.StatusPublished, conflictToStatus(err)
	}
	out, err := m.repo.SetCardRevision(ctx, ch)
	return out.RevisionStatus, conflictToStatus(err)
}

// History — журнал смен статуса по порядку.
func (m *Moderation) History(ctx context.Context, subject domain.Subject, id int64) ([]domain.StatusChange, error) {
	return m.repo.History(ctx, subject, id)
}

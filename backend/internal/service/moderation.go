package service

import (
	"context"
	"fmt"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// Moderator — решение модератора по заданию или анкете и журнал смен статуса. Контракт для будущей админки:
// её транспорт будет зависеть от этого интерфейса, а не от реализации.
type Moderator interface {
	Decide(ctx context.Context, d Decision) (domain.Status, error)
	History(ctx context.Context, subject domain.Subject, id int64) ([]domain.StatusChange, error)
}

// ModerationRepository — что модерации нужно от хранилища.
type ModerationRepository interface {
	Task(ctx context.Context, id int64) (domain.Task, error)
	SetTaskStatus(ctx context.Context, ch domain.StatusChange) (domain.Task, error)
	Card(ctx context.Context, userID int64) (domain.CreatorCard, error)
	SetCardStatus(ctx context.Context, ch domain.StatusChange) (domain.CreatorCard, error)
	History(ctx context.Context, subject domain.Subject, id int64) ([]domain.StatusChange, error)
	// Category / SetCategoryStatus — своя категория: «на проверке» → общая или отклонённая.
	Category(ctx context.Context, id int64) (domain.CategoryInfo, error)
	SetCategoryStatus(ctx context.Context, ch domain.StatusChange) (domain.CategoryInfo, error)
}

// Decision — решение: опубликовать или отклонить с причиной (её увидит владелец).
type Decision struct {
	Subject     domain.Subject
	ID          int64 // tasks.id, user_id анкеты или categories.id
	ModeratorID int64
	Approve     bool
	Reason      string
}

// Moderation — переходы «на проверке → опубликовано | отклонено» (свои категории — «→ общая | отклонена»)
// с записью в журнал.
type Moderation struct{ repo ModerationRepository }

var _ Moderator = (*Moderation)(nil)

// NewModeration создаёт сервис модерации.
func NewModeration(repo ModerationRepository) *Moderation { return &Moderation{repo: repo} }

// Decide — опубликовать или отклонить; только из «на проверке». Возвращает новый статус.
func (m *Moderation) Decide(ctx context.Context, d Decision) (domain.Status, error) {
	to := domain.StatusPublished
	reason := ""
	if !d.Approve {
		to = domain.StatusRejected
		reason = squash(d.Reason)
		if n := utf8.RuneCountInString(reason); n < 5 || n > 500 {
			return "", &ValidationError{Fields: map[string]string{"reason": "Причина отказа — от 5 до 500 символов"}}
		}
	}
	if d.ModeratorID == 0 {
		return "", fmt.Errorf("модерация: не указан модератор")
	}
	ch := domain.StatusChange{Subject: d.Subject, SubjectID: d.ID, To: to, Reason: reason, ActorID: d.ModeratorID}
	switch d.Subject {
	case domain.SubjectTask:
		t, err := m.repo.Task(ctx, d.ID)
		if err != nil {
			return "", err
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
		if d.Approve {
			ch.To = domain.CategoryApproved
		}
		out, err := m.repo.SetCategoryStatus(ctx, ch)
		return domain.Status(out.Status), conflictToStatus(err)
	}
	return "", fmt.Errorf("модерация: неизвестный предмет %q", d.Subject)
}

// History — журнал смен статуса по порядку.
func (m *Moderation) History(ctx context.Context, subject domain.Subject, id int64) ([]domain.StatusChange, error) {
	return m.repo.History(ctx, subject, id)
}

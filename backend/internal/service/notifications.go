package service

import (
	"context"

	"ohvat/backend/internal/domain"
)

// ===== уведомления в приложении: свой ящик у пользователя =====

// NoticesRepository — ящик уведомлений пользователя.
type NoticesRepository interface {
	List(ctx context.Context, userID int64, limit int) ([]domain.Notice, error)
	Unread(ctx context.Context, userID int64) (int64, error)
	MarkRead(ctx context.Context, id, userID int64) error
	MarkAllRead(ctx context.Context, userID int64) error
}

// NoticesBox — уведомления текущего пользователя (только свои; доступ — любой вошедший).
type NoticesBox struct{ repo NoticesRepository }

// NewNotices создаёт сервис уведомлений пользователя.
func NewNotices(repo NoticesRepository) *NoticesBox { return &NoticesBox{repo: repo} }

// List — последние уведомления и счётчик непрочитанных; limit — по умолчанию 50.
func (s *NoticesBox) List(ctx context.Context, u domain.User, limit int) ([]domain.Notice, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	items, err := s.repo.List(ctx, u.ID, limit)
	if err != nil {
		return nil, 0, err
	}
	unread, err := s.repo.Unread(ctx, u.ID)
	if err != nil {
		return nil, 0, err
	}
	return items, unread, nil
}

// MarkRead — одно прочитанным; чужое или уже прочитанное — без ошибки.
func (s *NoticesBox) MarkRead(ctx context.Context, u domain.User, id int64) error {
	return s.repo.MarkRead(ctx, id, u.ID)
}

// MarkAllRead — все прочитанными.
func (s *NoticesBox) MarkAllRead(ctx context.Context, u domain.User) error {
	return s.repo.MarkAllRead(ctx, u.ID)
}

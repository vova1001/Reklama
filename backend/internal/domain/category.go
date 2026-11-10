package domain

import "time"

// Статусы категорий: общая (в справочнике), своя на проверке, отклонённая.
const (
	CategoryApproved = "approved"
	CategoryPending  = "pending_review"
	CategoryRejected = "rejected"
)

// CategoryInfo — категория справочника или своя категория пользователя.
type CategoryInfo struct {
	ID        int64
	Slug      string
	Title     string
	Icon      string
	Status    string // CategoryApproved | CategoryPending | CategoryRejected
	AuthorID  int64  // 0 — общая из справочника
	CreatedAt time.Time
}

// Own — своя категория пользователя (ещё не общая).
func (c CategoryInfo) Own() bool { return c.Status != CategoryApproved }

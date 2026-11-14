package repository

import (
	"context"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// Notices — уведомления пользователей в приложении. SQL — только в queries/notifications.sql.
// Запись уведомления о решении модератора — в транзакции итога решения (см. insertNotification и Admin.FinishDecision):
// уведомление существует ровно тогда, когда решение применено.
type Notices struct{ q *store.Queries }

// NewNotices создаёт репозиторий уведомлений.
func NewNotices(db store.DBTX) *Notices { return &Notices{q: store.New(db)} }

// insertNotification — уведомление владельцу о применённом решении; вызывается внутри транзакции.
func insertNotification(ctx context.Context, q *store.Queries, userID, decisionID int64, n domain.Notice) error {
	return q.InsertNotification(ctx, store.InsertNotificationParams{
		UserID: userID, DecisionID: decisionID, Kind: string(n.Kind), ItemID: n.ItemID, Action: string(n.Action),
		Title: n.Title, Reason: n.Reason, Path: n.Path,
	})
}

// List — последние уведомления пользователя, новые сверху.
func (r *Notices) List(ctx context.Context, userID int64, limit int) ([]domain.Notice, error) {
	rows, err := r.q.Notifications(ctx, store.NotificationsParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, dbErr("уведомления", err)
	}
	out := make([]domain.Notice, 0, len(rows))
	for _, x := range rows {
		out = append(out, domain.Notice{
			ID: x.ID, Kind: domain.ReviewKind(x.Kind), ItemID: x.ItemID, Action: domain.ReviewAction(x.Action),
			Title: x.Title, Reason: x.Reason, Path: x.Path, ReadAt: tsPtr(x.ReadAt), CreatedAt: x.CreatedAt.Time,
		})
	}
	return out, nil
}

// Unread — сколько непрочитанных у пользователя.
func (r *Notices) Unread(ctx context.Context, userID int64) (int64, error) {
	n, err := r.q.UnreadNotifications(ctx, userID)
	return n, dbErr("счётчик уведомлений", err)
}

// MarkRead — пометить одно прочитанным; чужое или уже прочитанное — без ошибки (идемпотентно).
func (r *Notices) MarkRead(ctx context.Context, id, userID int64) error {
	return dbErr("прочтение уведомления", r.q.ReadNotification(ctx, store.ReadNotificationParams{ID: id, UserID: userID}))
}

// MarkAllRead — пометить все прочитанными.
func (r *Notices) MarkAllRead(ctx context.Context, userID int64) error {
	return dbErr("прочтение уведомлений", r.q.ReadAllNotifications(ctx, userID))
}

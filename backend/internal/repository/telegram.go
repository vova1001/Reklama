package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// Telegram — вход через бота, чаты бота, устройства пользователей. SQL — только в queries/telegram.sql.
type Telegram struct{ q *store.Queries }

// NewTelegram создаёт репозиторий бота.
func NewTelegram(db store.DBTX) *Telegram { return &Telegram{q: store.New(db)} }

// ===== вход через бота =====

// CreateTelegramLogin — записать новый вход (хеши токена и секрета браузера).
func (r *Telegram) CreateTelegramLogin(ctx context.Context, l domain.NewTelegramLogin) error {
	err := r.q.CreateTelegramLogin(ctx, store.CreateTelegramLoginParams{
		TokenHash: l.TokenHash, BrowserHash: l.BrowserHash, Ip: l.IP,
		CreatedAt: ts(l.CreatedAt), ExpiresAt: ts(l.ExpiresAt),
	})
	return dbErr("новый вход через бота", err)
}

// TelegramLoginStats — сколько входов через бота начато с адреса после since и когда первый.
func (r *Telegram) TelegramLoginStats(ctx context.Context, ip string, since time.Time) (int, time.Time, error) {
	s, err := r.q.CountTelegramLoginsByIP(ctx, store.CountTelegramLoginsByIPParams{Ip: ip, CreatedAt: ts(since)})
	return int(s.Started), s.FirstAt.Time, dbErr("лимит входов через бота", err)
}

// PendingTelegramLogin — ожидающий неистёкший вход по хешу токена.
func (r *Telegram) PendingTelegramLogin(ctx context.Context, tokenHash []byte, now time.Time) (domain.TelegramLogin, error) {
	l, err := r.q.GetPendingTelegramLogin(ctx, store.GetPendingTelegramLoginParams{TokenHash: tokenHash, ExpiresAt: ts(now)})
	return tgLoginOf(l), dbErr("вход через бота", err)
}

// ConfirmTelegramLogin — подтвердить вход человеком из Telegram; срок продлевается до graceUntil (если он позже).
// Нет ожидающего входа или его уже подтвердил другой человек — ErrNotFound.
func (r *Telegram) ConfirmTelegramLogin(ctx context.Context, tokenHash []byte, p domain.TelegramProfile, now, graceUntil time.Time) (domain.TelegramLogin, error) {
	l, err := r.q.ConfirmTelegramLogin(ctx, store.ConfirmTelegramLoginParams{
		TokenHash: tokenHash, ConfirmedAt: ts(now), TgUserID: pgtype.Int8{Int64: p.ID, Valid: true},
		TgName: p.Name(), TgUsername: p.Username, ExpiresAt: ts(graceUntil),
	})
	return tgLoginOf(l), dbErr("подтверждение входа через бота", err)
}

// CancelTelegramLogin — отменить неподтверждённый вход («Это не я»).
func (r *Telegram) CancelTelegramLogin(ctx context.Context, tokenHash []byte, now time.Time) (domain.TelegramLogin, error) {
	l, err := r.q.CancelTelegramLogin(ctx, store.CancelTelegramLoginParams{TokenHash: tokenHash, ConsumedAt: ts(now)})
	return tgLoginOf(l), dbErr("отмена входа через бота", err)
}

// TelegramLoginByBrowser — вход по хешу секрета браузера (в любом состоянии).
func (r *Telegram) TelegramLoginByBrowser(ctx context.Context, browserHash []byte) (domain.TelegramLogin, error) {
	l, err := r.q.GetTelegramLoginByBrowser(ctx, browserHash)
	return tgLoginOf(l), dbErr("вход через бота по браузеру", err)
}

// ConsumeTelegramLogin — забрать подтверждённый вход (атомарно, один раз).
func (r *Telegram) ConsumeTelegramLogin(ctx context.Context, browserHash []byte, now time.Time) (domain.TelegramLogin, error) {
	l, err := r.q.ConsumeTelegramLogin(ctx, store.ConsumeTelegramLoginParams{BrowserHash: browserHash, ConsumedAt: ts(now)})
	return tgLoginOf(l), dbErr("вход через бота: забрать", err)
}

// DeleteTelegramLoginsBefore — удалить старые строки входов.
func (r *Telegram) DeleteTelegramLoginsBefore(ctx context.Context, t time.Time) (int64, error) {
	n, err := r.q.DeleteOldTelegramLogins(ctx, ts(t))
	return n, dbErr("очистка входов через бота", err)
}

// ===== чаты бота =====

// SaveChat — запомнить личный чат (снимает отметку «бот остановлен»).
func (r *Telegram) SaveChat(ctx context.Context, tgUserID, chatID int64, username string) (domain.TelegramChat, error) {
	c, err := r.q.UpsertTelegramChat(ctx, store.UpsertTelegramChatParams{TgUserID: tgUserID, ChatID: chatID, Username: username})
	return chatOf(c), dbErr("чат бота", err)
}

// SetNotify — включить или выключить уведомления; чата нет — ErrNotFound.
func (r *Telegram) SetNotify(ctx context.Context, tgUserID int64, on bool) error {
	n, err := r.q.SetTelegramChatNotify(ctx, store.SetTelegramChatNotifyParams{TgUserID: tgUserID, Notify: on})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return dbErr("уведомления бота", err)
}

// BlockChat — пользователь остановил бота: больше не пишем.
func (r *Telegram) BlockChat(ctx context.Context, chatID int64) error {
	_, err := r.q.BlockTelegramChat(ctx, chatID)
	return dbErr("бот остановлен", err)
}

// Chat — чат по id пользователя Telegram.
func (r *Telegram) Chat(ctx context.Context, tgUserID int64) (domain.TelegramChat, error) {
	c, err := r.q.GetTelegramChat(ctx, tgUserID)
	return chatOf(c), dbErr("чат бота", err)
}

// NotifyChat — куда писать уведомления пользователю сайта; некуда — ErrNotFound.
func (r *Telegram) NotifyChat(ctx context.Context, userID int64) (domain.TelegramChat, error) {
	c, err := r.q.GetUserNotifyChat(ctx, userID)
	return chatOf(c), dbErr("чат уведомлений", err)
}

// ===== устройства =====

// TouchDevice — отметить устройство пользователя; true — увидели впервые.
func (r *Telegram) TouchDevice(ctx context.Context, userID int64, deviceHash []byte) (bool, error) {
	n, err := r.q.TouchUserDevice(ctx, store.TouchUserDeviceParams{UserID: userID, DeviceHash: deviceHash})
	return n, dbErr("устройство", err)
}

func tgLoginOf(l store.TelegramLogin) domain.TelegramLogin {
	out := domain.TelegramLogin{
		ID: l.ID, BrowserHash: l.BrowserHash, TgUserID: l.TgUserID.Int64, TgName: l.TgName,
		TgUsername: l.TgUsername, CreatedAt: l.CreatedAt.Time, ExpiresAt: l.ExpiresAt.Time, Cancelled: l.Cancelled,
	}
	if l.ConfirmedAt.Valid {
		t := l.ConfirmedAt.Time
		out.ConfirmedAt = &t
	}
	if l.ConsumedAt.Valid {
		t := l.ConsumedAt.Time
		out.ConsumedAt = &t
	}
	return out
}

func chatOf(c store.TelegramChat) domain.TelegramChat {
	return domain.TelegramChat{TgUserID: c.TgUserID, ChatID: c.ChatID, Username: c.Username, Notify: c.Notify, Blocked: c.BlockedAt.Valid}
}

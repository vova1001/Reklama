package repository

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// Social — подтверждённые соцсети, доступы OAuth (шифротекст), начатые подключения, каналы Telegram.
// SQL — только в queries/social.sql.
type Social struct {
	db DB
	q  *store.Queries
}

// NewSocial создаёт репозиторий соцсетей.
func NewSocial(db DB) *Social { return &Social{db: db, q: store.New(db)} }

func (r *Social) inTx(ctx context.Context, fn func(q *store.Queries) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(r.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ===== аккаунты =====

// Accounts — все аккаунты пользователя по площадкам.
func (r *Social) Accounts(ctx context.Context, userID int64) ([]domain.SocialAccount, error) {
	rows, err := r.q.ListSocialAccounts(ctx, userID)
	if err != nil {
		return nil, dbErr("соцсети пользователя", err)
	}
	return accountsOf(rows), nil
}

// VerifiedAccounts — подтверждённые аккаунты авторов (для анкет).
func (r *Social) VerifiedAccounts(ctx context.Context, userIDs []int64) ([]domain.SocialAccount, error) {
	rows, err := r.q.ListVerifiedSocialAccounts(ctx, userIDs)
	if err != nil {
		return nil, dbErr("подтверждённые соцсети", err)
	}
	return accountsOf(rows), nil
}

// Account — аккаунт пользователя; чужой или нет — ErrNotFound.
func (r *Social) Account(ctx context.Context, userID, id int64) (domain.SocialAccount, error) {
	row, err := r.q.GetSocialAccount(ctx, store.GetSocialAccountParams{ID: id, UserID: userID})
	return accountOf(row), dbErr("соцсеть", err)
}

// CountAccounts — сколько аккаунтов у пользователя.
func (r *Social) CountAccounts(ctx context.Context, userID int64) (int, error) {
	n, err := r.q.CountSocialAccounts(ctx, userID)
	return int(n), dbErr("число соцсетей", err)
}

// SaveAccount — создать или обновить аккаунт и записать цифры в историю (если подтверждён) — одной транзакцией.
func (r *Social) SaveAccount(ctx context.Context, a domain.SocialAccount, at time.Time) (domain.SocialAccount, error) {
	var row store.SocialAccount
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		row, err = q.UpsertSocialAccount(ctx, store.UpsertSocialAccountParams{
			UserID: a.UserID, Platform: string(a.Platform), ExternalID: a.ExternalID, Kind: a.Kind, Handle: a.Handle,
			Title: a.Title, Url: a.URL, Status: string(a.Status), Method: string(a.Method), CredentialID: optInt(a.CredentialID),
			Followers: i8(a.Stats.Followers), FollowersHidden: a.Stats.FollowersHidden, Views: i8(a.Stats.Views),
			Videos: i8(a.Stats.Videos), Likes: i8(a.Stats.Likes), FailReason: a.FailReason,
			VerifiedAt: tsOpt(a.VerifiedAt), RefreshedAt: tsOpt(a.RefreshedAt), NextRefreshAt: tsOpt(a.NextRefreshAt),
		})
		if err != nil || a.Status != domain.SocialVerified {
			return err
		}
		return q.InsertSocialStats(ctx, statsParams(row.ID, at, a.Stats))
	})
	if err != nil {
		return domain.SocialAccount{}, dbErr("сохранение соцсети", err)
	}
	return accountOf(row), nil
}

// SaveStats — свежие цифры (статус verified) и запись в историю — одной транзакцией.
func (r *Social) SaveStats(ctx context.Context, upd domain.SocialAccount, at, next time.Time) (domain.SocialAccount, error) {
	var row store.SocialAccount
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		row, err = q.SetSocialStats(ctx, store.SetSocialStatsParams{
			ID: upd.ID, Handle: upd.Handle, Title: upd.Title, Url: upd.URL, Followers: i8(upd.Stats.Followers),
			FollowersHidden: upd.Stats.FollowersHidden, Views: i8(upd.Stats.Views), Videos: i8(upd.Stats.Videos),
			Likes: i8(upd.Stats.Likes), RefreshedAt: ts(at), NextRefreshAt: ts(next),
		})
		if err != nil {
			return err
		}
		return q.InsertSocialStats(ctx, statsParams(row.ID, at, upd.Stats))
	})
	if err != nil {
		return domain.SocialAccount{}, dbErr("цифры соцсети", err)
	}
	return accountOf(row), nil
}

// SetRetry — временный сбой: следующая попытка в next.
func (r *Social) SetRetry(ctx context.Context, id int64, next time.Time) error {
	return dbErr("повтор обновления соцсети", r.q.SetSocialRetry(ctx, store.SetSocialRetryParams{ID: id, NextRefreshAt: ts(next)}))
}

// SetFailed — доступ потерян.
func (r *Social) SetFailed(ctx context.Context, id int64, reason string) error {
	return dbErr("соцсеть: доступ потерян", r.q.SetSocialFailed(ctx, store.SetSocialFailedParams{ID: id, FailReason: reason}))
}

// ClaimDue — аккаунты, которым пора обновиться (аренда до leaseUntil; другой сервер их не возьмёт).
func (r *Social) ClaimDue(ctx context.Context, now, leaseUntil time.Time, limit int) ([]domain.SocialAccount, error) {
	rows, err := r.q.ClaimDueSocialAccounts(ctx, store.ClaimDueSocialAccountsParams{Now: ts(now), LeaseUntil: ts(leaseUntil), Lim: int32(limit)})
	if err != nil {
		return nil, dbErr("соцсети на обновление", err)
	}
	return accountsOf(rows), nil
}

// DeleteAccount — отвязать аккаунт; чужой или нет — ErrNotFound.
func (r *Social) DeleteAccount(ctx context.Context, userID, id int64) (domain.SocialAccount, error) {
	row, err := r.q.DeleteSocialAccount(ctx, store.DeleteSocialAccountParams{ID: id, UserID: userID})
	return accountOf(row), dbErr("отвязка соцсети", err)
}

// AccountsByExternal — аккаунты всех пользователей по каналу площадки.
func (r *Social) AccountsByExternal(ctx context.Context, p domain.SocialPlatform, externalID string) ([]domain.SocialAccount, error) {
	rows, err := r.q.ListSocialAccountsByExternal(ctx, store.ListSocialAccountsByExternalParams{Platform: string(p), ExternalID: externalID})
	if err != nil {
		return nil, dbErr("соцсети по каналу", err)
	}
	return accountsOf(rows), nil
}

// History — последние записи истории цифр.
func (r *Social) History(ctx context.Context, accountID int64, limit int) ([]domain.SocialStats, []time.Time, error) {
	rows, err := r.q.ListSocialStats(ctx, store.ListSocialStatsParams{AccountID: accountID, Limit: int32(limit)})
	if err != nil {
		return nil, nil, dbErr("история цифр", err)
	}
	st := make([]domain.SocialStats, 0, len(rows))
	at := make([]time.Time, 0, len(rows))
	for _, x := range rows {
		st = append(st, domain.SocialStats{Followers: p8(x.Followers), Views: p8(x.Views), Videos: p8(x.Videos), Likes: p8(x.Likes)})
		at = append(at, x.At.Time)
	}
	return st, at, nil
}

// ===== доступы =====

// SaveCredential — новый доступ или замена токенов того же человека на площадке.
func (r *Social) SaveCredential(ctx context.Context, c domain.SocialCredential) (domain.SocialCredential, error) {
	row, err := r.q.UpsertSocialCredential(ctx, store.UpsertSocialCredentialParams{
		UserID: c.UserID, Platform: string(c.Platform), Subject: c.Subject, Secret: c.Secret,
		AccessExpiresAt: tsOpt(c.AccessExpiresAt), RefreshExpiresAt: tsOpt(c.RefreshExpiresAt),
	})
	return credOf(row), dbErr("сохранение доступа", err)
}

// Credential — доступ по id.
func (r *Social) Credential(ctx context.Context, id int64) (domain.SocialCredential, error) {
	row, err := r.q.GetSocialCredential(ctx, id)
	return credOf(row), dbErr("доступ", err)
}

// LockCredential — аренда доступа; занят — ErrConflict, нет — ErrNotFound.
func (r *Social) LockCredential(ctx context.Context, id int64, now, until time.Time) (domain.SocialCredential, error) {
	row, err := r.q.LockSocialCredential(ctx, store.LockSocialCredentialParams{ID: id, Now: ts(now), LockUntil: ts(until)})
	if err == nil {
		return credOf(row), nil
	}
	if err := dbErr("аренда доступа", err); !errors.Is(err, domain.ErrNotFound) {
		return domain.SocialCredential{}, err
	}
	if _, err := r.q.GetSocialCredential(ctx, id); err != nil {
		return domain.SocialCredential{}, dbErr("доступ", err)
	}
	return domain.SocialCredential{}, domain.ErrConflict
}

// UpdateCredential — новые токены и снятие аренды.
func (r *Social) UpdateCredential(ctx context.Context, c domain.SocialCredential) error {
	return dbErr("обновление доступа", r.q.SaveSocialCredential(ctx, store.SaveSocialCredentialParams{
		ID: c.ID, Secret: c.Secret, AccessExpiresAt: tsOpt(c.AccessExpiresAt), RefreshExpiresAt: tsOpt(c.RefreshExpiresAt),
	}))
}

// UnlockCredential — снять аренду.
func (r *Social) UnlockCredential(ctx context.Context, id int64) error {
	return dbErr("снятие аренды доступа", r.q.UnlockSocialCredential(ctx, id))
}

// CredentialAccounts — сколько аккаунтов пользуются доступом.
func (r *Social) CredentialAccounts(ctx context.Context, id int64) (int, error) {
	n, err := r.q.CountCredentialAccounts(ctx, pgtype.Int8{Int64: id, Valid: true})
	return int(n), dbErr("аккаунты доступа", err)
}

// DeleteCredential — удалить доступ (токены).
func (r *Social) DeleteCredential(ctx context.Context, id int64) error {
	return dbErr("удаление доступа", r.q.DeleteSocialCredential(ctx, id))
}

// ===== начатые подключения =====

// CreateState — записать начатое подключение (только хеш state).
func (r *Social) CreateState(ctx context.Context, hash []byte, s domain.SocialState) error {
	return dbErr("начало подключения", r.q.CreateSocialState(ctx, store.CreateSocialStateParams{
		StateHash: hash, UserID: s.UserID, Platform: string(s.Platform), Verifier: s.Verifier,
		CreatedAt: ts(s.CreatedAt), ExpiresAt: ts(s.ExpiresAt),
	}))
}

// StateStats — сколько подключений пользователь начал после since и когда первое.
func (r *Social) StateStats(ctx context.Context, userID int64, since time.Time) (int, time.Time, error) {
	row, err := r.q.CountSocialStates(ctx, store.CountSocialStatesParams{UserID: userID, CreatedAt: ts(since)})
	return int(row.Started), row.FirstAt.Time, dbErr("лимит подключений", err)
}

// PeekState — неистёкшее подключение без погашения.
func (r *Social) PeekState(ctx context.Context, hash []byte, p domain.SocialPlatform, now time.Time) (domain.SocialState, error) {
	row, err := r.q.GetSocialState(ctx, store.GetSocialStateParams{StateHash: hash, Platform: string(p), ExpiresAt: ts(now)})
	return stateOf(row), dbErr("подключение", err)
}

// TakeState — погасить неистёкшее подключение (один раз).
func (r *Social) TakeState(ctx context.Context, hash []byte, p domain.SocialPlatform, now time.Time) (domain.SocialState, error) {
	row, err := r.q.TakeSocialState(ctx, store.TakeSocialStateParams{StateHash: hash, Platform: string(p), ExpiresAt: ts(now)})
	return stateOf(row), dbErr("подключение", err)
}

// DeleteStatesBefore — удалить истёкшие подключения.
func (r *Social) DeleteStatesBefore(ctx context.Context, t time.Time) (int64, error) {
	n, err := r.q.DeleteOldSocialStates(ctx, ts(t))
	return n, dbErr("очистка подключений", err)
}

// ===== Telegram =====

// SaveTelegramChannel — канал и статус бота в нём.
func (r *Social) SaveTelegramChannel(ctx context.Context, ch domain.TelegramChannel) error {
	return dbErr("канал Telegram", r.q.UpsertTelegramChannel(ctx, store.UpsertTelegramChannelParams{
		ChatID: ch.ChatID, Title: ch.Title, Username: ch.Username, ChatType: ch.Type, AddedBy: ch.AddedBy, BotStatus: ch.BotStatus,
	}))
}

// TelegramChannelsAddedBy — каналы, куда этот человек добавил бота админом.
func (r *Social) TelegramChannelsAddedBy(ctx context.Context, tgUserID int64) ([]domain.TelegramChannel, error) {
	rows, err := r.q.ListTelegramChannelsAddedBy(ctx, tgUserID)
	if err != nil {
		return nil, dbErr("каналы Telegram", err)
	}
	out := make([]domain.TelegramChannel, 0, len(rows))
	for _, c := range rows {
		out = append(out, domain.TelegramChannel{ChatID: c.ChatID, Title: c.Title, Username: c.Username, Type: c.ChatType, AddedBy: c.AddedBy, BotStatus: c.BotStatus})
	}
	return out, nil
}

// UserTelegramID — Telegram пользователя; нет — ErrNotFound.
func (r *Social) UserTelegramID(ctx context.Context, userID int64) (int64, error) {
	s, err := r.q.GetUserTelegramID(ctx, userID)
	if err != nil {
		return 0, dbErr("Telegram пользователя", err)
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, domain.ErrNotFound
	}
	return id, nil
}

// ===== преобразования =====

func accountsOf(rows []store.SocialAccount) []domain.SocialAccount {
	out := make([]domain.SocialAccount, 0, len(rows))
	for _, r := range rows {
		out = append(out, accountOf(r))
	}
	return out
}

func accountOf(r store.SocialAccount) domain.SocialAccount {
	return domain.SocialAccount{
		ID: r.ID, UserID: r.UserID, Platform: domain.SocialPlatform(r.Platform), ExternalID: r.ExternalID, Kind: r.Kind,
		Handle: r.Handle, Title: r.Title, URL: r.Url, Status: domain.SocialStatus(r.Status), Method: domain.SocialMethod(r.Method),
		CredentialID: r.CredentialID.Int64,
		Stats: domain.SocialStats{Followers: p8(r.Followers), FollowersHidden: r.FollowersHidden, Views: p8(r.Views),
			Videos: p8(r.Videos), Likes: p8(r.Likes)},
		FailReason: r.FailReason, Failures: int(r.Failures), VerifiedAt: tsPtr(r.VerifiedAt), RefreshedAt: tsPtr(r.RefreshedAt),
		NextRefreshAt: tsPtr(r.NextRefreshAt), CreatedAt: r.CreatedAt.Time,
	}
}

func credOf(r store.SocialCredential) domain.SocialCredential {
	return domain.SocialCredential{ID: r.ID, UserID: r.UserID, Platform: domain.SocialPlatform(r.Platform), Subject: r.Subject,
		Secret: r.Secret, AccessExpiresAt: tsPtr(r.AccessExpiresAt), RefreshExpiresAt: tsPtr(r.RefreshExpiresAt)}
}

func stateOf(r store.SocialOauthState) domain.SocialState {
	return domain.SocialState{UserID: r.UserID, Platform: domain.SocialPlatform(r.Platform), Verifier: r.Verifier,
		CreatedAt: r.CreatedAt.Time, ExpiresAt: r.ExpiresAt.Time}
}

func statsParams(id int64, at time.Time, s domain.SocialStats) store.InsertSocialStatsParams {
	return store.InsertSocialStatsParams{AccountID: id, At: ts(at), Followers: i8(s.Followers), Views: i8(s.Views), Videos: i8(s.Videos), Likes: i8(s.Likes)}
}

func i8(p *int64) pgtype.Int8 {
	if p == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *p, Valid: true}
}

func p8(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func optInt(n int64) pgtype.Int8 { return pgtype.Int8{Int64: n, Valid: n != 0} }

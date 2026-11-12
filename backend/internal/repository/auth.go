package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// DB — подключение с транзакциями (pgxpool.Pool).
type DB interface {
	store.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Auth — пользователи, способы входа, коды из письма и сессии в PostgreSQL. SQL — только в queries/auth.sql.
type Auth struct {
	db DB
	q  *store.Queries
}

// NewAuth создаёт репозиторий входа.
func NewAuth(db DB) *Auth { return &Auth{db: db, q: store.New(db)} }

// inTx выполняет fn в транзакции.
func (r *Auth) inTx(ctx context.Context, fn func(q *store.Queries) error) error {
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

// ===== пользователи =====

// UserByID — пользователь по id.
func (r *Auth) UserByID(ctx context.Context, id int64) (domain.User, error) {
	u, err := r.q.GetUser(ctx, id)
	return userOf(u), dbErr("пользователь", err)
}

// UserByIdentity — пользователь по способу входа.
func (r *Auth) UserByIdentity(ctx context.Context, provider, subject string) (domain.User, error) {
	u, err := r.q.GetUserByIdentity(ctx, store.GetUserByIdentityParams{Provider: provider, Subject: subject})
	return userOf(u), dbErr("пользователь по способу входа", err)
}

// UserByEmail — пользователь по подтверждённой почте.
func (r *Auth) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	u, err := r.q.GetUserByEmail(ctx, text(email))
	return userOf(u), dbErr("пользователь по почте", err)
}

// CreateUserWithIdentity — новый пользователь и его первый способ входа.
func (r *Auth) CreateUserWithIdentity(ctx context.Context, nu domain.NewUser, id domain.Identity) (domain.User, error) {
	var out store.User
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		out, err = q.CreateUser(ctx, store.CreateUserParams{
			Name: nu.Name, Email: text(nu.Email), AvatarUrl: nu.AvatarURL,
		})
		if err != nil {
			return err
		}
		return q.CreateIdentity(ctx, store.CreateIdentityParams{
			UserID: out.ID, Provider: id.Provider, Subject: id.Subject, Email: text(id.Email),
		})
	})
	return userOf(out), dbErr("новый пользователь", err)
}

// LinkIdentity — привязать способ входа (и почту, если у пользователя её нет).
func (r *Auth) LinkIdentity(ctx context.Context, userID int64, id domain.Identity) error {
	err := r.inTx(ctx, func(q *store.Queries) error {
		if err := q.CreateIdentity(ctx, store.CreateIdentityParams{
			UserID: userID, Provider: id.Provider, Subject: id.Subject, Email: text(id.Email),
		}); err != nil {
			return err
		}
		if id.Email == "" {
			return nil
		}
		return q.SetUserEmailIfEmpty(ctx, store.SetUserEmailIfEmptyParams{ID: userID, Email: text(id.Email)})
	})
	return dbErr("привязка способа входа", err)
}

// TouchLogin — отметить вход: время входа пользователя и способа.
func (r *Auth) TouchLogin(ctx context.Context, userID int64, id domain.Identity) error {
	err := r.inTx(ctx, func(q *store.Queries) error {
		if err := q.TouchUserLogin(ctx, userID); err != nil {
			return err
		}
		return q.TouchIdentity(ctx, store.TouchIdentityParams{Provider: id.Provider, Subject: id.Subject})
	})
	return dbErr("отметка входа", err)
}

// Providers — способы входа пользователя.
func (r *Auth) Providers(ctx context.Context, userID int64) ([]string, error) {
	p, err := r.q.ListUserProviders(ctx, userID)
	return p, dbErr("способы входа", err)
}

// ===== коды =====

// CodeStatsByEmail — письма на почту с момента since.
func (r *Auth) CodeStatsByEmail(ctx context.Context, email string, since time.Time) (domain.CodeStats, error) {
	s, err := r.q.CodeStatsByEmail(ctx, store.CodeStatsByEmailParams{Email: email, CreatedAt: ts(since)})
	return domain.CodeStats{Sent: int(s.Sent), FirstAt: s.FirstAt.Time, LastAt: s.LastAt.Time}, dbErr("лимит почты", err)
}

// CodeStatsByIP — письма с адреса с момента since.
func (r *Auth) CodeStatsByIP(ctx context.Context, ip string, since time.Time) (domain.CodeStats, error) {
	s, err := r.q.CodeStatsByIP(ctx, store.CodeStatsByIPParams{Ip: ip, CreatedAt: ts(since)})
	return domain.CodeStats{Sent: int(s.Sent), FirstAt: s.FirstAt.Time}, dbErr("лимит адреса", err)
}

// CreateCode — гасит прежние коды почты и записывает новый.
func (r *Auth) CreateCode(ctx context.Context, c domain.NewLoginCode) error {
	err := r.inTx(ctx, func(q *store.Queries) error {
		if err := q.ConsumeActiveCodes(ctx, store.ConsumeActiveCodesParams{Email: c.Email, ConsumedAt: ts(c.CreatedAt)}); err != nil {
			return err
		}
		return q.CreateLoginCode(ctx, store.CreateLoginCodeParams{
			Email: c.Email, CodeHash: c.CodeHash, Ip: c.IP, CreatedAt: ts(c.CreatedAt), ExpiresAt: ts(c.ExpiresAt),
		})
	})
	return dbErr("новый код", err)
}

// ActiveCode — последний непогашенный и неистёкший код почты.
func (r *Auth) ActiveCode(ctx context.Context, email string, now time.Time) (domain.LoginCode, error) {
	c, err := r.q.GetActiveCode(ctx, store.GetActiveCodeParams{Email: email, ExpiresAt: ts(now)})
	if err != nil {
		return domain.LoginCode{}, dbErr("код", err)
	}
	return domain.LoginCode{
		ID: c.ID, Email: c.Email, CodeHash: c.CodeHash, Attempts: int(c.Attempts),
		CreatedAt: c.CreatedAt.Time, ExpiresAt: c.ExpiresAt.Time,
	}, nil
}

// IncAttempts — засчитать попытку ввода (атомарно, не больше max).
func (r *Auth) IncAttempts(ctx context.Context, id int64, maxAttempts int) (int, error) {
	n, err := r.q.IncCodeAttempts(ctx, store.IncCodeAttemptsParams{ID: id, Attempts: int16(maxAttempts)})
	return int(n), dbErr("попытка кода", err)
}

// ConsumeCode — погасить код; false — его уже погасил другой запрос.
func (r *Auth) ConsumeCode(ctx context.Context, id int64, now time.Time) (bool, error) {
	n, err := r.q.ConsumeCode(ctx, store.ConsumeCodeParams{ID: id, ConsumedAt: ts(now)})
	return n == 1, dbErr("гашение кода", err)
}

// DeleteCodesBefore — удалить коды, созданные раньше t.
func (r *Auth) DeleteCodesBefore(ctx context.Context, t time.Time) (int64, error) {
	n, err := r.q.DeleteOldCodes(ctx, ts(t))
	return n, dbErr("очистка кодов", err)
}

// ===== сессии =====

// CreateSession — новая сессия (хеш токена).
func (r *Auth) CreateSession(ctx context.Context, userID int64, tokenHash []byte, expires time.Time) error {
	err := r.q.CreateSession(ctx, store.CreateSessionParams{UserID: userID, TokenHash: tokenHash, ExpiresAt: ts(expires)})
	return dbErr("новая сессия", err)
}

// SessionUser — пользователь действующей сессии.
func (r *Auth) SessionUser(ctx context.Context, tokenHash []byte, now time.Time) (domain.User, error) {
	u, err := r.q.GetSessionUser(ctx, store.GetSessionUserParams{TokenHash: tokenHash, ExpiresAt: ts(now)})
	return userOf(u), dbErr("сессия", err)
}

// DeleteSession — выход.
func (r *Auth) DeleteSession(ctx context.Context, tokenHash []byte) error {
	return dbErr("выход", r.q.DeleteSession(ctx, tokenHash))
}

// DeleteExpiredSessions — удалить истёкшие сессии.
func (r *Auth) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	n, err := r.q.DeleteExpiredSessions(ctx, ts(now))
	return n, dbErr("очистка сессий", err)
}

// DeleteUserSessions — завершить все сеансы пользователя.
func (r *Auth) DeleteUserSessions(ctx context.Context, userID int64) (int64, error) {
	n, err := r.q.DeleteUserSessions(ctx, userID)
	return n, dbErr("завершение сеансов", err)
}

// ===== преобразования =====

// dbErr переводит ошибки pgx в ошибки домена: нет строки → ErrNotFound, нарушение уникальности → ErrConflict.
func dbErr(what string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return domain.ErrConflict
	}
	return fmt.Errorf("%s: %w", what, err)
}

func userOf(u store.User) domain.User {
	out := domain.User{
		ID: u.ID, Name: u.Name, Email: u.Email.String, AvatarURL: u.AvatarUrl, City: u.City,
		ActiveMode: u.ActiveMode, CreatedAt: u.CreatedAt.Time,
	}
	out.OnboardedAt = tsPtr(u.OnboardedAt)
	out.LastLoginAt = tsPtr(u.LastLoginAt)
	out.StaffRole = domain.StaffRole(u.StaffRole.String)
	out.BlockedAt = tsPtr(u.BlockedAt)
	out.BlockedReason = u.BlockedReason
	return out
}

func text(s string) pgtype.Text         { return pgtype.Text{String: s, Valid: s != ""} }
func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ===== 2FA в сессии, входы, первый администратор =====

// SessionMFA — когда в этой сессии подтверждена 2FA (nil — не подтверждена); сессии нет — ErrNotFound.
func (r *Auth) SessionMFA(ctx context.Context, tokenHash []byte, now time.Time) (*time.Time, error) {
	row, err := r.q.GetSessionMFA(ctx, store.GetSessionMFAParams{TokenHash: tokenHash, Now: ts(now)})
	if err != nil {
		return nil, dbErr("2FA сессии", err)
	}
	return tsPtr(row.MfaAt), nil
}

// SetSessionMFA — отметить подтверждение 2FA в сессии; сессии нет — ErrNotFound.
func (r *Auth) SetSessionMFA(ctx context.Context, tokenHash []byte, at, now time.Time) error {
	n, err := r.q.SetSessionMFA(ctx, store.SetSessionMFAParams{TokenHash: tokenHash, MfaAt: ts(at), Now: ts(now)})
	if err != nil {
		return dbErr("2FA сессии", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// AddLoginEvent — запомнить вход (способ, адрес, браузер) для карточки пользователя в админке.
func (r *Auth) AddLoginEvent(ctx context.Context, userID int64, method, ip, ua string) error {
	return dbErr("вход в журнал", r.q.AddLoginEvent(ctx, store.AddLoginEventParams{UserID: userID, Method: method, Ip: ip, UserAgent: ua}))
}

// DeleteLoginEventsBefore — удалить старые входы (хранятся 180 дней).
func (r *Auth) DeleteLoginEventsBefore(ctx context.Context, before time.Time) (int64, error) {
	n, err := r.q.DeleteOldLoginEvents(ctx, ts(before))
	return n, dbErr("очистка входов", err)
}

// GrantBootstrapAdmin — роль администратора (из ADMIN_EMAILS) и запись в журнал — одной транзакцией;
// уже администратор — ErrNotFound (ничего не меняется).
func (r *Auth) GrantBootstrapAdmin(ctx context.Context, userID int64, e domain.AuditEntry) (domain.User, error) {
	var row store.User
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		if row, err = q.GrantBootstrapAdmin(ctx, userID); err != nil {
			return err
		}
		return insertAudit(ctx, q, e)
	})
	if err != nil {
		return domain.User{}, dbErr("первый администратор", err)
	}
	return userOf(row), nil
}

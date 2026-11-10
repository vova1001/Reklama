package repository

import (
	"context"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// Profiles — режим аккаунта, общие данные и профили режимов в PostgreSQL. SQL — только в queries/profile.sql
// и queries/auth.sql.
type Profiles struct{ q *store.Queries }

// NewProfiles создаёт репозиторий профилей.
func NewProfiles(db store.DBTX) *Profiles { return &Profiles{q: store.New(db)} }

// SetMode — сделать режим активным; пользователя нет — ErrNotFound.
func (r *Profiles) SetMode(ctx context.Context, userID int64, mode string) (domain.User, error) {
	u, err := r.q.SetUserMode(ctx, store.SetUserModeParams{ID: userID, ActiveMode: mode})
	return userOf(u), dbErr("режим", err)
}

// UpdateBasics — общие данные (первое сохранение отмечает «заполнены»).
func (r *Profiles) UpdateBasics(ctx context.Context, userID int64, b domain.Basics) (domain.User, error) {
	u, err := r.q.UpdateUserBasics(ctx, store.UpdateUserBasicsParams{ID: userID, Name: b.Name, City: b.City})
	return userOf(u), dbErr("общие данные", err)
}

// BusinessProfile — профиль бизнеса; не заполнен — ErrNotFound.
func (r *Profiles) BusinessProfile(ctx context.Context, userID int64) (domain.BusinessProfile, error) {
	p, err := r.q.GetBusinessProfile(ctx, userID)
	return businessOf(p), dbErr("профиль бизнеса", err)
}

// CreatorProfile — профиль автора; не заполнен — ErrNotFound.
func (r *Profiles) CreatorProfile(ctx context.Context, userID int64) (domain.CreatorProfile, error) {
	p, err := r.q.GetCreatorProfile(ctx, userID)
	return creatorOf(p), dbErr("профиль автора", err)
}

// SaveBusinessProfile — создать или обновить профиль бизнеса.
func (r *Profiles) SaveBusinessProfile(ctx context.Context, userID int64, p domain.BusinessProfile) (domain.BusinessProfile, error) {
	out, err := r.q.UpsertBusinessProfile(ctx, store.UpsertBusinessProfileParams{
		UserID: userID, Name: p.Name, City: p.City, Spheres: nonNil(p.Spheres),
	})
	return businessOf(out), dbErr("сохранение профиля бизнеса", err)
}

// SaveCreatorProfile — создать или обновить профиль автора.
func (r *Profiles) SaveCreatorProfile(ctx context.Context, userID int64, p domain.CreatorProfile) (domain.CreatorProfile, error) {
	out, err := r.q.UpsertCreatorProfile(ctx, store.UpsertCreatorProfileParams{
		UserID: userID, Name: p.Name, City: p.City, Spheres: nonNil(p.Spheres), Channels: nonNil(p.Channels),
	})
	return creatorOf(out), dbErr("сохранение профиля автора", err)
}

// CountCategories — сколько из переданных тем существует и активно.
func (r *Profiles) CountCategories(ctx context.Context, slugs []string) (int, error) {
	n, err := r.q.CountActiveCategories(ctx, nonNil(slugs))
	return int(n), dbErr("темы", err)
}

func businessOf(p store.BusinessProfile) domain.BusinessProfile {
	return domain.BusinessProfile{Name: p.Name, City: p.City, Spheres: nonNil(p.Spheres), UpdatedAt: p.UpdatedAt.Time}
}

func creatorOf(p store.CreatorProfile) domain.CreatorProfile {
	return domain.CreatorProfile{Name: p.Name, City: p.City, Spheres: nonNil(p.Spheres), Channels: nonNil(p.Channels), UpdatedAt: p.UpdatedAt.Time}
}

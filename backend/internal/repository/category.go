package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// Categories — справочник категорий и свои категории пользователей. SQL — только в queries/category.sql.
type Categories struct {
	db DB
	q  *store.Queries
}

// NewCategories создаёт репозиторий категорий.
func NewCategories(db DB) *Categories { return &Categories{db: db, q: store.New(db)} }

// Common — общий справочник (одобренные и активные), по порядку.
func (r *Categories) Common(ctx context.Context) ([]domain.CategoryInfo, error) {
	rows, err := r.q.ListCommonCategories(ctx)
	return categoriesOf(rows), dbErr("справочник категорий", err)
}

// ByAuthor — свои категории пользователя (на проверке и отклонённые).
func (r *Categories) ByAuthor(ctx context.Context, userID int64) ([]domain.CategoryInfo, error) {
	rows, err := r.q.ListAuthorCategories(ctx, pgInt8(userID))
	return categoriesOf(rows), dbErr("свои категории", err)
}

// FindByNorm — общая категория с таким названием, иначе своя на проверке у этого автора; нет — ErrNotFound.
func (r *Categories) FindByNorm(ctx context.Context, userID int64, norm string) (domain.CategoryInfo, error) {
	c, err := r.q.FindCommonCategory(ctx, norm)
	if err == nil {
		return categoryOf(c), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.CategoryInfo{}, dbErr("категория", err)
	}
	c, err = r.q.FindAuthorCategory(ctx, store.FindAuthorCategoryParams{AuthorID: pgInt8(userID), Norm: norm})
	return categoryOf(c), dbErr("своя категория", err)
}

// CountPending — сколько своих категорий пользователя ждут проверки (лимит).
func (r *Categories) CountPending(ctx context.Context, userID int64) (int, error) {
	n, err := r.q.CountAuthorPendingCategories(ctx, pgInt8(userID))
	return int(n), dbErr("число своих категорий", err)
}

// CreateCustom — своя категория «на проверке»; такая уже есть у автора — ErrConflict.
func (r *Categories) CreateCustom(ctx context.Context, userID int64, slug, title, norm string) (domain.CategoryInfo, error) {
	c, err := r.q.CreateCustomCategory(ctx, store.CreateCustomCategoryParams{Slug: slug, Title: title, AuthorID: pgInt8(userID), Norm: norm})
	return categoryOf(c), dbErr("новая категория", err)
}

// Usable — сколько из тем годится пользователю (общие или его свои на проверке) и сколько из них свои.
func (r *Categories) Usable(ctx context.Context, userID int64, slugs []string) (int, int, error) {
	row, err := r.q.UsableCategories(ctx, store.UsableCategoriesParams{Slugs: nonNil(slugs), UserID: pgInt8(userID)})
	return int(row.Usable), int(row.Custom), dbErr("проверка категорий", err)
}

// Category — категория по id (для модерации).
func (r *Categories) Category(ctx context.Context, id int64) (domain.CategoryInfo, error) {
	c, err := r.q.GetCategory(ctx, id)
	return categoryOf(c), dbErr("категория", err)
}

// SetCategoryStatus — решение модератора и запись в журнал. Статус уже другой — ErrConflict; одобрить, когда
// общая с таким названием уже есть, — тоже ErrConflict (уникальность в базе).
func (r *Categories) SetCategoryStatus(ctx context.Context, ch domain.StatusChange) (domain.CategoryInfo, error) {
	var out store.Category
	err := inTx(ctx, r.db, r.q, func(q *store.Queries) error {
		var err error
		out, err = q.SetCategoryStatus(ctx, store.SetCategoryStatusParams{ID: ch.SubjectID, FromStatus: string(ch.From), ToStatus: string(ch.To)})
		if err != nil {
			return err
		}
		return q.LogCategoryChange(ctx, store.LogCategoryChangeParams{
			SubjectID: ch.SubjectID, FromStatus: string(ch.From), ToStatus: string(ch.To), Reason: ch.Reason,
			ActorID: pgtype.Int8{Int64: ch.ActorID, Valid: ch.ActorID != 0},
		})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CategoryInfo{}, domain.ErrConflict
	}
	return categoryOf(out), dbErr("статус категории", err)
}

func categoryOf(c store.Category) domain.CategoryInfo {
	return domain.CategoryInfo{
		ID: c.ID, Slug: c.Slug, Title: c.Title, Icon: c.Icon, Status: c.Status, AuthorID: c.AuthorID.Int64, CreatedAt: c.CreatedAt.Time,
	}
}

func categoriesOf(rows []store.Category) []domain.CategoryInfo {
	out := make([]domain.CategoryInfo, 0, len(rows))
	for _, c := range rows {
		out = append(out, categoryOf(c))
	}
	return out
}

// inTx — fn в транзакции (общий помощник репозиториев).
func inTx(ctx context.Context, db DB, q *store.Queries, fn func(q *store.Queries) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

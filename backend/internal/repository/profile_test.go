package repository_test

import (
	"context"
	"errors"
	"testing"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/repository"
	"ohvat/backend/migrations"
)

func TestProfiles(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	auth := repository.NewAuth(pool)
	r := repository.NewProfiles(pool)
	u, err := auth.CreateUserWithIdentity(ctx, domain.NewUser{Name: "Анна"}, domain.Identity{Provider: "email", Subject: "a@x.ru"})
	if err != nil {
		t.Fatal(err)
	}

	// режим
	got, err := r.SetMode(ctx, u.ID, domain.ModeCreator)
	if err != nil || got.ActiveMode != domain.ModeCreator {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := r.SetMode(ctx, u.ID, "admin"); err == nil {
		t.Error("CHECK в базе: чужой режим не записывается")
	}
	if _, err := r.SetMode(ctx, 999999, domain.ModeBusiness); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("нет пользователя: %v", err)
	}

	// общие данные: дата первого заполнения не меняется
	b, err := r.UpdateBasics(ctx, u.ID, domain.Basics{Name: "Анна К", City: "Казань"})
	if err != nil || !b.Onboarded() || b.City != "Казань" || b.ActiveMode != domain.ModeCreator {
		t.Fatalf("%v %+v", err, b)
	}
	first := *b.OnboardedAt
	b, _ = r.UpdateBasics(ctx, u.ID, domain.Basics{Name: "Анна", City: "Казань"})
	if !b.OnboardedAt.Equal(first) {
		t.Error("дата заполнения не меняется при правке")
	}

	// профили: пусто → создать → обновить
	if _, err := r.BusinessProfile(ctx, u.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("бизнес пуст: %v", err)
	}
	if _, err := r.CreatorProfile(ctx, u.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("автор пуст: %v", err)
	}
	bp, err := r.SaveBusinessProfile(ctx, u.ID, domain.BusinessProfile{Name: "Кофейня", City: "Казань", Spheres: []string{"food"}})
	if err != nil || bp.Name != "Кофейня" || bp.Spheres[0] != "food" || bp.UpdatedAt.IsZero() {
		t.Fatalf("%v %+v", err, bp)
	}
	bp, err = r.SaveBusinessProfile(ctx, u.ID, domain.BusinessProfile{Name: "Зерно", City: "Казань"})
	if err != nil || bp.Name != "Зерно" || len(bp.Spheres) != 0 || bp.Spheres == nil {
		t.Fatalf("обновление: %v %+v", err, bp)
	}
	cp, err := r.SaveCreatorProfile(ctx, u.ID, domain.CreatorProfile{Name: "Алина", City: "Москва", Spheres: []string{"beauty"}, Channels: []string{"@alina_tg"}})
	if err != nil || cp.Channels[0] != "@alina_tg" {
		t.Fatalf("%v %+v", err, cp)
	}
	cp, err = r.CreatorProfile(ctx, u.ID)
	if err != nil || cp.Name != "Алина" || cp.Spheres[0] != "beauty" {
		t.Fatalf("%v %+v", err, cp)
	}
	if got, err := r.BusinessProfile(ctx, u.ID); err != nil || got.Name != "Зерно" {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := r.SaveBusinessProfile(ctx, 999999, domain.BusinessProfile{Name: "x", City: "y"}); err == nil {
		t.Error("профиль без пользователя (внешний ключ)")
	}

	// справочник тем
	n, err := r.CountCategories(ctx, []string{"food", "beauty", "nope"})
	if err != nil || n != 2 {
		t.Errorf("темы: %d %v", n, err)
	}
	// удаление пользователя удаляет профили
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreatorProfile(ctx, u.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("каскад: %v", err)
	}
}

// Миграция 00005: прежние роли и анкеты переносятся в режим и профили, откат возвращает роль.
func TestModesMigration(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if err := migrations.DownTo(pool, 4); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = migrations.Up(pool) })
	var biz, cre, none int64
	q := `INSERT INTO users (role, name, city, spheres, channels, onboarded_at) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`
	if err := pool.QueryRow(ctx, q, "business", "Кофейня", "Казань", []string{"food"}, []string{}, "2026-01-01T00:00:00Z").Scan(&biz); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, q, "creator", "Алина", "Москва", []string{}, []string{"@alina_tg"}, "2026-01-01T00:00:00Z").Scan(&cre); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, q, nil, "Без анкеты", "", []string{}, []string{}, nil).Scan(&none); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO login_codes (email, code_hash, role, expires_at) VALUES ('a@x.ru', '\x01', 'creator', now())`); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Up(pool); err != nil {
		t.Fatal(err)
	}

	r := repository.NewProfiles(pool)
	a := repository.NewAuth(pool)
	if u, _ := a.UserByID(ctx, cre); u.ActiveMode != domain.ModeCreator || u.Name != "Алина" {
		t.Errorf("автор: %+v", u)
	}
	if u, _ := a.UserByID(ctx, none); u.ActiveMode != domain.ModeBusiness {
		t.Errorf("без роли — бизнес: %+v", u)
	}
	if p, err := r.BusinessProfile(ctx, biz); err != nil || p.Name != "Кофейня" || p.Spheres[0] != "food" {
		t.Errorf("профиль бизнеса: %v %+v", err, p)
	}
	if p, err := r.CreatorProfile(ctx, cre); err != nil || p.Channels[0] != "@alina_tg" || p.City != "Москва" {
		t.Errorf("профиль автора: %v %+v", err, p)
	}
	if _, err := r.BusinessProfile(ctx, none); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("без анкеты — без профиля: %v", err)
	}

	// откат: у кого есть оба профиля — роль активного режима
	if _, err := r.SaveBusinessProfile(ctx, cre, domain.BusinessProfile{Name: "Студия", City: "Москва", Spheres: []string{"beauty"}}); err != nil {
		t.Fatal(err)
	}
	if err := migrations.DownTo(pool, 4); err != nil {
		t.Fatal(err)
	}
	var role *string
	var channels []string
	if err := pool.QueryRow(ctx, `SELECT role, channels FROM users WHERE id = $1`, cre).Scan(&role, &channels); err != nil {
		t.Fatal(err)
	}
	if role == nil || *role != "creator" || len(channels) != 1 {
		t.Errorf("откат, активный режим автора: %v %v", role, channels)
	}
	if err := pool.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, biz).Scan(&role); err != nil || role == nil || *role != "business" {
		t.Errorf("откат, бизнес: %v %v", err, role)
	}
	if err := pool.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, none).Scan(&role); err != nil || role != nil {
		t.Errorf("откат, без профиля — без роли: %v %v", err, role)
	}
}

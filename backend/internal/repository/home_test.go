package repository_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/repository"
	"ohvat/backend/migrations"
)

// Интеграционные тесты на настоящей PostgreSQL (make test поднимает её в Docker).
// Без TEST_DATABASE_URL — пропускаются.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL не задан — интеграционные тесты пропущены")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// С чистого листа: откат и накат — заодно проверка обратимости миграций.
	if err := migrations.Up(pool); err != nil {
		t.Fatalf("миграции up: %v", err)
	}
	if err := migrations.Down(pool); err != nil {
		t.Fatalf("миграции down: %v", err)
	}
	if err := migrations.Up(pool); err != nil {
		t.Fatalf("миграции up повторно: %v", err)
	}
	return pool
}

func TestSeedData(t *testing.T) {
	ctx := context.Background()
	r := repository.NewHome(testPool(t))

	cats, err := r.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 12 || cats[0].Title != "Еда и кафе" || cats[11].Title != "Путешествия" {
		t.Errorf("темы: %d, %+v", len(cats), cats)
	}

	plans, err := r.Plans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var biz, aut []domain.Plan
	for _, p := range plans {
		if p.Audience == domain.AudienceBusiness {
			biz = append(biz, p)
		} else {
			aut = append(aut, p)
		}
	}
	if len(biz) != 3 || len(aut) != 3 {
		t.Fatalf("тарифы: бизнес %d, авторы %d", len(biz), len(aut))
	}
	if biz[0].PriceKopecks != 0 || biz[1].PriceKopecks != 300000 || biz[2].PriceKopecks != 1000000 {
		t.Errorf("цены бизнеса: %+v", biz)
	}
	if !biz[1].Recommended || biz[0].Recommended || !aut[1].Recommended {
		t.Error("рекомендуемый тариф — «Про»")
	}
	if len(biz[2].Features) != 5 || biz[2].Features[0] != "Размещение 60 дней" {
		t.Errorf("пункты «Максимум» по порядку: %+v", biz[2].Features)
	}
	if aut[2].Name != "Топ" || len(aut[2].Features) != 3 {
		t.Errorf("тариф авторов «Топ»: %+v", aut[2])
	}

	faq, err := r.FAQ(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(faq) != 4 || faq[0].Question != "Как сервис проверяет выполнение?" {
		t.Errorf("вопросы: %+v", faq)
	}
}

// Скрытое (is_active / is_published = false) на главную не попадает; порядок — по position.
func TestFiltersAndOrder(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	exec := func(sql string) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE categories SET is_active = false WHERE slug = 'food'`)
	exec(`UPDATE categories SET position = 0 WHERE slug = 'travel'`)
	exec(`UPDATE plans SET is_active = false WHERE audience = 'creator' AND slug = 'top'`)
	exec(`UPDATE faq SET is_published = false WHERE position = 1`)
	exec(`INSERT INTO plans (audience, slug, name, price_kopecks, position) VALUES ('creator', 'empty', 'Пустой', 100, 9)`)

	r := repository.NewHome(tx)
	cats, err := r.Categories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 11 || cats[0].Slug != "travel" {
		t.Errorf("темы после скрытия и сортировки: %+v", cats)
	}
	plans, err := r.Plans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, p := range plans {
		if p.Audience == "creator" && p.Slug == "top" {
			t.Error("скрытый тариф попал в выдачу")
		}
		if p.Slug == "empty" {
			found = true
			if p.Features == nil || len(p.Features) != 0 {
				t.Errorf("тариф без пунктов — пустой список: %+v", p.Features)
			}
		}
	}
	if !found {
		t.Error("новый тариф не найден")
	}
	faq, err := r.FAQ(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(faq) != 3 {
		t.Errorf("неопубликованный вопрос попал в выдачу: %d", len(faq))
	}
}

// Ошибка базы возвращается наверх, а не теряется.
func TestQueryError(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	conn, err := pgx.Connect(ctx, pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(ctx)
	r := repository.NewHome(conn)
	if _, err := r.Categories(ctx); err == nil {
		t.Error("темы: ожидали ошибку закрытого соединения")
	}
	if _, err := r.Plans(ctx); err == nil {
		t.Error("тарифы: ожидали ошибку")
	}
	if _, err := r.FAQ(ctx); err == nil {
		t.Error("вопросы: ожидали ошибку")
	}
}

package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/repository"
)

func TestAuthUsersAndIdentities(t *testing.T) {
	ctx := context.Background()
	r := repository.NewAuth(testPool(t))

	u, err := r.CreateUserWithIdentity(ctx, domain.NewUser{Role: "business", Name: "Анна", Email: "anna@mail.ru"},
		domain.Identity{Provider: "email", Subject: "anna@mail.ru", Email: "anna@mail.ru"})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 || u.Role != "business" || u.Email != "anna@mail.ru" || u.Onboarded() || u.Spheres == nil {
		t.Fatalf("%+v", u)
	}
	// поиск
	for name, f := range map[string]func() (domain.User, error){
		"по id":      func() (domain.User, error) { return r.UserByID(ctx, u.ID) },
		"по способу": func() (domain.User, error) { return r.UserByIdentity(ctx, "email", "anna@mail.ru") },
		"по почте":   func() (domain.User, error) { return r.UserByEmail(ctx, "anna@mail.ru") },
	} {
		got, err := f()
		if err != nil || got.ID != u.ID {
			t.Errorf("%s: %v %+v", name, err, got)
		}
	}
	if _, err := r.UserByEmail(ctx, "nope@mail.ru"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("нет почты: %v", err)
	}
	if _, err := r.UserByIdentity(ctx, "google", "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("нет способа: %v", err)
	}
	// конфликт почты и способа — откат транзакции, пользователь не создаётся
	if _, err := r.CreateUserWithIdentity(ctx, domain.NewUser{Email: "anna@mail.ru"}, domain.Identity{Provider: "google", Subject: "g1"}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("дубль почты: %v", err)
	}
	if _, err := r.UserByIdentity(ctx, "google", "g1"); !errors.Is(err, domain.ErrNotFound) {
		t.Error("транзакция должна откатиться целиком")
	}
	if _, err := r.CreateUserWithIdentity(ctx, domain.NewUser{}, domain.Identity{Provider: "email", Subject: "anna@mail.ru"}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("дубль способа: %v", err)
	}

	// Telegram без почты → привязка Google с почтой: почта появляется у пользователя
	tg, err := r.CreateUserWithIdentity(ctx, domain.NewUser{Name: "Иван"}, domain.Identity{Provider: "telegram", Subject: "42"})
	if err != nil || tg.Email != "" || tg.Role != "" {
		t.Fatalf("%v %+v", err, tg)
	}
	if err := r.LinkIdentity(ctx, tg.ID, domain.Identity{Provider: "google", Subject: "g2", Email: "ivan@gmail.com"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.UserByEmail(ctx, "ivan@gmail.com"); got.ID != tg.ID {
		t.Error("почта из Google должна записаться")
	}
	if err := r.LinkIdentity(ctx, u.ID, domain.Identity{Provider: "google", Subject: "g2"}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("способ уже привязан к другому: %v", err)
	}
	p, err := r.Providers(ctx, tg.ID)
	if err != nil || len(p) != 2 || p[0] != "google" || p[1] != "telegram" {
		t.Errorf("способы: %v %v", p, err)
	}

	// роль записывается, только если пустая
	if err := r.TouchLogin(ctx, tg.ID, domain.Identity{Provider: "telegram", Subject: "42"}, "creator"); err != nil {
		t.Fatal(err)
	}
	if err := r.TouchLogin(ctx, tg.ID, domain.Identity{Provider: "telegram", Subject: "42"}, "business"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.UserByID(ctx, tg.ID); got.Role != "creator" {
		t.Errorf("роль: %q", got.Role)
	}

	// анкета
	n, err := r.CountCategories(ctx, []string{"food", "beauty", "nope"})
	if err != nil || n != 2 {
		t.Errorf("темы: %d %v", n, err)
	}
	pr, err := r.UpdateProfile(ctx, u.ID, domain.Profile{Role: "business", Name: "Кофейня", City: "Казань", Spheres: []string{"food"}})
	if err != nil || !pr.Onboarded() || pr.City != "Казань" || pr.Spheres[0] != "food" || len(pr.Channels) != 0 {
		t.Fatalf("%v %+v", err, pr)
	}
	first := *pr.OnboardedAt
	pr, _ = r.UpdateProfile(ctx, u.ID, domain.Profile{Role: "business", Name: "Кофейня 2", City: "Казань", Spheres: []string{"food"}})
	if !pr.OnboardedAt.Equal(first) {
		t.Error("дата заполнения анкеты не меняется при правке")
	}
}

func TestAuthCodes(t *testing.T) {
	ctx := context.Background()
	r := repository.NewAuth(testPool(t))
	now := time.Now().UTC().Truncate(time.Microsecond)
	mk := func(email, ip string, at time.Time, hash string) {
		t.Helper()
		if err := r.CreateCode(ctx, domain.NewLoginCode{Email: email, CodeHash: []byte(hash), Role: "creator", IP: ip, CreatedAt: at, ExpiresAt: at.Add(10 * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	mk("a@mail.ru", "1.1.1.1", now.Add(-3*time.Minute), "h1")
	mk("a@mail.ru", "1.1.1.1", now.Add(-time.Minute), "h2")
	mk("b@mail.ru", "1.1.1.1", now.Add(-2*time.Hour), "h3")

	st, err := r.CodeStatsByEmail(ctx, "a@mail.ru", now.Add(-time.Hour))
	if err != nil || st.Sent != 2 || !st.LastAt.Equal(now.Add(-time.Minute)) || !st.FirstAt.Equal(now.Add(-3*time.Minute)) {
		t.Errorf("по почте: %+v %v", st, err)
	}
	ip, err := r.CodeStatsByIP(ctx, "1.1.1.1", now.Add(-time.Hour))
	if err != nil || ip.Sent != 2 {
		t.Errorf("по адресу: %+v %v", ip, err)
	}
	empty, _ := r.CodeStatsByEmail(ctx, "none@mail.ru", now.Add(-time.Hour))
	if empty.Sent != 0 {
		t.Error("пусто")
	}

	// активный — только последний (прежний погашен новым)
	c, err := r.ActiveCode(ctx, "a@mail.ru", now)
	if err != nil || string(c.CodeHash) != "h2" || c.Role != "creator" {
		t.Fatalf("%v %+v", err, c)
	}
	if _, err := r.ActiveCode(ctx, "a@mail.ru", now.Add(10*time.Minute)); !errors.Is(err, domain.ErrNotFound) {
		t.Error("истёкший код")
	}

	// попытки атомарны: 10 параллельных — засчитано ровно 5
	var wg sync.WaitGroup
	var mu sync.Mutex
	okN := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.IncAttempts(ctx, c.ID, 5); err == nil {
				mu.Lock()
				okN++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if okN != 5 {
		t.Errorf("попыток засчитано %d", okN)
	}
	if _, err := r.IncAttempts(ctx, c.ID, 5); !errors.Is(err, domain.ErrNotFound) {
		t.Error("6-я попытка")
	}

	// погасить можно один раз
	ok1, _ := r.ConsumeCode(ctx, c.ID, now)
	ok2, _ := r.ConsumeCode(ctx, c.ID, now)
	if !ok1 || ok2 {
		t.Errorf("гашение: %v %v", ok1, ok2)
	}
	n, err := r.DeleteCodesBefore(ctx, now.Add(-time.Hour))
	if err != nil || n != 1 {
		t.Errorf("очистка: %d %v", n, err)
	}
}

func TestAuthSessions(t *testing.T) {
	ctx := context.Background()
	r := repository.NewAuth(testPool(t))
	u, err := r.CreateUserWithIdentity(ctx, domain.NewUser{Name: "A"}, domain.Identity{Provider: "telegram", Subject: "1"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := r.CreateSession(ctx, u.ID, []byte("hash-live"), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateSession(ctx, u.ID, []byte("hash-old"), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, err := r.SessionUser(ctx, []byte("hash-live"), now); err != nil || got.ID != u.ID {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := r.SessionUser(ctx, []byte("hash-old"), now); !errors.Is(err, domain.ErrNotFound) {
		t.Error("истёкшая сессия")
	}
	if n, err := r.DeleteExpiredSessions(ctx, now); err != nil || n != 1 {
		t.Errorf("очистка: %d %v", n, err)
	}
	if err := r.DeleteSession(ctx, []byte("hash-live")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SessionUser(ctx, []byte("hash-live"), now); !errors.Is(err, domain.ErrNotFound) {
		t.Error("после выхода")
	}
	if err := repository.NewHealth(testPool(t)).Ping(ctx); err != nil {
		t.Error(err)
	}
}

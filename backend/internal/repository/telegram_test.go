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

func TestTelegramLogins(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	r := repository.NewTelegram(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	tok, br := []byte("token-hash-1"), []byte("browser-hash-1")
	if err := r.CreateTelegramLogin(ctx, domain.NewTelegramLogin{TokenHash: tok, BrowserHash: br, Role: "creator", IP: "1.1.1.1",
		CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateTelegramLogin(ctx, domain.NewTelegramLogin{TokenHash: tok, BrowserHash: []byte("b2"), CreatedAt: now, ExpiresAt: now}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("дубль токена: %v", err)
	}
	if n, first, err := r.TelegramLoginStats(ctx, "1.1.1.1", now.Add(-time.Hour)); err != nil || n != 1 || !first.Equal(now) {
		t.Errorf("%d %v %v", n, first, err)
	}
	l, err := r.PendingTelegramLogin(ctx, tok, now)
	if err != nil || l.Role != "creator" || l.ConfirmedAt != nil {
		t.Fatalf("%v %+v", err, l)
	}
	if _, err := r.PendingTelegramLogin(ctx, tok, now.Add(6*time.Minute)); !errors.Is(err, domain.ErrNotFound) {
		t.Error("истёк", err)
	}
	// забрать до подтверждения — нельзя
	if _, err := r.ConsumeTelegramLogin(ctx, br, now); !errors.Is(err, domain.ErrNotFound) {
		t.Error(err)
	}
	p := domain.TelegramProfile{ID: 777, FirstName: "Анна", LastName: "К", Username: "anna"}
	l, err = r.ConfirmTelegramLogin(ctx, tok, p, now.Add(time.Minute), now.Add(3*time.Minute))
	if err != nil || l.TgUserID != 777 || l.TgName != "Анна К" || l.ConfirmedAt == nil || !l.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("%v %+v", err, l)
	}
	// тот же человек — снова можно, другой — нет; grace продлевает срок
	if l, err = r.ConfirmTelegramLogin(ctx, tok, p, now.Add(4*time.Minute), now.Add(7*time.Minute)); err != nil || !l.ExpiresAt.Equal(now.Add(7*time.Minute)) {
		t.Errorf("%v %+v", err, l)
	}
	if _, err := r.ConfirmTelegramLogin(ctx, tok, domain.TelegramProfile{ID: 9}, now, now); !errors.Is(err, domain.ErrNotFound) {
		t.Error("другой человек", err)
	}
	// отменить подтверждённый — нельзя
	if _, err := r.CancelTelegramLogin(ctx, tok, now); !errors.Is(err, domain.ErrNotFound) {
		t.Error(err)
	}
	// забрать — ровно один из параллельных запросов
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.ConsumeTelegramLogin(ctx, br, now.Add(2*time.Minute)); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("забрали %d раз", ok)
	}
	if l, err := r.TelegramLoginByBrowser(ctx, br); err != nil || l.ConsumedAt == nil || l.Cancelled {
		t.Errorf("%v %+v", err, l)
	}
	// отмена неподтверждённого
	_ = r.CreateTelegramLogin(ctx, domain.NewTelegramLogin{TokenHash: []byte("t2"), BrowserHash: []byte("b2"), CreatedAt: now, ExpiresAt: now.Add(time.Minute)})
	if l, err := r.CancelTelegramLogin(ctx, []byte("t2"), now); err != nil || !l.Cancelled || l.ConsumedAt == nil {
		t.Errorf("%v %+v", err, l)
	}
	if _, err := r.ConfirmTelegramLogin(ctx, []byte("t2"), p, now, now); !errors.Is(err, domain.ErrNotFound) {
		t.Error("после отмены", err)
	}
	if _, err := r.TelegramLoginByBrowser(ctx, []byte("nope")); !errors.Is(err, domain.ErrNotFound) {
		t.Error(err)
	}
	if n, err := r.DeleteTelegramLoginsBefore(ctx, now.Add(time.Second)); err != nil || n != 2 {
		t.Error(n, err)
	}
}

func TestTelegramChatsDevicesSessions(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	r := repository.NewTelegram(pool)
	auth := repository.NewAuth(pool)

	c, err := r.SaveChat(ctx, 777, 777, "anna")
	if err != nil || !c.Notify || c.Blocked || c.Username != "anna" {
		t.Fatalf("%v %+v", err, c)
	}
	// пользователь сайта с Telegram 777 — уведомления идут в его чат
	u, err := auth.CreateUserWithIdentity(ctx, domain.NewUser{Name: "Анна"}, domain.Identity{Provider: "telegram", Subject: "777"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.NotifyChat(ctx, u.ID); err != nil || got.ChatID != 777 {
		t.Fatalf("%v %+v", err, got)
	}
	if err := r.SetNotify(ctx, 777, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.NotifyChat(ctx, u.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Error("после /stop", err)
	}
	_ = r.SetNotify(ctx, 777, true)
	if err := r.BlockChat(ctx, 777); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Chat(ctx, 777); !got.Blocked {
		t.Error("blocked")
	}
	if _, err := r.NotifyChat(ctx, u.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Error("бот остановлен", err)
	}
	if c, _ = r.SaveChat(ctx, 777, 777, "anna2"); c.Blocked || c.Username != "anna2" {
		t.Errorf("снова написал боту: %+v", c)
	}
	if err := r.SetNotify(ctx, 1, true); !errors.Is(err, domain.ErrNotFound) {
		t.Error(err)
	}
	if _, err := r.Chat(ctx, 1); !errors.Is(err, domain.ErrNotFound) {
		t.Error(err)
	}
	// без Telegram — некуда
	u2, _ := auth.CreateUserWithIdentity(ctx, domain.NewUser{}, domain.Identity{Provider: "email", Subject: "x@x.ru"})
	if _, err := r.NotifyChat(ctx, u2.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Error(err)
	}

	// устройства: первый раз — true
	if first, err := r.TouchDevice(ctx, u.ID, []byte("dev")); err != nil || !first {
		t.Fatal(first, err)
	}
	if first, err := r.TouchDevice(ctx, u.ID, []byte("dev")); err != nil || first {
		t.Fatal(first, err)
	}
	if first, _ := r.TouchDevice(ctx, u2.ID, []byte("dev")); !first {
		t.Error("у другого пользователя — своё")
	}

	// завершить все сеансы
	exp := time.Now().Add(time.Hour)
	_ = auth.CreateSession(ctx, u.ID, []byte("s1"), exp)
	_ = auth.CreateSession(ctx, u.ID, []byte("s2"), exp)
	_ = auth.CreateSession(ctx, u2.ID, []byte("s3"), exp)
	if n, err := auth.DeleteUserSessions(ctx, u.ID); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if _, err := auth.SessionUser(ctx, []byte("s3"), time.Now()); err != nil {
		t.Error("чужая сессия удалена")
	}
}

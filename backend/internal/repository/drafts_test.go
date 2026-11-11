package repository_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/repository"
	"ohvat/backend/internal/service"
)

// Общее для обоих хранилищ: сохранить, заменить, прочитать, чужое не видно, удалить.
func checkDraftStore(t *testing.T, s service.DraftStore, uid, other int64) {
	t.Helper()
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := s.GetDraft(ctx, uid, "card"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("нет: %v", err)
	}
	if err := s.PutDraft(ctx, uid, domain.FormDraft{Form: "card", Data: []byte(`{"name":"Алина"}`), SavedAt: at}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := s.PutDraft(ctx, uid, domain.FormDraft{Form: "card", Data: []byte(`{"name":"Алина К"}`), SavedAt: at.Add(time.Second)}, time.Hour); err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDraft(ctx, uid, "card")
	if err != nil || d.Form != "card" || !d.SavedAt.Equal(at.Add(time.Second)) {
		t.Fatalf("%v %+v", err, d)
	}
	if string(d.Data) != `{"name":"Алина К"}` && string(d.Data) != `{"name": "Алина К"}` {
		t.Errorf("данные: %s", d.Data)
	}
	if _, err := s.GetDraft(ctx, other, "card"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("чужое: %v", err)
	}
	if err := s.DeleteDraft(ctx, uid, "card"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDraft(ctx, uid, "card"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("удалён: %v", err)
	}
}

func TestDraftsPG(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	auth := repository.NewAuth(pool)
	u, o := newUser(t, auth, "d1@x.ru"), newUser(t, auth, "d2@x.ru")
	r := repository.NewDraftsPG(pool)
	checkDraftStore(t, r, u.ID, o.ID)
	// старше 7 дней — не читается и удаляется очисткой
	_ = r.PutDraft(ctx, u.ID, domain.FormDraft{Form: "task-new", Data: []byte(`{}`), SavedAt: time.Now().Add(-8 * 24 * time.Hour)}, 0)
	if _, err := r.GetDraft(ctx, u.ID, "task-new"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("истёк: %v", err)
	}
	if n, err := r.Cleanup(ctx, time.Now().Add(-7*24*time.Hour)); err != nil || n != 1 {
		t.Errorf("очистка: %v %d", err, n)
	}
}

func TestDraftsRedis(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL не задан")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })
	ctx := context.Background()
	r := repository.NewDraftsRedis(rdb)
	uid := time.Now().UnixNano() % 1e9
	checkDraftStore(t, r, uid, uid+1)
	// срок — у ключа
	_ = r.PutDraft(ctx, uid, domain.FormDraft{Form: "task-5", Data: []byte(`{}`), SavedAt: time.Now()}, 7*24*time.Hour)
	if ttl := rdb.TTL(ctx, "ohvat:draft:"+itoa(uid)+":task-5").Val(); ttl < 6*24*time.Hour || ttl > 7*24*time.Hour {
		t.Errorf("TTL: %v", ttl)
	}
	// испорченное значение — ошибка, не паника
	rdb.Set(ctx, "ohvat:draft:"+itoa(uid)+":card", "not json", time.Minute)
	if _, err := r.GetDraft(ctx, uid, "card"); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Errorf("испорченное: %v", err)
	}
	// Redis недоступен — ошибка (запасное хранилище решает выше)
	bad := repository.NewDraftsRedis(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond}))
	if err := bad.PutDraft(ctx, uid, domain.FormDraft{Form: "card", Data: []byte(`{}`)}, time.Minute); err == nil {
		t.Error("недоступен: запись")
	}
	if _, err := bad.GetDraft(ctx, uid, "card"); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Error("недоступен: чтение")
	}
	if err := bad.DeleteDraft(ctx, uid, "card"); err == nil {
		t.Error("недоступен: удаление")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

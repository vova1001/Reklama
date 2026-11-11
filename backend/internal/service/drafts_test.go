package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
)

// memDrafts — черновики форм в памяти; fail — ошибка любого метода (как недоступный Redis).
type memDrafts struct {
	mu   sync.Mutex
	m    map[string]domain.FormDraft
	ttl  map[string]time.Duration
	fail error
}

func newMemDrafts() *memDrafts {
	return &memDrafts{m: map[string]domain.FormDraft{}, ttl: map[string]time.Duration{}}
}

func dkey(uid int64, form string) string { return strconv.FormatInt(uid, 10) + ":" + form }

func (s *memDrafts) GetDraft(_ context.Context, uid int64, form string) (domain.FormDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return domain.FormDraft{}, s.fail
	}
	d, ok := s.m[dkey(uid, form)]
	if !ok {
		return domain.FormDraft{}, domain.ErrNotFound
	}
	return d, nil
}

func (s *memDrafts) PutDraft(_ context.Context, uid int64, d domain.FormDraft, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.m[dkey(uid, d.Form)], s.ttl[dkey(uid, d.Form)] = d, ttl
	return nil
}

func (s *memDrafts) DeleteDraft(_ context.Context, uid int64, form string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	delete(s.m, dkey(uid, form))
	return nil
}

func (s *memDrafts) Cleanup(context.Context, time.Time) (int64, error) { return 0, s.fail }

func TestDrafts(t *testing.T) {
	ctx := context.Background()
	st := newMemDrafts()
	clock := &fakeClock{t: time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)}
	s := NewDrafts(st, DraftConfig{MaxBytes: 100}, clock)
	u := domain.User{ID: 7}

	if d, err := s.Get(ctx, u, "card"); err != nil || d != nil {
		t.Fatalf("нет: %v %+v", err, d)
	}
	at, err := s.Save(ctx, u, "task-new", []byte(`{"title":"Сторис","blocks":[]}`))
	if err != nil || !at.Equal(clock.t) {
		t.Fatalf("%v %v", err, at)
	}
	if st.ttl[dkey(7, "task-new")] != 7*24*time.Hour {
		t.Errorf("срок: %v", st.ttl)
	}
	d, err := s.Get(ctx, u, "task-new")
	if err != nil || string(d.Data) != `{"title":"Сторис","blocks":[]}` || !d.SavedAt.Equal(clock.t) {
		t.Fatalf("%v %+v", err, d)
	}
	// чужой — не виден
	if d, _ := s.Get(ctx, domain.User{ID: 8}, "task-new"); d != nil {
		t.Error("чужой черновик")
	}
	// проверки: имя формы, размер, объект JSON
	for _, form := range []string{"", "task-0", "task-x", "../card", "card2", "task-" + strings.Repeat("1", 19)} {
		if _, err := s.Save(ctx, u, form, []byte(`{}`)); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("форма %q: %v", form, err)
		}
	}
	if _, err := s.Save(ctx, u, "card", []byte(`{"x":"`+strings.Repeat("я", 60)+`"}`)); !errors.Is(err, ErrDraftTooLarge) {
		t.Errorf("размер: %v", err)
	}
	for _, bad := range []string{`[]`, `null`, `"x"`, `{`} {
		if _, err := s.Save(ctx, u, "card", []byte(bad)); fields(t, err)["data"] == "" {
			t.Errorf("%s", bad)
		}
	}
	if err := s.Delete(ctx, u, "task-new"); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Get(ctx, u, "task-new"); d != nil {
		t.Error("удалён")
	}
	if err := s.Delete(ctx, u, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("форма: %v", err)
	}
	if _, err := s.Get(ctx, u, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("форма: %v", err)
	}
	if s.MaxBytes() != 100 || NewDrafts(st, DraftConfig{}, nil).MaxBytes() != 64<<10 {
		t.Error("пределы")
	}
	// ошибка хранилища — наружу
	st.fail = errors.New("down")
	if _, err := s.Get(ctx, u, "card"); err == nil {
		t.Error("ошибка чтения")
	}
}

// Redis недоступен — пишем и читаем PostgreSQL; вернулся — читаем Redis, а нет в нём — запасную копию.
func TestFallbackDrafts(t *testing.T) {
	ctx := context.Background()
	redis, pg := newMemDrafts(), newMemDrafts()
	f := FallbackDrafts{Primary: redis, Secondary: pg}
	d := domain.FormDraft{Form: "card", Data: []byte(`{"a":1}`)}

	redis.fail = errors.New("redis down")
	if err := f.PutDraft(ctx, 1, d, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, ok := pg.m[dkey(1, "card")]; !ok {
		t.Fatal("не в запасном")
	}
	if got, err := f.GetDraft(ctx, 1, "card"); err != nil || string(got.Data) != `{"a":1}` {
		t.Fatalf("чтение при сбое: %v", err)
	}
	redis.fail = nil
	if got, err := f.GetDraft(ctx, 1, "card"); err != nil || string(got.Data) != `{"a":1}` {
		t.Fatalf("нет в Redis — из запасного: %v", err)
	}
	d.Data = []byte(`{"a":2}`)
	_ = f.PutDraft(ctx, 1, d, time.Hour)
	if got, _ := f.GetDraft(ctx, 1, "card"); string(got.Data) != `{"a":2}` {
		t.Errorf("свежее — из Redis: %s", got.Data)
	}
	if err := f.DeleteDraft(ctx, 1, "card"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.GetDraft(ctx, 1, "card"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("удалён из обоих: %v", err)
	}
	// удаление при сбое Redis — из запасного всё равно
	redis.fail = errors.New("down")
	if err := f.DeleteDraft(ctx, 1, "card"); err != nil {
		t.Errorf("удаление при сбое: %v", err)
	}
	// очистка: отменённый контекст — выходит сразу
	c, cancel := context.WithCancel(ctx)
	cancel()
	NewDrafts(pg, DraftConfig{}, nil).RunCleanup(c, pg, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

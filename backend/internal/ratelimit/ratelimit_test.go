package ratelimit

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMemoryBucket(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(0, 0)
	m := NewMemory(Rate{RPS: 1, Burst: 3})
	m.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if ok, _, _ := m.Allow(ctx, "a"); !ok {
			t.Fatalf("запас: %d", i)
		}
	}
	ok, wait, err := m.Allow(ctx, "a")
	if ok || wait != time.Second || err != nil {
		t.Fatalf("4-й: %v %v %v", ok, wait, err)
	}
	if ok, _, _ := m.Allow(ctx, "b"); !ok {
		t.Fatal("другой ключ — свой бакет")
	}
	now = now.Add(1500 * time.Millisecond)
	if ok, _, _ := m.Allow(ctx, "a"); !ok {
		t.Fatal("токен восстановился")
	}
	// часы назад не дают лишних токенов
	now = now.Add(-time.Hour)
	if ok, _, _ := m.Allow(ctx, "a"); ok {
		t.Fatal("часы назад")
	}
	now = now.Add(2 * time.Hour)
	if n := m.Sweep(); n != 2 || m.Size() != 0 {
		t.Errorf("очистка: %d, осталось %d", n, m.Size())
	}
}

func TestRateNormalized(t *testing.T) {
	r := Rate{}.normalized()
	if r.RPS != 1 || r.Burst != 1 {
		t.Errorf("%+v", r)
	}
	if d := (Rate{RPS: 0.5, Burst: 10}).idleAfter(); d != 21*time.Second {
		t.Errorf("idle %v", d)
	}
}

func TestMemoryConcurrent(t *testing.T) {
	m := NewMemory(Rate{RPS: 0.001, Burst: 50})
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _, _ := m.Allow(context.Background(), "k"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 50 {
		t.Errorf("пропущено %d из запаса 50", allowed)
	}
}

func TestMemoryRunStops(t *testing.T) {
	m := NewMemory(Rate{RPS: 1, Burst: 1})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx, time.Millisecond); close(done) }()
	time.Sleep(5 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("очистка не остановилась")
	}
}

type brokenLimiter struct{ calls int }

func (b *brokenLimiter) Allow(context.Context, string) (bool, time.Duration, error) {
	b.calls++
	return false, 0, errors.New("redis: connection refused")
}

type okLimiter struct{}

func (okLimiter) Allow(context.Context, string) (bool, time.Duration, error) { return true, 0, nil }

func TestFallback(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	f := &Fallback{Primary: okLimiter{}, Secondary: NewMemory(Rate{RPS: 0.001, Burst: 1}), Log: log}
	for i := 0; i < 3; i++ {
		if ok, _, err := f.Allow(ctx, "k"); !ok || err != nil {
			t.Fatal("основной работает — его решение")
		}
	}
	br := &brokenLimiter{}
	f = &Fallback{Primary: br, Secondary: NewMemory(Rate{RPS: 0.001, Burst: 1}), Log: log}
	if ok, _, err := f.Allow(ctx, "k"); !ok || err != nil {
		t.Fatalf("запасной: %v %v", ok, err)
	}
	if ok, wait, _ := f.Allow(ctx, "k"); ok || wait <= 0 {
		t.Fatal("запасной тоже ограничивает")
	}
	if br.calls != 2 || strings.Count(buf.String(), "основное хранилище недоступно") != 1 {
		t.Errorf("вызовы %d, журнал: %s", br.calls, buf.String())
	}
}

// Интеграционный: настоящий Redis (TEST_REDIS_URL, в make test поднимается в Docker).
func TestRedis(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL не задан")
	}
	ctx := context.Background()
	rdb, err := Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer rdb.Close()
	prefix := "test:" + strings.ReplaceAll(t.Name(), "/", "_") + ":" + time.Now().Format("150405.000000") + ":"
	l := NewRedis(rdb, prefix, Rate{RPS: 1, Burst: 3})
	for i := 0; i < 3; i++ {
		if ok, _, err := l.Allow(ctx, "a"); !ok || err != nil {
			t.Fatalf("запас %d: %v", i, err)
		}
	}
	ok, wait, err := l.Allow(ctx, "a")
	if ok || err != nil || wait <= 0 || wait > time.Second {
		t.Fatalf("4-й: %v %v %v", ok, wait, err)
	}
	if ok, _, _ := l.Allow(ctx, "b"); !ok {
		t.Fatal("другой ключ — свой бакет")
	}
	if ttl := rdb.PTTL(ctx, prefix+"a").Val(); ttl <= 0 || ttl > 4*time.Second {
		t.Errorf("срок ключа: %v", ttl)
	}
	time.Sleep(1100 * time.Millisecond)
	if ok, _, _ := l.Allow(ctx, "a"); !ok {
		t.Fatal("токен восстановился")
	}
	// параллельно с двух «серверов» — общий бакет
	l2 := NewRedis(rdb, prefix, Rate{RPS: 0.001, Burst: 20})
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _, err := l2.Allow(ctx, "shared"); ok && err == nil {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 20 {
		t.Errorf("общий бакет: пропущено %d из 20", allowed)
	}
	// сбой соединения — ошибка (её подхватит Fallback)
	_ = rdb.Close()
	if _, _, err := l.Allow(ctx, "a"); err == nil {
		t.Error("закрытый клиент — ждали ошибку")
	}
	if _, err := Connect(ctx, "http://bad"); err == nil {
		t.Error("плохой адрес")
	}
}

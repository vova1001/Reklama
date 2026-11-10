package mail

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// flaky — падает первые fails раз, затем отправляет.
type flaky struct {
	mu    sync.Mutex
	fails int
	calls int
	sent  []Message
	block chan struct{}
}

func (f *flaky) Name() string { return "test" }
func (f *flaky) Send(ctx context.Context, m Message) error {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.fails {
		return errors.New("временный сбой")
	}
	f.sent = append(f.sent, m)
	return nil
}

func TestQueueRetriesAndDrains(t *testing.T) {
	cases := []struct {
		name      string
		fails     int
		retries   int
		wantSent  int
		wantCalls int
	}{
		{"сразу", 0, 3, 1, 1},
		{"со второй попытки", 1, 3, 1, 2},
		{"повторы исчерпаны", 10, 2, 0, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &flaky{fails: c.fails}
			q := NewQueue(f, Options{Workers: 2, Retries: c.retries, Backoff: time.Millisecond}, quiet)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { q.Run(ctx); close(done) }()
			if err := q.SendLoginCode(ctx, "a@mail.ru", "123456"); err != nil {
				t.Fatal(err)
			}
			cancel() // остановка: письмо в очереди всё равно досылается
			<-done
			if len(f.sent) != c.wantSent || f.calls != c.wantCalls {
				t.Fatalf("отправлено %d, попыток %d", len(f.sent), f.calls)
			}
			if err := q.Enqueue(Message{}); !errors.Is(err, ErrClosed) {
				t.Errorf("после остановки: %v", err)
			}
		})
	}
}

func TestQueueFullAndDrainTimeout(t *testing.T) {
	f := &flaky{block: make(chan struct{})}
	q := NewQueue(f, Options{Workers: 1, Size: 1, DrainTimeout: 20 * time.Millisecond}, quiet)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { q.Run(ctx); close(done) }()
	_ = q.Enqueue(Message{To: "1@a.ru"})
	time.Sleep(10 * time.Millisecond) // воркер взял первое и висит
	_ = q.Enqueue(Message{To: "2@a.ru"})
	if err := q.Enqueue(Message{To: "3@a.ru"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("переполнение: %v", err)
	}
	cancel()
	select {
	case <-done: // по истечении DrainTimeout отправка прерывается, воркеры выходят
	case <-time.After(2 * time.Second):
		t.Fatal("очередь не остановилась")
	}
}

func TestResend(t *testing.T) {
	var got map[string]any
	var auth string
	var status atomic.Int32
	status.Store(200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	defer srv.Close()
	r := Resend{APIKey: "re_test", From: "Охват <a@b.ru>", BaseURL: srv.URL}
	m := LoginCodeMessage("anna@mail.ru", "654321")
	if err := r.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer re_test" || got["from"] != "Охват <a@b.ru>" || !strings.Contains(got["html"].(string), "654321") ||
		got["to"].([]any)[0] != "anna@mail.ru" || !strings.Contains(got["subject"].(string), "654321") {
		t.Errorf("%v %v", auth, got)
	}
	status.Store(422)
	if err := r.Send(context.Background(), m); err == nil || !strings.Contains(err.Error(), "422") {
		t.Errorf("ошибка сервиса: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Send(ctx, m); err == nil {
		t.Error("отменённый контекст")
	}
}

func TestMIMEAndLog(t *testing.T) {
	b := string(mimeMessage("Охват <hello@ohvat.ru>", LoginCodeMessage("a@b.ru", "111222")))
	for _, want := range []string{"From: =?utf-8?q?", "<hello@ohvat.ru>", "To: a@b.ru", "Subject: =?utf-8?q?", "multipart/alternative", "text/plain; charset=utf-8", "text/html; charset=utf-8", "111222"} {
		if !strings.Contains(b, want) {
			t.Errorf("нет %q", want)
		}
	}
	if envelope("Охват <hello@ohvat.ru>") != "hello@ohvat.ru" || envelope(" a@b.ru ") != "a@b.ru" {
		t.Error("адрес конверта")
	}
	if maskEmail("anna@mail.ru") != "a***@mail.ru" || maskEmail("x") != "***" {
		t.Error("маска")
	}
	if err := (Log{Logger: quiet}).Send(context.Background(), Message{To: "a@b.ru"}); err != nil {
		t.Error(err)
	}
	if (SMTP{}).Name() != "smtp" || (Resend{}).Name() != "resend" || (Log{}).Name() != "log" {
		t.Error("имена")
	}
	// SMTP: недоступный сервер — ошибка в пределах дедлайна
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := (SMTP{Host: "127.0.0.1", Port: "1", From: "a@b.ru"}).Send(ctx, Message{To: "c@d.ru"}); err == nil {
		t.Error("SMTP: ждали ошибку подключения")
	}
}

// Ответ Resend 4xx (кроме 429) — без повторов; 5xx — повторы; 429 — повтор после Retry-After.
func TestQueueResendStatuses(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		retryAfter string
		wantCalls  int32
	}{
		{"403 validation_error", 403, "", 1},
		{"422", 422, "", 1},
		{"400", 400, "", 1},
		{"500 — повторы", 500, "", 3},
		{"503 — повторы", 503, "", 3},
		{"429 — повторы с Retry-After", 429, "1", 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if c.retryAfter != "" {
					w.Header().Set("Retry-After", c.retryAfter)
				}
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(`{"name":"validation_error","message":"нельзя"}`))
			}))
			defer srv.Close()
			q := NewQueue(Resend{APIKey: "re_test", From: "a@b.ru", BaseURL: srv.URL}, Options{Workers: 1, Retries: 2, Backoff: time.Millisecond}, quiet)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { q.Run(ctx); close(done) }()
			start := time.Now()
			if err := q.SendLoginCode(ctx, "a@mail.ru", "123456"); err != nil {
				t.Fatal(err)
			}
			cancel()
			<-done
			if got := calls.Load(); got != c.wantCalls {
				t.Fatalf("попыток %d, нужно %d", got, c.wantCalls)
			}
			if c.retryAfter != "" && time.Since(start) < 2*time.Second {
				t.Errorf("Retry-After не учтён: %v", time.Since(start))
			}
		})
	}
}

func TestSendErrorPermanentAndRetryAfter(t *testing.T) {
	for _, c := range []struct {
		e    SendError
		want bool
	}{
		{SendError{Via: "resend", Status: 403}, true}, {SendError{Via: "resend", Status: 429}, false},
		{SendError{Via: "resend", Status: 502}, false}, {SendError{Via: "smtp", Status: 550}, true}, {SendError{Via: "smtp", Status: 451}, false},
	} {
		if c.e.Permanent() != c.want {
			t.Errorf("%+v: %v", c.e, !c.want)
		}
	}
	if retryAfter("7") != 7*time.Second || retryAfter("") != 0 || retryAfter("x") != 0 {
		t.Error("Retry-After в секундах")
	}
	if d := retryAfter(time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)); d < 50*time.Second || d > time.Minute {
		t.Errorf("Retry-After датой: %v", d)
	}
}

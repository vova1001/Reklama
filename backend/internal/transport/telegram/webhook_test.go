package telegram

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
	"testing"
	"time"

	"ohvat/backend/internal/service"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type fakeBot struct {
	mu   sync.Mutex
	got  []service.BotUpdate
	err  error
	hold chan struct{}
}

func (f *fakeBot) Handle(ctx context.Context, u service.BotUpdate) error {
	if f.hold != nil {
		<-f.hold
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, u)
	return f.err
}

func (f *fakeBot) n() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

const secret = "test-secret_1"

func post(h http.Handler, body, sec string) int {
	r := httptest.NewRequest(http.MethodPost, "/api/telegram/webhook", strings.NewReader(body))
	if sec != "" {
		r.Header.Set(SecretHeader, sec)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func start(id int64, text string) string {
	b, _ := json.Marshal(map[string]any{"update_id": id, "message": map[string]any{
		"message_id": 1, "from": map[string]any{"id": 777, "first_name": "Анна", "username": "anna"},
		"chat": map[string]any{"id": 777, "type": "private"}, "text": text}})
	return string(b)
}

func TestWebhookSecretAndMethods(t *testing.T) {
	bot := &fakeBot{}
	h := New(bot, Options{Secret: secret, Log: quiet})
	if c := post(h, start(1, "/start"), ""); c != 401 {
		t.Error("без секрета", c)
	}
	if c := post(h, start(1, "/start"), "wrong"); c != 401 {
		t.Error("чужой секрет", c)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(SecretHeader, secret)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Error("GET", w.Code)
	}
	if bot.n() != 0 {
		t.Fatal("чужие запросы дошли до бота")
	}
	if c := post(h, start(1, "/start login_x"), secret); c != 200 || bot.n() != 1 {
		t.Fatal(c)
	}
	u := bot.got[0]
	if u.ChatID != 777 || !u.Private || u.From.ID != 777 || u.From.FirstName != "Анна" || u.Text != "/start login_x" {
		t.Errorf("%+v", u)
	}
	// без секрета в настройках — вебхука нет
	if c := post(New(bot, Options{Log: quiet}), start(2, "/start"), ""); c != 404 {
		t.Error(c)
	}
}

func TestWebhookUpdatesKinds(t *testing.T) {
	bot := &fakeBot{}
	h := New(bot, Options{Secret: secret, Log: quiet, UserBurst: 100})
	cases := []struct {
		body string
		want func(u service.BotUpdate) bool
	}{
		{`{"update_id":10,"callback_query":{"id":"cb","from":{"id":5},"data":"ok:abc","message":{"message_id":9,"chat":{"id":5,"type":"private"}}}}`,
			func(u service.BotUpdate) bool {
				return u.CallbackID == "cb" && u.CallbackData == "ok:abc" && u.MessageID == 9
			}},
		{`{"update_id":11,"my_chat_member":{"chat":{"id":5,"type":"private"},"from":{"id":5},"new_chat_member":{"status":"kicked"}}}`,
			func(u service.BotUpdate) bool { return u.Blocked && u.ChatID == 5 }},
		{`{"update_id":12,"my_chat_member":{"chat":{"id":5,"type":"private"},"from":{"id":5},"new_chat_member":{"status":"member"}}}`,
			func(u service.BotUpdate) bool { return u.Unblocked }},
		{`{"update_id":13,"message":{"from":{"id":5,"first_name":"` + strings.Repeat("я", 100) + `"},"chat":{"id":-1,"type":"group"},"text":"/start"}}`,
			func(u service.BotUpdate) bool { return !u.Private && len([]rune(u.From.FirstName)) == 64 }},
	}
	for i, c := range cases {
		if code := post(h, c.body, secret); code != 200 || bot.n() != i+1 || !c.want(bot.got[i]) {
			t.Errorf("%d: %d %+v", i, code, bot.got)
		}
	}
	// не нужное боту — 200 без вызова
	for _, body := range []string{
		`not json`, `{}`, `{"update_id":20,"edited_message":{}}`,
		`{"update_id":21,"message":{"from":{"id":5},"chat":{"id":5,"type":"private"}}}`,
		`{"update_id":22,"message":{"from":{"id":5},"chat":{"id":5,"type":"private"},"text":"` + strings.Repeat("a", 5000) + `"}}`,
		`{"update_id":23,"callback_query":{"id":"x","from":{"id":5},"data":"x"}}`,
		`{"update_id":24,"callback_query":{"id":"x","from":{"id":5},"data":"` + strings.Repeat("a", 65) + `","message":{"chat":{"id":5,"type":"private"}}}}`,
		`{"update_id":25,"my_chat_member":{"chat":{"id":5,"type":"private"},"new_chat_member":{"status":"administrator"}}}`,
	} {
		if code := post(h, body, secret); code != 200 {
			t.Error(code)
		}
	}
	if bot.n() != len(cases) {
		t.Errorf("лишние вызовы: %d", bot.n())
	}
	// слишком большое тело
	h2 := New(bot, Options{Secret: secret, Log: quiet, MaxBody: 64})
	if code := post(h2, start(30, strings.Repeat("a", 200)), secret); code != 200 || bot.n() != len(cases) {
		t.Error(code)
	}
}

func TestWebhookDedupRetryFloodAndOverload(t *testing.T) {
	t.Run("повтор от Telegram обрабатывается один раз", func(t *testing.T) {
		bot := &fakeBot{}
		h := New(bot, Options{Secret: secret, Log: quiet})
		post(h, start(1, "/help"), secret)
		post(h, start(1, "/help"), secret)
		if bot.n() != 1 {
			t.Error(bot.n())
		}
	})
	t.Run("сбой бота — 500, повтор обрабатывается заново", func(t *testing.T) {
		bot := &fakeBot{err: errors.New("db down")}
		h := New(bot, Options{Secret: secret, Log: quiet})
		if c := post(h, start(1, "/help"), secret); c != 500 {
			t.Fatal(c)
		}
		bot.err = nil
		if c := post(h, start(1, "/help"), secret); c != 200 || bot.n() != 2 {
			t.Fatal(c, bot.n())
		}
	})
	t.Run("флуд от одного человека отсекается", func(t *testing.T) {
		bot := &fakeBot{}
		h := New(bot, Options{Secret: secret, Log: quiet, UserRPS: 0.001, UserBurst: 3})
		for i := int64(1); i <= 10; i++ {
			post(h, start(i, "/help"), secret)
		}
		if bot.n() != 3 {
			t.Error(bot.n())
		}
		h.flood.now = func() time.Time { return time.Now().Add(time.Hour) }
		h.Sweep()
		if len(h.flood.b) != 0 {
			t.Error("очистка")
		}
	})
	t.Run("перегрузка — 503", func(t *testing.T) {
		bot := &fakeBot{hold: make(chan struct{})}
		h := New(bot, Options{Secret: secret, Log: quiet, MaxConcurrent: 1})
		done := make(chan int)
		go func() { done <- post(h, start(1, "/help"), secret) }()
		for len(h.sem) == 0 {
			time.Sleep(time.Millisecond)
		}
		if c := post(h, start(2, "/help"), secret); c != 503 {
			t.Error(c)
		}
		close(bot.hold)
		if c := <-done; c != 200 {
			t.Error(c)
		}
	})
	t.Run("таймаут обработки", func(t *testing.T) {
		slow := botFunc(func(ctx context.Context, _ service.BotUpdate) error { <-ctx.Done(); return ctx.Err() })
		h := New(slow, Options{Secret: secret, Log: quiet, Timeout: 20 * time.Millisecond})
		if c := post(h, start(1, "/help"), secret); c != 500 {
			t.Error(c)
		}
	})
}

type botFunc func(ctx context.Context, u service.BotUpdate) error

func (f botFunc) Handle(ctx context.Context, u service.BotUpdate) error { return f(ctx, u) }

func TestRecentIDsRing(t *testing.T) {
	r := newRecentIDs(3)
	for i := int64(1); i <= 4; i++ {
		if !r.add(i) {
			t.Fatal(i)
		}
	}
	if !r.add(1) || r.add(4) {
		t.Error("кольцо: старый id вытеснен, свежий помнится")
	}
}

type fakeUpdates struct {
	mu    sync.Mutex
	batch [][]json.RawMessage
	errs  int
	seen  []int64
}

func (f *fakeUpdates) GetUpdates(ctx context.Context, offset int64, _ int) ([]json.RawMessage, error) {
	f.mu.Lock()
	f.seen = append(f.seen, offset)
	if f.errs > 0 {
		f.errs--
		f.mu.Unlock()
		return nil, errors.New("сеть")
	}
	if len(f.batch) == 0 {
		f.mu.Unlock()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	b := f.batch[0]
	f.batch = f.batch[1:]
	f.mu.Unlock()
	return b, nil
}

func TestPoller(t *testing.T) {
	bot := &fakeBot{}
	src := &fakeUpdates{errs: 1, batch: [][]json.RawMessage{
		{json.RawMessage(start(5, "/start")), json.RawMessage(`garbage`), json.RawMessage(start(6, "/help"))},
		{json.RawMessage(start(7, "/stop"))},
	}}
	h := New(bot, Options{Secret: secret, Log: quiet})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { NewPoller(src, h, quiet).Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for bot.n() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	src.mu.Lock()
	defer src.mu.Unlock()
	if bot.n() != 3 || src.seen[len(src.seen)-1] != 8 {
		t.Errorf("обработано %d, offset %v", bot.n(), src.seen)
	}
}

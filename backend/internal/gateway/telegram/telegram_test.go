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
	"sync/atomic"
	"testing"
	"time"

	"ohvat/backend/internal/service"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

const testToken = "123456:SECRET-token-for-tests"

// fakeAPI — подставной Bot API: запоминает вызовы, ответ — по функции.
type fakeAPI struct {
	mu    sync.Mutex
	calls []call
	reply func(method string, body map[string]any) (int, string)
}

type call struct {
	method string
	body   map[string]any
}

func (f *fakeAPI) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/bot" + testToken + "/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":404,"description":"Not Found"}`))
			return
		}
		m := strings.TrimPrefix(r.URL.Path, prefix)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.calls = append(f.calls, call{m, body})
		f.mu.Unlock()
		code, out := 200, `{"ok":true,"result":true}`
		if f.reply != nil {
			code, out = f.reply(m, body)
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(out))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeAPI) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.method
	}
	return out
}

func TestClientMethodsAndErrors(t *testing.T) {
	ctx := context.Background()
	f := &fakeAPI{reply: func(m string, b map[string]any) (int, string) {
		switch m {
		case "getMe":
			return 200, `{"ok":true,"result":{"id":1,"username":"reklama_auth_bot","is_bot":true}}`
		case "getWebhookInfo":
			return 200, `{"ok":true,"result":{"url":"https://a.ru/api/telegram/webhook","pending_update_count":2}}`
		case "sendMessage":
			if b["chat_id"].(float64) == 403 {
				return 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
			}
			if b["chat_id"].(float64) == 429 {
				return 429, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`
			}
		case "getUpdates":
			return 200, `{"ok":true,"result":[{"update_id":5},{"update_id":6}]}`
		}
		return 200, `{"ok":true,"result":true}`
	}}
	c := New(testToken, Options{BaseURL: f.server(t).URL})
	if me, err := c.GetMe(ctx); err != nil || me.Username != "reklama_auth_bot" {
		t.Fatal(me, err)
	}
	if w, err := c.GetWebhookInfo(ctx); err != nil || w.PendingUpdateCount != 2 {
		t.Fatal(w, err)
	}
	if err := c.SendMessage(ctx, 1, "<b>hi</b>", &Markup{InlineKeyboard: [][]InlineButton{{{Text: "a", CallbackData: "ok:x"}}}}); err != nil {
		t.Fatal(err)
	}
	b := f.calls[len(f.calls)-1].body
	if b["parse_mode"] != "HTML" || b["disable_web_page_preview"] != true || b["reply_markup"] == nil {
		t.Errorf("%v", b)
	}
	if err := c.EditMessageText(ctx, 1, 2, "x", nil); err != nil {
		t.Fatal(err)
	}
	if b := f.calls[len(f.calls)-1].body; b["message_id"].(float64) != 2 || b["reply_markup"] == nil {
		t.Errorf("правка без кнопок должна убрать клавиатуру: %v", b)
	}
	var ae *APIError
	if err := c.SendMessage(ctx, 403, "x", nil); !errors.As(err, &ae) || ae.Code != 403 || ae.Temporary() {
		t.Error(err)
	}
	if err := c.SendMessage(ctx, 429, "x", nil); !errors.As(err, &ae) || ae.RetryAfter != 7*time.Second || !ae.Temporary() {
		t.Error(err)
	}
	if ups, err := c.GetUpdates(ctx, 5, 1); err != nil || len(ups) != 2 {
		t.Error(ups, err)
	}
	for _, fn := range []func() error{
		func() error { return c.AnswerCallbackQuery(ctx, "id", "ok") },
		func() error { return c.SetMyCommands(ctx, Commands) },
		func() error { return c.DeleteWebhook(ctx) },
		func() error { return c.SetWebhook(ctx, WebhookParams{URL: "https://a.ru"}) },
	} {
		if err := fn(); err != nil {
			t.Error(err)
		}
	}
	// неверный токен — 404
	bad := New("1:bad", Options{BaseURL: c.base})
	if _, err := bad.GetMe(ctx); !errors.As(err, &ae) || ae.Code != 404 {
		t.Error(err)
	}
}

// Токен не должен попадать в текст ошибок (они уходят в журнал).
func TestClientNeverLeaksToken(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/slow"):
			time.Sleep(200 * time.Millisecond)
		case strings.HasSuffix(r.URL.Path, "/echo"):
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"bad path ` + r.URL.Path + `"}`))
		default:
			_, _ = w.Write([]byte(`not json`))
		}
	}))
	defer srv.Close()
	c := New(testToken, Options{BaseURL: srv.URL, Timeout: 50 * time.Millisecond})
	closed := New(testToken, Options{BaseURL: "http://127.0.0.1:1"})
	for name, err := range map[string]error{
		"таймаут":   c.Call(ctx, "slow", nil, nil),
		"эхо":       c.Call(ctx, "echo", nil, nil),
		"не JSON":   c.Call(ctx, "x", nil, nil),
		"нет связи": closed.Call(ctx, "getMe", nil, nil),
	} {
		if err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "SECRET") {
			t.Errorf("%s: %v", name, err)
		}
	}
	var ne *NetError
	if err := c.Call(ctx, "slow", nil, nil); !errors.As(err, &ne) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("таймаут: %v", err)
	}
}

func TestSetup(t *testing.T) {
	ctx := context.Background()
	f := &fakeAPI{reply: func(m string, _ map[string]any) (int, string) {
		if m == "getMe" {
			return 200, `{"ok":true,"result":{"id":1,"username":"Reklama_Auth_Bot","is_bot":true}}`
		}
		if m == "getWebhookInfo" {
			return 200, `{"ok":true,"result":{"url":"https://a.ru/api/telegram/webhook"}}`
		}
		return 200, `{"ok":true,"result":true}`
	}}
	c := New(testToken, Options{BaseURL: f.server(t).URL})
	info, err := c.Setup(ctx, SetupOptions{BotName: "reklama_auth_bot", WebhookURL: "https://a.ru/api/telegram/webhook", WebhookSecret: "sec"})
	if err != nil || info.URL == "" {
		t.Fatal(err)
	}
	if got := strings.Join(f.methods(), ","); got != "getMe,setMyCommands,setWebhook,getWebhookInfo" {
		t.Error(got)
	}
	wh := f.calls[2].body
	if wh["secret_token"] != "sec" || wh["max_connections"].(float64) != 20 || len(wh["allowed_updates"].([]any)) != 3 {
		t.Errorf("%v", wh)
	}
	f.calls = nil
	if _, err := c.Setup(ctx, SetupOptions{Polling: true}); err != nil || !strings.Contains(strings.Join(f.methods(), ","), "deleteWebhook") {
		t.Error(err, f.methods())
	}
	if _, err := c.Setup(ctx, SetupOptions{BotName: "other_bot"}); !errors.Is(err, ErrBotName) {
		t.Error(err)
	}
	// неверное имя — без повторов, сайт продолжает работать
	done := make(chan error, 1)
	go func() { done <- c.SetupWithRetry(ctx, SetupOptions{BotName: "other_bot"}, quiet) }()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("повторяет неисправимое")
	}
	// сбой сети — повторяет до отмены
	down := New(testToken, Options{BaseURL: "http://127.0.0.1:1"})
	cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := down.SetupWithRetry(cctx, SetupOptions{}, quiet); err != nil {
		t.Error(err)
	}
}

// apiFunc — подставной API для очереди.
type apiFunc struct {
	mu    sync.Mutex
	log   []string
	times []time.Time
	fn    func(kind string, chat int64, n int) error
	n     map[int64]int
}

func (a *apiFunc) do(kind string, chat int64) error {
	a.mu.Lock()
	if a.n == nil {
		a.n = map[int64]int{}
	}
	a.n[chat]++
	n := a.n[chat]
	a.log = append(a.log, kind)
	a.times = append(a.times, time.Now())
	a.mu.Unlock()
	if a.fn != nil {
		return a.fn(kind, chat, n)
	}
	return nil
}

func (a *apiFunc) SendMessage(_ context.Context, chat int64, text string, _ *Markup) error {
	return a.do("send:"+text, chat)
}
func (a *apiFunc) EditMessageText(_ context.Context, chat, _ int64, text string, _ *Markup) error {
	return a.do("edit:"+text, chat)
}
func (a *apiFunc) AnswerCallbackQuery(_ context.Context, id, _ string) error { return a.do("answer:"+id, 0) }

func (a *apiFunc) calls() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.log...)
}

func runOutbox(t *testing.T, api API, opt OutboxOptions) (*Outbox, func()) {
	t.Helper()
	o := NewOutbox(api, opt, quiet)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { o.Run(ctx); close(done) }()
	return o, func() { cancel(); <-done }
}

func TestOutboxOrderRateAndRetries(t *testing.T) {
	ctx := context.Background()
	t.Run("порядок в чате и пауза между сообщениями", func(t *testing.T) {
		a := &apiFunc{}
		o, stop := runOutbox(t, a, OutboxOptions{Workers: 3, PerChat: 30 * time.Millisecond, RPS: 1000})
		for i := 0; i < 3; i++ {
			_ = o.Send(ctx, service.BotMessage{ChatID: 7, Text: string(rune('a' + i))})
		}
		_ = o.Send(ctx, service.BotMessage{ChatID: 7, EditMessageID: 1, Text: "e"})
		_ = o.AnswerCallback(ctx, "cb", "ok")
		stop()
		var chat7 []string
		for _, c := range a.calls() {
			if !strings.HasPrefix(c, "answer") {
				chat7 = append(chat7, c)
			}
		}
		if strings.Join(chat7, ",") != "send:a,send:b,send:c,edit:e" {
			t.Fatal(chat7)
		}
		var prev time.Time
		for i, c := range a.calls() {
			if strings.HasPrefix(c, "answer") {
				continue
			}
			if !prev.IsZero() && a.times[i].Sub(prev) < 25*time.Millisecond {
				t.Errorf("пауза в чате не соблюдена: %v", a.times[i].Sub(prev))
			}
			prev = a.times[i]
		}
	})
	t.Run("общий лимит скорости", func(t *testing.T) {
		a := &apiFunc{}
		o, stop := runOutbox(t, a, OutboxOptions{Workers: 4, RPS: 100, PerChat: -1})
		start := time.Now()
		for i := int64(0); i < 20; i++ {
			_ = o.Send(ctx, service.BotMessage{ChatID: i, Text: "x"})
		}
		stop()
		if el := time.Since(start); el < 180*time.Millisecond || len(a.calls()) != 20 {
			t.Errorf("20 сообщений при 100/с за %v (%d)", el, len(a.calls()))
		}
	})
	t.Run("ошибки", func(t *testing.T) {
		var blocked atomic.Int64
		a := &apiFunc{fn: func(_ string, chat int64, n int) error {
			switch chat {
			case 1: // сеть — дважды, потом успех
				if n <= 2 {
					return &NetError{Method: "sendMessage", Err: errors.New("reset")}
				}
			case 2:
				return &APIError{Code: 403, Description: "Forbidden: bot was blocked by the user"}
			case 3:
				return &APIError{Code: 400, Description: "Bad Request: chat not found"}
			case 4:
				if n == 1 {
					return &APIError{Code: 429, RetryAfter: 50 * time.Millisecond}
				}
			case 5:
				return &APIError{Code: 429, RetryAfter: time.Hour}
			case 6:
				return &APIError{Code: 400, Description: "Bad Request: message is not modified"}
			case 8:
				return &APIError{Code: 502}
			}
			return nil
		}}
		_, stop := func() (*Outbox, func()) {
			o, stop := runOutbox(t, a, OutboxOptions{Workers: 2, RPS: 1000, PerChat: -1, Retries: 3, Backoff: time.Millisecond,
				MaxRetryAfter: time.Second, OnBlocked: func(id int64) { blocked.Store(id) }})
			for chat := int64(1); chat <= 8; chat++ {
				_ = o.Send(ctx, service.BotMessage{ChatID: chat, Text: "x"})
			}
			return o, stop
		}()
		stop()
		a.mu.Lock()
		defer a.mu.Unlock()
		want := map[int64]int{1: 3, 2: 1, 3: 1, 4: 2, 5: 1, 6: 1, 7: 1, 8: 4}
		for chat, n := range want {
			if a.n[chat] != n {
				t.Errorf("чат %d: попыток %d, нужно %d", chat, a.n[chat], n)
			}
		}
		if blocked.Load() != 2 {
			t.Error("OnBlocked")
		}
	})
	t.Run("переполнение и остановка", func(t *testing.T) {
		hold := make(chan struct{})
		a := &apiFunc{fn: func(string, int64, int) error { <-hold; return nil }}
		o := NewOutbox(a, OutboxOptions{Workers: 1, Size: 1, DrainTimeout: 30 * time.Millisecond, PerChat: -1}, quiet)
		ctx2, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { o.Run(ctx2); close(done) }()
		_ = o.Send(ctx, service.BotMessage{ChatID: 1})
		time.Sleep(20 * time.Millisecond)
		_ = o.Send(ctx, service.BotMessage{ChatID: 1})
		if err := o.Send(ctx, service.BotMessage{ChatID: 1}); !errors.Is(err, ErrQueueFull) {
			t.Fatal(err)
		}
		cancel()
		time.Sleep(50 * time.Millisecond)
		close(hold)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("не остановилась")
		}
		if err := o.Send(ctx, service.BotMessage{ChatID: 1}); !errors.Is(err, ErrClosed) {
			t.Error(err)
		}
	})
}

func TestMarkupOf(t *testing.T) {
	if markupOf(nil) != nil {
		t.Error("без кнопок")
	}
	m := markupOf([][]service.BotButton{{{Text: "a", URL: "https://x"}, {Text: "b", Data: "ok:1"}}})
	if len(m.InlineKeyboard[0]) != 2 || m.InlineKeyboard[0][1].CallbackData != "ok:1" || m.InlineKeyboard[0][0].URL != "https://x" {
		t.Errorf("%+v", m)
	}
}

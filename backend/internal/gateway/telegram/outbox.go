package telegram

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"
	"strings"
	"sync"
	"time"

	"ohvat/backend/internal/service"
)

// API — методы Bot API, которые нужны очереди (Client; в тестах — подставной).
type API interface {
	SendMessage(ctx context.Context, chatID int64, text string, m *Markup) error
	EditMessageText(ctx context.Context, chatID, messageID int64, text string, m *Markup) error
	AnswerCallbackQuery(ctx context.Context, id, text string) error
}

// Ошибки постановки в очередь.
var (
	ErrQueueFull = errors.New("очередь бота переполнена")
	ErrClosed    = errors.New("очередь бота остановлена")
)

// OutboxOptions — лимиты и повторы.
type OutboxOptions struct {
	Workers       int                // воркеров (чат всегда у одного воркера — порядок сообщений сохраняется), 4
	Size          int                // ёмкость очереди каждого воркера, 256
	RPS           float64            // сообщений в секунду на всего бота (лимит Telegram ~30), 25
	PerChat       time.Duration      // пауза между сообщениями в один чат (лимит Telegram ~1 в секунду), 1 с
	Retries       int                // повторов после первой попытки (сеть, 5xx, 429), 3
	Backoff       time.Duration      // пауза перед первым повтором (дальше ×2), 1 с
	MaxRetryAfter time.Duration      // дольше этого 429 не ждём — сообщение отбрасывается, 60 с
	DrainTimeout  time.Duration      // сколько досылать при остановке, 10 с
	OnBlocked     func(chatID int64) // Telegram ответил 403: пользователь остановил бота
}

func (o *OutboxOptions) defaults() {
	if o.Workers <= 0 {
		o.Workers = 4
	}
	if o.Size <= 0 {
		o.Size = 256
	}
	if o.RPS <= 0 {
		o.RPS = 25
	}
	if o.PerChat < 0 {
		o.PerChat = 0
	} else if o.PerChat == 0 {
		o.PerChat = time.Second
	}
	if o.Retries < 0 {
		o.Retries = 0
	} else if o.Retries == 0 {
		o.Retries = 3
	}
	if o.Backoff <= 0 {
		o.Backoff = time.Second
	}
	if o.MaxRetryAfter <= 0 {
		o.MaxRetryAfter = time.Minute
	}
	if o.DrainTimeout <= 0 {
		o.DrainTimeout = 10 * time.Second
	}
}

type jobKind int

const (
	jobSend jobKind = iota
	jobEdit
	jobAnswer
)

type job struct {
	kind   jobKind
	chatID int64
	msgID  int64
	text   string
	markup *Markup
	cbID   string
}

// Outbox — очередь сообщений бота: общий лимит скорости, пауза между сообщениями в чат, повторы с растущей
// паузой, 429 — ждём retry_after, 403 — чат остановлен (OnBlocked). Реализует service.BotMessenger.
type Outbox struct {
	api    API
	opt    OutboxOptions
	log    *slog.Logger
	shards []chan job
	lim    *pacer
	mu     sync.RWMutex
	closed bool
}

// NewOutbox создаёт очередь; воркеры запускаются в Run.
func NewOutbox(api API, opt OutboxOptions, log *slog.Logger) *Outbox {
	opt.defaults()
	if log == nil {
		log = slog.Default()
	}
	o := &Outbox{api: api, opt: opt, log: log, lim: newPacer(opt.RPS)}
	o.shards = make([]chan job, opt.Workers)
	for i := range o.shards {
		o.shards[i] = make(chan job, opt.Size)
	}
	return o
}

// Send — сообщение или правка сообщения (service.BotMessenger).
func (o *Outbox) Send(_ context.Context, m service.BotMessage) error {
	j := job{kind: jobSend, chatID: m.ChatID, msgID: m.EditMessageID, text: m.Text, markup: markupOf(m.Buttons)}
	if m.EditMessageID != 0 {
		j.kind = jobEdit
	}
	return o.enqueue(j, uint64(m.ChatID))
}

// AnswerCallback — ответ на нажатие кнопки (service.BotMessenger).
func (o *Outbox) AnswerCallback(_ context.Context, id, text string) error {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return o.enqueue(job{kind: jobAnswer, cbID: id, text: text}, h.Sum64())
}

func (o *Outbox) enqueue(j job, key uint64) error {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if o.closed {
		return ErrClosed
	}
	select {
	case o.shards[key%uint64(len(o.shards))] <- j:
		return nil
	default:
		return ErrQueueFull
	}
}

// Len — сколько ждёт отправки.
func (o *Outbox) Len() int {
	n := 0
	for _, s := range o.shards {
		n += len(s)
	}
	return n
}

// Run запускает воркеров до отмены ctx; затем досылает очередь (не дольше DrainTimeout) и возвращается,
// когда все воркеры завершились.
func (o *Outbox) Run(ctx context.Context) {
	sendCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	var wg sync.WaitGroup
	for _, ch := range o.shards {
		wg.Add(1)
		go func(ch <-chan job) {
			defer wg.Done()
			last := map[int64]time.Time{} // чат → когда писали (только этот воркер)
			for j := range ch {
				o.deliver(sendCtx, j, last)
			}
		}(ch)
	}
	<-ctx.Done()
	o.mu.Lock()
	o.closed = true
	for _, ch := range o.shards {
		close(ch)
	}
	o.mu.Unlock()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(o.opt.DrainTimeout):
		o.log.Warn("бот: остановка — не все сообщения ушли", "left", o.Len())
		cancel()
		<-done
	}
}

func (o *Outbox) deliver(ctx context.Context, j job, last map[int64]time.Time) {
	if j.kind != jobAnswer && o.opt.PerChat > 0 {
		if t, ok := last[j.chatID]; ok {
			if !sleep(ctx, time.Until(t.Add(o.opt.PerChat))) {
				return
			}
		}
		defer func() {
			last[j.chatID] = time.Now()
			if len(last) > 4096 { // не копим старые чаты
				for k, t := range last {
					if time.Since(t) > o.opt.PerChat {
						delete(last, k)
					}
				}
			}
		}()
	}
	wait := o.opt.Backoff
	for attempt := 0; ; attempt++ {
		if err := o.lim.wait(ctx); err != nil {
			return
		}
		err := o.call(ctx, j)
		if err == nil {
			return
		}
		var ae *APIError
		isAPI := errors.As(err, &ae)
		switch {
		case isAPI && ae.Code == 403:
			o.log.Warn("бот: чат недоступен (бот остановлен)", "chat", j.chatID)
			if o.opt.OnBlocked != nil && j.chatID != 0 {
				o.opt.OnBlocked(j.chatID)
			}
			return
		case isAPI && ae.Code == 400 && strings.Contains(ae.Description, "message is not modified"):
			return
		case isAPI && !ae.Temporary():
			o.log.Error("бот: сообщение отклонено", "kind", j.kind, "chat", j.chatID, "err", err)
			return
		}
		if attempt >= o.opt.Retries || ctx.Err() != nil {
			o.log.Error("бот: сообщение не отправлено", "kind", j.kind, "chat", j.chatID, "attempts", attempt+1, "err", err)
			return
		}
		pause := wait
		if isAPI && ae.RetryAfter > 0 {
			if ae.RetryAfter > o.opt.MaxRetryAfter {
				o.log.Error("бот: лимит Telegram — сообщение отброшено", "chat", j.chatID, "retry_after", ae.RetryAfter)
				return
			}
			pause = ae.RetryAfter
			o.lim.pause(pause) // 429 — притормаживаем всю отправку
		}
		o.log.Warn("бот: повтор отправки", "kind", j.kind, "chat", j.chatID, "attempt", attempt+1, "err", err)
		if !sleep(ctx, pause) {
			return
		}
		wait *= 2
	}
}

func (o *Outbox) call(ctx context.Context, j job) error {
	switch j.kind {
	case jobEdit:
		return o.api.EditMessageText(ctx, j.chatID, j.msgID, j.text, j.markup)
	case jobAnswer:
		return o.api.AnswerCallbackQuery(ctx, j.cbID, j.text)
	default:
		return o.api.SendMessage(ctx, j.chatID, j.text, j.markup)
	}
}

func markupOf(rows [][]service.BotButton) *Markup {
	if len(rows) == 0 {
		return nil
	}
	m := &Markup{InlineKeyboard: make([][]InlineButton, 0, len(rows))}
	for _, r := range rows {
		row := make([]InlineButton, 0, len(r))
		for _, b := range r {
			row = append(row, InlineButton{Text: b.Text, URL: b.URL, CallbackData: b.Data})
		}
		m.InlineKeyboard = append(m.InlineKeyboard, row)
	}
	return m
}

// sleep ждёт d или отмены ctx; false — отменили.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// pacer — равномерный общий лимит: не чаще rps вызовов в секунду на все воркеры; pause сдвигает всё.
type pacer struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newPacer(rps float64) *pacer {
	return &pacer{interval: time.Duration(float64(time.Second) / rps)}
}

func (p *pacer) wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	at := p.next
	if at.Before(now) {
		at = now
	}
	p.next = at.Add(p.interval)
	p.mu.Unlock()
	if !sleep(ctx, time.Until(at)) {
		return ctx.Err()
	}
	return nil
}

func (p *pacer) pause(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if until := time.Now().Add(d); until.After(p.next) {
		p.next = until
	}
}

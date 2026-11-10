// Package mail — отправка писем: очередь с пулом воркеров и повторами поверх способа отправки
// (Resend по HTTPS, SMTP, журнал в режиме разработки). Реализует service.Mailer.
package mail

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Message — письмо.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Sender — способ отправки одного письма.
type Sender interface {
	Name() string
	Send(ctx context.Context, m Message) error
}

// Ошибки постановки в очередь.
var (
	ErrQueueFull = errors.New("очередь писем переполнена")
	ErrClosed    = errors.New("очередь писем остановлена")
)

// Options — настройки очереди.
type Options struct {
	Workers      int           // воркеров, 2
	Size         int           // ёмкость очереди, 256
	Retries      int           // повторов после первой попытки, 3
	Backoff      time.Duration // пауза перед первым повтором (дальше ×2), 2 с
	SendTimeout  time.Duration // дедлайн одной попытки, 15 с
	DrainTimeout time.Duration // сколько дожидаться неотправленных писем при остановке, 20 с
}

// Queue — очередь писем с пулом воркеров.
type Queue struct {
	sender Sender
	opt    Options
	log    *slog.Logger
	ch     chan Message
	mu     sync.RWMutex
	closed bool
}

// NewQueue создаёт очередь; воркеры запускаются в Run.
func NewQueue(s Sender, opt Options, log *slog.Logger) *Queue {
	if opt.Workers <= 0 {
		opt.Workers = 2
	}
	if opt.Size <= 0 {
		opt.Size = 256
	}
	if opt.Retries < 0 {
		opt.Retries = 0
	} else if opt.Retries == 0 {
		opt.Retries = 3
	}
	if opt.Backoff <= 0 {
		opt.Backoff = 2 * time.Second
	}
	if opt.SendTimeout <= 0 {
		opt.SendTimeout = 15 * time.Second
	}
	if opt.DrainTimeout <= 0 {
		opt.DrainTimeout = 20 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	return &Queue{sender: s, opt: opt, log: log, ch: make(chan Message, opt.Size)}
}

// Enqueue ставит письмо в очередь, не дожидаясь отправки.
func (q *Queue) Enqueue(m Message) error {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed {
		return ErrClosed
	}
	select {
	case q.ch <- m:
		return nil
	default:
		return ErrQueueFull
	}
}

// SendLoginCode — письмо с кодом входа (service.Mailer).
func (q *Queue) SendLoginCode(_ context.Context, to, code string) error {
	return q.Enqueue(LoginCodeMessage(to, code))
}

// Run запускает воркеров и ждёт отмены ctx; затем закрывает очередь, дожидается отправки оставшихся писем
// (не дольше DrainTimeout) и возвращается, когда все воркеры завершились.
func (q *Queue) Run(ctx context.Context) {
	// после остановки письма ещё DrainTimeout досылаются, затем контекст отправки отменяется
	sendCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	var wg sync.WaitGroup
	for range q.opt.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range q.ch {
				q.deliver(sendCtx, m)
			}
		}()
	}
	<-ctx.Done()
	q.mu.Lock()
	q.closed = true
	close(q.ch)
	q.mu.Unlock()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(q.opt.DrainTimeout):
		q.log.Warn("почта: остановка — не все письма успели уйти", "left", len(q.ch))
		cancel()
		<-done
	}
}

// deliver отправляет письмо с повторами (пауза растёт вдвое).
func (q *Queue) deliver(ctx context.Context, m Message) {
	wait := q.opt.Backoff
	for attempt := 0; ; attempt++ {
		actx, cancel := context.WithTimeout(ctx, q.opt.SendTimeout)
		err := q.sender.Send(actx, m)
		cancel()
		if err == nil {
			q.log.Info("почта: письмо отправлено", "via", q.sender.Name(), "to", maskEmail(m.To), "attempt", attempt+1)
			return
		}
		if attempt >= q.opt.Retries || ctx.Err() != nil {
			q.log.Error("почта: письмо не отправлено", "via", q.sender.Name(), "to", maskEmail(m.To), "attempts", attempt+1, "err", err)
			return
		}
		q.log.Warn("почта: повтор отправки", "via", q.sender.Name(), "to", maskEmail(m.To), "attempt", attempt+1, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait *= 2
	}
}

// maskEmail — почта в журнале без персональных данных: a***@mail.ru.
func maskEmail(e string) string {
	for i := len(e) - 1; i > 0; i-- {
		if e[i] == '@' {
			return e[:1] + "***" + e[i:]
		}
	}
	return "***"
}

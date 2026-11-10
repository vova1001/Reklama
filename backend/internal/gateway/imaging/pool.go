package imaging

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"ohvat/backend/internal/service"
)

// Pool — пул воркеров поверх обработчика: не больше Workers фото одновременно и не больше Queue в очереди.
// Очередь полна — сразу service.ErrBusy (лучше «попробуйте через минуту», чем съесть всю память);
// клиент ушёл (ctx) — задача снимается с очереди или прерывается между шагами.
type Pool struct {
	next service.ImageProcessor
	jobs chan *job
	wait time.Duration
	log  *slog.Logger
	once sync.Once
}

type job struct {
	ctx  context.Context
	data []byte
	opt  service.ImageOptions
	done chan result
}

type result struct {
	img service.ProcessedImage
	err error
}

// PoolOptions — размер пула.
type PoolOptions struct {
	Workers int           // одновременно; 0 — половина ядер (не меньше 1)
	Queue   int           // ждут в очереди; 0 — 4 × Workers
	Wait    time.Duration // сколько ждать места в очереди, 2 с
}

// NewPool создаёт пул; воркеры запускает Run.
func NewPool(next service.ImageProcessor, o PoolOptions, log *slog.Logger) *Pool {
	if o.Workers <= 0 {
		o.Workers = max(1, runtime.GOMAXPROCS(0)/2)
	}
	if o.Queue <= 0 {
		o.Queue = 4 * o.Workers
	}
	if o.Wait <= 0 {
		o.Wait = 2 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	p := &Pool{next: next, jobs: make(chan *job, o.Queue), wait: o.Wait, log: log}
	p.start(o.Workers)
	return p
}

func (p *Pool) start(n int) {
	for i := 0; i < n; i++ {
		go func() {
			for j := range p.jobs {
				if err := j.ctx.Err(); err != nil { // клиент ушёл, пока ждали
					j.done <- result{err: err}
					continue
				}
				img, err := p.process(j)
				j.done <- result{img: img, err: err}
			}
		}()
	}
}

// process — одна задача; паника декодера не роняет сервер.
func (p *Pool) process(j *job) (img service.ProcessedImage, err error) {
	defer func() {
		if r := recover(); r != nil {
			p.log.Error("фото: паника при обработке", "panic", r)
			err = service.ErrImageType
		}
	}()
	return p.next.Process(j.ctx, j.data, j.opt)
}

// Process — поставить фото в очередь и дождаться результата.
func (p *Pool) Process(ctx context.Context, data []byte, opt service.ImageOptions) (service.ProcessedImage, error) {
	j := &job{ctx: ctx, data: data, opt: opt, done: make(chan result, 1)}
	t := time.NewTimer(p.wait)
	defer t.Stop()
	select {
	case p.jobs <- j:
	case <-t.C:
		return service.ProcessedImage{}, service.ErrBusy
	case <-ctx.Done():
		return service.ProcessedImage{}, ctx.Err()
	}
	select {
	case r := <-j.done:
		return r.img, r.err
	case <-ctx.Done():
		return service.ProcessedImage{}, ctx.Err()
	}
}

// Close — остановить приём; воркеры доделают очередь и выйдут.
func (p *Pool) Close() { p.once.Do(func() { close(p.jobs) }) }

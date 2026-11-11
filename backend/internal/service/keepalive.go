package service

import (
	"context"
	"log/slog"
	"time"
)

// Prober — запрос к своему публичному адресу (через прокси хостинга): ответ 2xx — nil.
type Prober interface {
	Probe(ctx context.Context) error
}

// KeepAlive — самопинг: бесплатный хостинг (Render) усыпляет сервис без входящих запросов через 15 минут;
// запрос к своему публичному адресу раз в Every считается входящим. В журнал — только ошибки.
type KeepAlive struct {
	Prober  Prober
	Every   time.Duration // период, 10 минут
	Timeout time.Duration // на один запрос, 30 секунд
	Log     *slog.Logger
}

// Run — пинговать раз в Every, пока не отменён ctx. Первый запрос — через Every после запуска
// (сразу после старта сервис и так разбужен).
func (k KeepAlive) Run(ctx context.Context) {
	if k.Every <= 0 {
		k.Every = 10 * time.Minute
	}
	if k.Timeout <= 0 {
		k.Timeout = 30 * time.Second
	}
	if k.Log == nil {
		k.Log = slog.Default()
	}
	t := time.NewTicker(k.Every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			k.ping(ctx)
		}
	}
}

func (k KeepAlive) ping(ctx context.Context) {
	pctx, cancel := context.WithTimeout(ctx, k.Timeout)
	defer cancel()
	if err := k.Prober.Probe(pctx); err != nil && ctx.Err() == nil {
		k.Log.Error("самопинг: сайт не ответил", "err", err)
	}
}

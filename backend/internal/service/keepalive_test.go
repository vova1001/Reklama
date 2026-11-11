package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type probeFake struct {
	mu    sync.Mutex
	calls int
	errs  []error
	dl    []bool // у запроса был срок (таймаут)
}

func (p *probeFake) Probe(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := ctx.Deadline()
	p.dl = append(p.dl, ok)
	p.calls++
	if len(p.errs) >= p.calls {
		return p.errs[p.calls-1]
	}
	return nil
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestKeepAliveRun(t *testing.T) {
	p := &probeFake{errs: []error{nil, errors.New("ответ 502")}}
	var logs syncBuf
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		KeepAlive{Prober: p, Every: 5 * time.Millisecond, Timeout: time.Second, Log: slog.New(slog.NewTextHandler(&logs, nil))}.Run(ctx)
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for {
		p.mu.Lock()
		n := p.calls
		p.mu.Unlock()
		if n >= 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("пингов: %d", n)
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
	out := logs.String()
	if strings.Count(out, "level=ERROR") != 1 || !strings.Contains(out, "502") || strings.Contains(out, "level=INFO") {
		t.Errorf("в журнале — только ошибка: %s", out)
	}
	for i, ok := range p.dl {
		if !ok {
			t.Errorf("запрос %d без таймаута", i)
		}
	}
}

func TestKeepAliveStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &probeFake{}
	KeepAlive{Prober: p}.Run(ctx) // значения по умолчанию, сразу выходит
	if p.calls != 0 {
		t.Errorf("пингов после отмены: %d", p.calls)
	}
}

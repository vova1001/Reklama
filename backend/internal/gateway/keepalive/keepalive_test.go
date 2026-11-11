package keepalive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProbe(t *testing.T) {
	var code atomic.Int32
	code.Store(200)
	var path, ua atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path.Store(r.URL.Path)
		ua.Store(r.UserAgent())
		w.WriteHeader(int(code.Load()))
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := New(srv.URL + "/")
	if err := c.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if path.Load() != "/healthz" || !strings.HasPrefix(ua.Load().(string), "ohvat-keepalive") {
		t.Errorf("запрос: %v %v", path.Load(), ua.Load())
	}
	code.Store(503)
	if err := c.Probe(context.Background()); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("503: %v", err)
	}
}

func TestProbeTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := New(srv.URL).Probe(ctx); err == nil {
		t.Fatal("ждали ошибку по таймауту")
	}
	if err := New("http://[::1]:namedport").Probe(context.Background()); err == nil {
		t.Fatal("ждали ошибку адреса")
	}
}

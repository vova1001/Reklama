package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ohvat/backend/internal/domain"
	"ohvat/backend/migrations"
)

// Пример из документации AWS (Signature V4, GET Object с Range) — подпись должна совпасть побайтно.
func TestSignAWSVector(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	sign(req, map[string]string{"Range": "bytes=0-9"}, nil, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"us-east-1", time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("подпись:\n%s\n%s", got, want)
	}
}

// fakeS3 — S3 в памяти: проверяет, что запрос подписан, хранит объекты.
type fakeS3 struct {
	mu   sync.Mutex
	objs map[string][]byte
	ct   map[string]string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=key/") || r.Header.Get("X-Amz-Content-Sha256") == "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		f.objs[r.URL.Path], f.ct[r.URL.Path] = b, r.Header.Get("Content-Type")
	case http.MethodGet:
		b, ok := f.objs[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", f.ct[r.URL.Path])
		_, _ = w.Write(b)
	case http.MethodDelete:
		delete(f.objs, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}
}

func TestS3(t *testing.T) {
	f := &fakeS3{objs: map[string][]byte{}, ct: map[string]string{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	s := NewS3(srv.URL+"/", "photos", "", "key", "secret")
	ctx := context.Background()
	if err := s.Put(ctx, "media/abc/320.webp", "image/webp", []byte("RIFF")); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.objs["/photos/media/abc/320.webp"]; !ok {
		t.Fatalf("ключ: %v", f.objs)
	}
	b, err := s.Get(ctx, "media/abc/320.webp")
	if err != nil || string(b.Data) != "RIFF" || b.ContentType != "image/webp" {
		t.Fatalf("%v %+v", err, b)
	}
	if _, err := s.Get(ctx, "media/none/320.webp"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("нет: %v", err)
	}
	if err := s.Delete(ctx, []string{"media/abc/320.webp", "media/none/1.webp"}); err != nil {
		t.Fatal(err)
	}
	if len(f.objs) != 0 {
		t.Error("не удалено")
	}
	bad := NewS3(srv.URL, "photos", "", "other", "secret")
	if err := bad.Put(ctx, "k", "image/webp", nil); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("отказ: %v", err)
	}
}

func TestPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL не задан")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := migrations.Up(pool); err != nil {
		t.Fatal(err)
	}
	p := NewPostgres(pool)
	if err := p.Put(ctx, "t/a.webp", "image/webp", []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := p.Put(ctx, "t/a.webp", "image/webp", []byte{3}); err != nil {
		t.Fatal("перезапись:", err)
	}
	b, err := p.Get(ctx, "t/a.webp")
	if err != nil || len(b.Data) != 1 || b.Data[0] != 3 || b.ModTime.IsZero() {
		t.Fatalf("%v %+v", err, b)
	}
	if err := p.Delete(ctx, []string{"t/a.webp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(ctx, "t/a.webp"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("%v", err)
	}
	if err := p.Delete(ctx, nil); err != nil {
		t.Error(err)
	}
}

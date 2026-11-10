package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
)

type mediaEnv struct {
	clock *fakeClock
	repo  *memMedia
	blobs *memBlobs
	img   *fakeImages
	s     *Media
}

func newMediaEnv(cfg MediaConfig) *mediaEnv {
	c := &fakeClock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
	e := &mediaEnv{clock: c, repo: newMemMedia(c), blobs: &memBlobs{data: map[string][]byte{}}, img: &fakeImages{}}
	e.s = NewMedia(e.repo, e.blobs, e.img, cfg, nil)
	e.s.clock = c
	return e
}

func TestMediaUpload(t *testing.T) {
	e := newMediaEnv(MediaConfig{MaxBytes: 10, MaxFiles: 2})
	ctx := context.Background()
	u := domain.User{ID: 3}
	m, err := e.s.Upload(ctx, u, UploadInput{Purpose: domain.PurposeTaskCover, Data: []byte("photo")})
	if err != nil || len(m.ID) != 22 || m.Bytes != 12 || m.Width != 1600 {
		t.Fatalf("%v %+v", err, m)
	}
	if e.img.last.Aspect != 16.0/9 || len(e.img.last.Sizes) != 3 {
		t.Errorf("пропорции обложки: %+v", e.img.last)
	}
	if len(e.blobs.data) != 3 {
		t.Errorf("файлов: %d", len(e.blobs.data))
	}
	cases := map[string]struct {
		in   UploadInput
		want error
	}{
		"назначение": {UploadInput{Purpose: "banner", Data: []byte("x")}, &ValidationError{}},
		"пусто":      {UploadInput{Purpose: domain.PurposeAvatar}, &ValidationError{}},
		"обрезка":    {UploadInput{Purpose: domain.PurposeAvatar, Data: []byte("x"), Crop: &CropRect{X: 0.8, W: 0.5, H: 0.5}}, &ValidationError{}},
		"тип":        {UploadInput{Purpose: domain.PurposeAvatar, Data: []byte("bad")}, ErrImageType},
		"пиксели":    {UploadInput{Purpose: domain.PurposeAvatar, Data: []byte("huge")}, ErrImageTooLarge},
		"байты":      {UploadInput{Purpose: domain.PurposeAvatar, Data: []byte("12345678901")}, ErrImageTooLarge},
	}
	for name, c := range cases {
		_, err := e.s.Upload(ctx, u, c.in)
		var ve *ValidationError
		if _, isVE := c.want.(*ValidationError); isVE {
			if !errors.As(err, &ve) {
				t.Errorf("%s: %v", name, err)
			}
		} else if !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// квота по числу
	if _, err := e.s.Upload(ctx, u, UploadInput{Purpose: domain.PurposeTaskPhoto, Data: []byte("p")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Upload(ctx, u, UploadInput{Purpose: domain.PurposeTaskPhoto, Data: []byte("p")}); !errors.Is(err, ErrMediaQuota) {
		t.Errorf("квота: %v", err)
	}
	// хранилище упало — записи нет
	e2 := newMediaEnv(MediaConfig{})
	e2.blobs.fail = errors.New("s3 down")
	if _, err := e2.s.Upload(ctx, u, UploadInput{Purpose: domain.PurposeAvatar, Data: []byte("p")}); err == nil || len(e2.repo.items) != 0 {
		t.Errorf("сбой хранилища: %v", err)
	}
}

func TestMediaQuotaBytes(t *testing.T) {
	e := newMediaEnv(MediaConfig{QuotaBytes: 20})
	u := domain.User{ID: 3}
	e.repo.add(domain.Media{ID: strings.Repeat("q", 22), OwnerID: 3, Bytes: 20})
	if _, err := e.s.Upload(context.Background(), u, UploadInput{Purpose: domain.PurposeAvatar, Data: []byte("p")}); !errors.Is(err, ErrMediaQuota) {
		t.Errorf("%v", err)
	}
}

func TestMediaOpenAndAvatar(t *testing.T) {
	e := newMediaEnv(MediaConfig{})
	ctx := context.Background()
	owner := domain.User{ID: 3}
	m, _ := e.s.Upload(ctx, owner, UploadInput{Purpose: domain.PurposeAvatar, Data: []byte("p")})
	// не прикреплено — видит только владелец; остальным — как нет
	if _, pub, err := e.s.Open(ctx, 3, m.ID, 320); err != nil || pub {
		t.Fatalf("владелец: %v %v", err, pub)
	}
	for _, viewer := range []int64{0, 4} {
		if _, _, err := e.s.Open(ctx, viewer, m.ID, 320); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("зритель %d: %v", viewer, err)
		}
	}
	for _, bad := range []struct {
		id   string
		size int
	}{{m.ID, 321}, {"../../etc/passwd", 320}, {strings.Repeat("z", 22), 320}} {
		if _, _, err := e.s.Open(ctx, 3, bad.id, bad.size); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	// аватар профиля — виден всем
	url, err := e.s.SetAvatar(ctx, owner, m.ID)
	if err != nil || url != "/media/"+m.ID+"/320.webp" {
		t.Fatalf("%v %s", err, url)
	}
	if _, pub, err := e.s.Open(ctx, 0, m.ID, 800); err != nil || !pub {
		t.Errorf("аватар всем: %v %v", err, pub)
	}
	if _, err := e.s.SetAvatar(ctx, domain.User{ID: 4}, m.ID); err == nil {
		t.Error("чужое фото в аватар")
	}
	if url, err := e.s.SetAvatar(ctx, owner, ""); err != nil || url != "" {
		t.Errorf("убрать: %v %q", err, url)
	}
}

func TestMediaDeleteAndCleanup(t *testing.T) {
	e := newMediaEnv(MediaConfig{OrphanTTL: time.Hour})
	ctx := context.Background()
	u := domain.User{ID: 3}
	a, _ := e.s.Upload(ctx, u, UploadInput{Purpose: domain.PurposeTaskPhoto, Data: []byte("p")})
	b, _ := e.s.Upload(ctx, u, UploadInput{Purpose: domain.PurposeTaskPhoto, Data: []byte("p")})
	c, _ := e.s.Upload(ctx, u, UploadInput{Purpose: domain.PurposeTaskPhoto, Data: []byte("p")})
	if err := e.s.Delete(ctx, domain.User{ID: 4}, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("чужое: %v", err)
	}
	if err := e.s.Delete(ctx, u, a.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.blobs.data) != 6 {
		t.Errorf("файлы удалены: %d", len(e.blobs.data))
	}
	e.repo.items[c.ID].Subject = "task" // прикреплено — не сирота
	if n, _ := e.s.Cleanup(ctx); n != 0 {
		t.Error("рано")
	}
	e.clock.add(2 * time.Hour)
	n, err := e.s.Cleanup(ctx)
	if err != nil || n != 1 || e.repo.items[b.ID] != nil || e.repo.items[c.ID] == nil || len(e.blobs.data) != 3 {
		t.Errorf("очистка: %v %d", err, n)
	}
}

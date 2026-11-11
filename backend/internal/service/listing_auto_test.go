package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
)

func newAutoEnv() *listingEnv {
	c := &fakeClock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
	e := &listingEnv{clock: c, repo: newMemListings(c), cats: newMemCats(), media: newMemMedia(c), plats: fakePlatforms{}}
	e.repo.cats = e.cats
	e.plats[author.ID] = []domain.Platform{{Kind: "youtube", Type: domain.SocialChannel, Followers: 12000, Verified: true}}
	d := ListingDeps{Categories: e.cats, Submits: e.repo, Media: e.media, Clock: c, Mode: domain.ModerationAuto, Platforms: e.plats}
	e.tasks, e.cards, e.mod = NewTasks(e.repo, d), NewCards(e.repo, d), NewModeration(e.repo)
	return e
}

// MODERATION_MODE=auto: отправка сразу публикует; в журнале — «на проверке» от владельца и «опубликовано» от системы.
func TestAutoModeration(t *testing.T) {
	e := newAutoEnv()
	ctx := context.Background()

	d, _ := e.tasks.Create(ctx, biz, fullTask())
	got, err := e.tasks.Submit(ctx, biz, d.ID)
	if err != nil || got.Status != domain.StatusPublished || got.PublishedAt == nil || got.RejectReason != "" {
		t.Fatalf("задание: %v %+v", err, got)
	}
	h, _ := e.mod.History(ctx, domain.SubjectTask, d.ID)
	if len(h) != 2 || h[0].To != domain.StatusPending || h[0].ActorID != biz.ID || h[0].Auto ||
		h[1].To != domain.StatusPublished || !h[1].Auto || h[1].ActorID != 0 || h[1].Reason != domain.AutoApproveNote {
		t.Fatalf("журнал: %+v", h)
	}
	// модератору решать нечего
	if _, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, ModeratorID: 1, Approve: true}); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("уже опубликовано: %v", err)
	}
	// в ленте — сразу
	if p, _ := e.tasks.Feed(ctx, FeedQuery{}); len(p.Tasks) != 1 {
		t.Errorf("лента: %+v", p)
	}

	// анкета: блоки и фото не обязательны (регрессия: анкета без «О себе» не отправлялась)
	in := fullCard()
	in.Blocks = nil
	if _, err := e.cards.Save(ctx, author, in); err != nil {
		t.Fatal(err)
	}
	c, err := e.cards.Submit(ctx, author)
	if err != nil || c.Status != domain.StatusPublished {
		t.Fatalf("анкета: %v %+v", err, c)
	}
	if p, _ := e.cards.Feed(ctx, FeedQuery{}); len(p.Cards) != 1 || p.Cards[0].UserID != author.ID {
		t.Errorf("лента анкет: %+v", p)
	}
	if pub, err := e.cards.Public(ctx, author.ID); err != nil || pub.Name != "Алина" {
		t.Errorf("страница анкеты: %v", err)
	}
	// снять, чтобы изменить → правка → снова сразу опубликована
	if c, err = e.cards.Withdraw(ctx, author); err != nil || c.Status != domain.StatusDraft {
		t.Fatalf("снять: %v %+v", err, c)
	}
	if _, err := e.cards.Public(ctx, author.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("снятая не видна: %v", err)
	}
	if _, err := e.cards.Save(ctx, author, fullCard()); err != nil {
		t.Fatal(err)
	}
	if c, err = e.cards.Submit(ctx, author); err != nil || c.Status != domain.StatusPublished {
		t.Fatalf("повторно: %v %+v", err, c)
	}
}

// Ручной режим (и пустой) — ждёт модератора.
func TestManualModerationDefault(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	d, _ := e.tasks.Create(context.Background(), biz, fullTask())
	if got, err := e.tasks.Submit(context.Background(), biz, d.ID); err != nil || got.Status != domain.StatusPending {
		t.Fatalf("%v %+v", err, got)
	}
}

// Автоматическое одобрение атомарно: статус сменился между чтением и записью — ничего не меняется.
func TestAutoModerationConflict(t *testing.T) {
	e := newAutoEnv()
	ctx := context.Background()
	d, _ := e.tasks.Create(ctx, biz, fullTask())
	chain := domain.SubmitChain(domain.ModerationAuto, domain.SubjectTask, d.ID, domain.StatusRejected, biz.ID)
	if _, err := e.repo.SetTaskStatus(ctx, chain...); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("ждали конфликт: %v", err)
	}
	if got, _ := e.tasks.Get(ctx, biz, d.ID); got.Status != domain.StatusDraft || len(e.repo.log) != 0 {
		t.Fatalf("частично применилось: %+v %v", got, e.repo.log)
	}
}

func TestFeedFilter(t *testing.T) {
	e := newAutoEnv()
	ctx := context.Background()
	mk := func(city string, nationwide bool, cat, format string) int64 {
		in := fullTask()
		in.City, in.Nationwide, in.Categories, in.Formats = city, nationwide, []string{cat}, []string{format}
		d, err := e.tasks.Create(ctx, biz, in)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.tasks.Submit(ctx, biz, d.ID); err != nil {
			t.Fatal(err)
		}
		return d.ID
	}
	kazan := mk("Казань", false, "food", "reels")
	all := mk("", true, "beauty", "shorts")
	_ = mk("Москва", false, "food", "shorts")

	ids := func(q FeedQuery) []int64 {
		p, err := e.tasks.Feed(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, t := range p.Tasks {
			out = append(out, t.ID)
		}
		return out
	}
	if got := ids(FeedQuery{Filter: domain.FeedFilter{City: " Казань "}}); len(got) != 2 || got[0] != all || got[1] != kazan {
		t.Errorf("город (и «Вся Россия»): %v", got)
	}
	if got := ids(FeedQuery{Filter: domain.FeedFilter{Categories: []string{"food"}, Formats: []string{"reels"}}}); len(got) != 1 || got[0] != kazan {
		t.Errorf("сфера и формат: %v", got)
	}
	// несколько сфер — любая из них; пустые значения отбрасываются
	if got := ids(FeedQuery{Filter: domain.FeedFilter{Categories: []string{"beauty", " ", "sport"}}}); len(got) != 1 || got[0] != all {
		t.Errorf("любая из сфер: %v", got)
	}
	if got := ids(FeedQuery{Filter: domain.FeedFilter{Formats: []string{"reels", "shorts", "reels"}}}); len(got) != 3 || e.repo.lastFilter.Formats[1] != "shorts" || len(e.repo.lastFilter.Formats) != 2 {
		t.Errorf("любой из форматов: %v %+v", got, e.repo.lastFilter)
	}
	// страницы по курсору
	p, _ := e.tasks.Feed(ctx, FeedQuery{Limit: 2})
	if len(p.Tasks) != 2 || p.Next == nil {
		t.Fatalf("страница 1: %+v", p)
	}
	if p2, _ := e.tasks.Feed(ctx, FeedQuery{Limit: 2, After: p.Next}); len(p2.Tasks) != 1 || p2.Next != nil {
		t.Fatalf("страница 2: %+v", p2)
	}
	// неверный отбор — ошибки по полям
	for _, c := range []struct {
		q     domain.FeedFilter
		field string
	}{
		{domain.FeedFilter{Categories: []string{"Еда!"}}, "category"}, {domain.FeedFilter{Categories: []string{"a", "b", "c", "d", "e", "f"}}, "category"},
		{domain.FeedFilter{Formats: []string{"vine"}}, "format"}, {domain.FeedFilter{Formats: []string{"Reels"}}, "format"},
		{domain.FeedFilter{City: strings.Repeat("я", 81)}, "city"},
	} {
		if f := fields(t, func() error { _, err := e.tasks.Feed(ctx, FeedQuery{Filter: c.q}); return err }()); f[c.field] == "" {
			t.Errorf("%+v: %v", c.q, f)
		}
	}
	if f := fields(t, func() error {
		_, err := e.cards.Feed(ctx, FeedQuery{Filter: domain.FeedFilter{Categories: []string{"Еда!"}}})
		return err
	}()); f["topic"] == "" {
		t.Errorf("тема у анкет: %v", f)
	}
	// у анкет формата нет
	if f := fields(t, func() error {
		_, err := e.cards.Feed(ctx, FeedQuery{Filter: domain.FeedFilter{Formats: []string{"reels"}}})
		return err
	}()); f["format"] == "" {
		t.Errorf("формат у анкет: %v", f)
	}
}

// Срок с временем по Москве; прошёл — задание завершается и пропадает из ленты.
func TestTaskExpiry(t *testing.T) {
	e := newAutoEnv()
	ctx := context.Background()
	in := fullTask()
	in.PublishBy, in.PublishTime = "2026-11-12", "18:30"
	d, err := e.tasks.Create(ctx, biz, in)
	if err != nil || d.PublishTime != "18:30" || !d.DeadlineAt.Equal(time.Date(2026, 11, 12, 15, 30, 0, 0, time.UTC)) {
		t.Fatalf("срок: %v %+v", err, d.DeadlineAt)
	}
	in.PublishTime = ""
	d2, _ := e.tasks.Create(ctx, biz, in)
	if !d2.DeadlineAt.Equal(time.Date(2026, 11, 12, 20, 59, 59, 0, time.UTC)) {
		t.Errorf("без времени — до конца дня по Москве: %v", d2.DeadlineAt)
	}
	// просмотры обязательны
	in.MinViews = 0
	d3, _ := e.tasks.Create(ctx, biz, in)
	if f := fields(t, func() error { _, err := e.tasks.Submit(ctx, biz, d3.ID); return err }()); f["minViews"] != "Укажите число просмотров" {
		t.Errorf("просмотры: %v", f)
	}
	if _, err := e.tasks.Submit(ctx, biz, d.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.tasks.Expire(ctx); n != 0 {
		t.Errorf("рано: %d", n)
	}
	e.clock.add(55 * time.Hour) // 12 ноября 16:00 UTC — срок 15:30 прошёл
	if n, err := e.tasks.Expire(ctx); err != nil || n != 1 {
		t.Fatalf("завершено: %v %d", err, n)
	}
	got, _ := e.tasks.Get(ctx, biz, d.ID)
	if got.Status != domain.StatusExpired {
		t.Errorf("статус: %v", got.Status)
	}
	// владелец может вернуть в черновик и продлить
	if got, err = e.tasks.Withdraw(ctx, biz, d.ID); err != nil || got.Status != domain.StatusDraft {
		t.Errorf("в черновик: %v %+v", err, got)
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	e.tasks.RunExpiry(c, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

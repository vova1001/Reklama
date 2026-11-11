package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
)

type listingEnv struct {
	clock *fakeClock
	repo  *memListings
	cats  *memCats
	media *memMedia
	tasks *Tasks
	cards *Cards
	mod   *Moderation
}

func newListingEnv(lim ListingLimits) *listingEnv {
	c := &fakeClock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
	e := &listingEnv{clock: c, repo: newMemListings(c), cats: newMemCats(), media: newMemMedia(c)}
	e.repo.cats = e.cats
	d := ListingDeps{Categories: e.cats, Submits: e.repo, Media: e.media, Clock: c, Limits: lim}
	e.tasks, e.cards, e.mod = NewTasks(e.repo, d), NewCards(e.repo, d), NewModeration(e.repo)
	return e
}

var biz = domain.User{ID: 7, ActiveMode: domain.ModeBusiness}

func fullTask() TaskInput {
	return TaskInput{
		Title: "  Сторис  о новом меню ", City: "Казань", Categories: []string{"food"}, Formats: []string{"reels", "shorts", "reels"},
		Access: "approval", MinViews: 5000, RewardKopecks: 300000, PublishBy: "2026-11-20",
		Blocks: []domain.Block{
			{Kind: domain.BlockText, Title: "Описание", Items: []string{"Снимите  завтрак", " "}},
			{Kind: domain.BlockList, Title: "Что показать", Items: []string{"Интерьер", "Сырники"}},
			{Kind: domain.BlockList, Title: "", Items: nil}, // пустой — отбросится
		},
	}
}

func fields(t *testing.T, err error) map[string]string {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("ждали ValidationError, а %v", err)
	}
	return ve.Fields
}

func TestTaskLifecycle(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	ctx := context.Background()

	// черновик может быть пустым
	d, err := e.tasks.Create(ctx, biz, TaskInput{})
	if err != nil || d.Status != domain.StatusDraft || d.Access != domain.AccessOpen {
		t.Fatalf("%v %+v", err, d)
	}
	// неполный — на проверку нельзя, ошибки по полям
	f := fields(t, func() error { _, err := e.tasks.Submit(ctx, biz, d.ID); return err }())
	for _, k := range []string{"title", "city", "categories", "formats", "minViews", "reward", "publishBy", "blocks"} {
		if f[k] == "" {
			t.Errorf("нет ошибки %s: %v", k, f)
		}
	}
	// заполнить
	got, err := e.tasks.Update(ctx, biz, d.ID, fullTask())
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Сторис о новом меню" || len(got.Formats) != 2 || len(got.Blocks) != 2 || len(got.Blocks[0].Items) != 1 ||
		got.Blocks[0].Items[0] != "Снимите завтрак" || got.PublishBy.Format(time.DateOnly) != "2026-11-20" {
		t.Fatalf("нормализация: %+v", got)
	}
	// на проверку → правка запрещена → вернуть в черновик → снова на проверку
	got, err = e.tasks.Submit(ctx, biz, d.ID)
	if err != nil || got.Status != domain.StatusPending {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := e.tasks.Update(ctx, biz, d.ID, fullTask()); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("правка на проверке: %v", err)
	}
	if err := e.tasks.Delete(ctx, biz, d.ID); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("удаление на проверке: %v", err)
	}
	if _, err := e.tasks.Submit(ctx, biz, d.ID); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("повторная отправка: %v", err)
	}
	if got, err = e.tasks.Withdraw(ctx, biz, d.ID); err != nil || got.Status != domain.StatusDraft {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := e.tasks.Withdraw(ctx, biz, d.ID); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("черновик не отзывается: %v", err)
	}
	if _, err := e.tasks.Submit(ctx, biz, d.ID); err != nil {
		t.Fatal(err)
	}
	// модератор отклоняет с причиной → правка → снова на проверку → публикует
	if _, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, ModeratorID: 1, Reason: "ok"}); fields(t, err)["reason"] == "" {
		t.Error("короткая причина")
	}
	st, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, ModeratorID: 1, Reason: "Уточните срок"})
	if err != nil || st != domain.StatusRejected {
		t.Fatalf("%v %v", err, st)
	}
	got, _ = e.tasks.Get(ctx, biz, d.ID)
	if got.RejectReason != "Уточните срок" {
		t.Errorf("причина: %+v", got)
	}
	if _, err := e.tasks.Update(ctx, biz, d.ID, fullTask()); err != nil {
		t.Errorf("отклонённое правится: %v", err)
	}
	if got, err = e.tasks.Submit(ctx, biz, d.ID); err != nil || got.RejectReason != "" {
		t.Fatalf("повторно: %v %+v", err, got)
	}
	if _, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, ModeratorID: 1, Approve: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, ModeratorID: 1, Approve: true}); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("дважды: %v", err)
	}
	// журнал
	h, _ := e.mod.History(ctx, domain.SubjectTask, d.ID)
	var path []string
	for _, c := range h {
		path = append(path, string(c.To))
	}
	if strings.Join(path, ">") != "pending_review>draft>pending_review>rejected>pending_review>published" {
		t.Errorf("журнал: %v", path)
	}
	// лента
	p, err := e.tasks.Feed(ctx, FeedQuery{})
	if err != nil || len(p.Tasks) != 1 || p.Next != nil {
		t.Fatalf("%v %+v", err, p)
	}
	if pub, err := e.tasks.Public(ctx, d.ID); err != nil || pub.ID != d.ID {
		t.Fatalf("страница задания: %v", err)
	}
	// снять с публикации, чтобы изменить: из ленты и со страницы пропадает
	if got, err = e.tasks.Withdraw(ctx, biz, d.ID); err != nil || got.Status != domain.StatusDraft {
		t.Fatalf("снять с публикации: %v %+v", err, got)
	}
	if _, err := e.tasks.Public(ctx, d.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("черновик не виден: %v", err)
	}
	// чужое — как нет
	if _, err := e.tasks.Get(ctx, domain.User{ID: 99}, d.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("чужое: %v", err)
	}
}

func TestTaskShape(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	ctx := context.Background()
	long := strings.Repeat("я", 101)
	many := make([]domain.Block, 9)
	cases := []struct {
		name  string
		mut   func(*TaskInput)
		field string
	}{
		{"название", func(in *TaskInput) { in.Title = long }, "title"},
		{"свой формат короткий", func(in *TaskInput) { in.Formats = []string{"reels", "я"} }, "formats"},
		{"свой формат ссылка", func(in *TaskInput) { in.Formats = []string{"vk.com/clip"} }, "formats"},
		{"свой формат без букв", func(in *TaskInput) { in.Formats = []string{"123"} }, "formats"},
		{"форматов много", func(in *TaskInput) { in.Formats = []string{"reels", "shorts", "tiktok", "Stories"} }, "formats"},
		{"сфер много", func(in *TaskInput) { in.Categories = []string{"a", "b", "c", "d", "e", "f"} }, "categories"},
		{"тип", func(in *TaskInput) { in.Access = "vip" }, "access"},
		{"просмотры", func(in *TaskInput) { in.MinViews = -1 }, "minViews"},
		{"время срока", func(in *TaskInput) { in.PublishTime = "25:00" }, "publishTime"},
		{"оплата", func(in *TaskInput) { in.RewardKopecks = 2e8 }, "reward"},
		{"дата", func(in *TaskInput) { in.PublishBy = "20.11.2026" }, "publishBy"},
		{"сфера не slug", func(in *TaskInput) { in.Categories = []string{"food", "Еда"} }, "categories"},
		{"сфера не из списка", func(in *TaskInput) { in.Categories = []string{"food", "cars"} }, "categories"},
		{"блоков много", func(in *TaskInput) { in.Blocks = many }, "blocks"},
		{"вид блока", func(in *TaskInput) { in.Blocks = []domain.Block{{Kind: "html", Items: []string{"x"}}} }, "blocks.0"},
		{"заголовок блока", func(in *TaskInput) { in.Blocks = []domain.Block{{Kind: "list", Title: long}} }, "blocks.0"},
		{"пункт длинный", func(in *TaskInput) {
			in.Blocks = []domain.Block{{Kind: "list", Title: "x", Items: []string{strings.Repeat("a", 201)}}}
		}, "blocks.0"},
		{"пунктов много", func(in *TaskInput) {
			in.Blocks = []domain.Block{{Kind: "list", Title: "x", Items: strings.Split(strings.Repeat("a,", 16), ",")}}
		}, "blocks.0"},
		{"ссылка", func(in *TaskInput) {
			in.Blocks = []domain.Block{{Kind: "links", Title: "x", Items: []string{"javascript:alert(1)"}}}
		}, "blocks.0"},
		{"текста много", func(in *TaskInput) {
			p := strings.Repeat("б", 1000)
			in.Blocks = []domain.Block{{Kind: "text", Title: "a", Items: []string{p, p, p, p, p, p, p}}}
		}, "blocks"},
		{"обложка чужая", func(in *TaskInput) { in.Cover = "AAAAAAAAAAAAAAAAAAAAAA" }, "cover"},
		{"фото много", func(in *TaskInput) {
			in.Photos = []string{"a", "b", "c", "d", "e", "f", "g"}
		}, "photos"},
	}
	for _, c := range cases {
		in := fullTask()
		c.mut(&in)
		_, err := e.tasks.Create(ctx, biz, in)
		if f := fields(t, err); f[c.field] == "" {
			t.Errorf("%s: нет ошибки %s: %v", c.name, c.field, f)
		}
	}
	// «Вся Россия» — город стирается и не нужен
	in := fullTask()
	in.Nationwide, in.City = true, "Казань"
	got, err := e.tasks.Create(ctx, biz, in)
	if err != nil || got.City != "" {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := e.tasks.Submit(ctx, biz, got.ID); err != nil {
		t.Errorf("вся Россия: %v", err)
	}
	// ссылки нормализуются
	in = fullTask()
	in.Blocks = append(in.Blocks, domain.Block{Kind: "links", Title: "Примеры", Items: []string{"http://Example.com/a"}})
	got, _ = e.tasks.Create(ctx, biz, in)
	if got.Blocks[2].Items[0] != "https://example.com/a" {
		t.Errorf("ссылка: %v", got.Blocks[2].Items)
	}
}

func TestTaskDeadline(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	ctx := context.Background()
	for date, ok := range map[string]bool{"2026-11-10": false, "2026-11-11": true, "2027-02-08": true, "2027-02-09": false} {
		in := fullTask()
		in.PublishBy = date
		d, err := e.tasks.Create(ctx, biz, in)
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.tasks.Submit(ctx, biz, d.ID)
		if (err == nil) != ok {
			t.Errorf("%s: %v", date, err)
		}
	}
	// «сегодня» — по Москве: в 22:00 UTC 10 ноября в Москве уже 11-е, срок 11-го — не «от завтра»
	e.clock.add(13 * time.Hour)
	in := fullTask()
	in.PublishBy = "2026-11-11"
	d, _ := e.tasks.Create(ctx, biz, in)
	if _, err := e.tasks.Submit(ctx, biz, d.ID); fields(t, err)["publishBy"] == "" {
		t.Error("по Москве уже 11-е")
	}
}

func TestTaskLimits(t *testing.T) {
	e := newListingEnv(ListingLimits{MaxTasks: 3, SubmitsPerDay: 2})
	ctx := context.Background()
	var ids []int64
	for i := 0; i < 3; i++ {
		d, err := e.tasks.Create(ctx, biz, fullTask())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, d.ID)
	}
	if _, err := e.tasks.Create(ctx, biz, fullTask()); !errors.Is(err, ErrTooManyTasks) {
		t.Errorf("предел заданий: %v", err)
	}
	if err := e.tasks.Delete(ctx, biz, ids[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tasks.Get(ctx, biz, ids[2]); !errors.Is(err, domain.ErrNotFound) {
		t.Error("удалено")
	}
	for _, id := range ids[:2] {
		if _, err := e.tasks.Submit(ctx, biz, id); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = e.tasks.Withdraw(ctx, biz, ids[0])
	e.clock.add(time.Hour)
	_, err := e.tasks.Submit(ctx, biz, ids[0])
	var le *LimitError
	if !errors.As(err, &le) || le.Reason != "submits_daily" || le.RetryAfter != 23*time.Hour {
		t.Fatalf("лимит отправок: %v %+v", err, le)
	}
	e.clock.add(23 * time.Hour)
	if _, err := e.tasks.Submit(ctx, biz, ids[0]); err != nil {
		t.Errorf("через сутки: %v", err)
	}
}

func TestTaskMedia(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	ctx := context.Background()
	cover := strings.Repeat("c", 22)
	ph := strings.Repeat("p", 22)
	alien := strings.Repeat("x", 22)
	wrong := strings.Repeat("w", 22)
	e.media.add(domain.Media{ID: cover, OwnerID: biz.ID, Purpose: domain.PurposeTaskCover})
	e.media.add(domain.Media{ID: ph, OwnerID: biz.ID, Purpose: domain.PurposeTaskPhoto})
	e.media.add(domain.Media{ID: alien, OwnerID: 99, Purpose: domain.PurposeTaskPhoto})
	e.media.add(domain.Media{ID: wrong, OwnerID: biz.ID, Purpose: domain.PurposeAvatar})
	in := fullTask()
	in.Cover, in.Photos = cover, []string{ph, ph}
	got, err := e.tasks.Create(ctx, biz, in)
	if err != nil || got.Cover != cover || len(got.Photos) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	for name, mut := range map[string]func(*TaskInput){
		"чужое":        func(in *TaskInput) { in.Photos = []string{alien} },
		"не то":        func(in *TaskInput) { in.Photos = []string{wrong} },
		"обложка-фото": func(in *TaskInput) { in.Cover = ph },
	} {
		in := fullTask()
		mut(&in)
		if _, err := e.tasks.Create(ctx, biz, in); err == nil {
			t.Errorf("%s: принято", name)
		}
	}
	// уже прикреплено к другому заданию — к новому нельзя, к своему — можно
	e.media.items[ph].Subject, e.media.items[ph].SubjectID = string(domain.SubjectTask), got.ID
	in = fullTask()
	in.Photos = []string{ph}
	if _, err := e.tasks.Create(ctx, biz, in); err == nil {
		t.Error("фото другого задания")
	}
	if _, err := e.tasks.Update(ctx, biz, got.ID, in); err != nil {
		t.Errorf("своё: %v", err)
	}
}

var author = domain.User{ID: 8, ActiveMode: domain.ModeCreator}

func fullCard() CardInput {
	return CardInput{
		Name: "Алина", City: "Москва", Topics: []string{"beauty", "food"},
		Platforms: []PlatformInput{{URL: "youtube.com/@alina", Followers: 12000}, {URL: "https://t.me/alina", Followers: 800}, {}},
		Blocks: []domain.Block{
			{Kind: domain.BlockText, Title: "О себе", Items: []string{"Снимаю еду"}},
			{Kind: domain.BlockList, Title: "Форматы и цены", Items: []string{"Reels — 5 000 ₽"}},
			{Kind: domain.BlockLinks, Title: "Примеры работ", Items: []string{"https://vk.com/clip1"}},
		},
	}
}

func TestCardLifecycle(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	ctx := context.Background()
	if c, err := e.cards.Get(ctx, author); err != nil || c != nil {
		t.Fatalf("нет анкеты: %v %+v", err, c)
	}
	if _, err := e.cards.Submit(ctx, author); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("без анкеты: %v", err)
	}
	c, err := e.cards.Save(ctx, author, CardInput{Name: "А"})
	if err != nil || c.Status != domain.StatusDraft {
		t.Fatalf("черновик: %v %+v", err, c)
	}
	f := fields(t, func() error { _, err := e.cards.Submit(ctx, author); return err }())
	for _, k := range []string{"name", "city", "topics", "platforms"} {
		if f[k] == "" {
			t.Errorf("нет ошибки %s: %v", k, f)
		}
	}
	if f["blocks"] != "" || f["works"] != "" || f["avatar"] != "" {
		t.Errorf("лишнее обязательное: %v", f)
	}
	c, err = e.cards.Save(ctx, author, fullCard())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Platforms) != 2 || c.Platforms[0].Kind != "youtube" || c.Platforms[0].URL != "https://youtube.com/@alina" || c.Platforms[1].Kind != "telegram" {
		t.Fatalf("площадки: %+v", c.Platforms)
	}
	if c, err = e.cards.Submit(ctx, author); err != nil || c.Status != domain.StatusPending {
		t.Fatalf("%v %+v", err, c)
	}
	if _, err := e.cards.Save(ctx, author, fullCard()); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("на проверке: %v", err)
	}
	if c, err = e.cards.Withdraw(ctx, author); err != nil || c.Status != domain.StatusDraft {
		t.Fatalf("%v %+v", err, c)
	}
	_, _ = e.cards.Submit(ctx, author)
	if st, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectCard, ID: author.ID, ModeratorID: 1, Approve: true}); err != nil || st != domain.StatusPublished {
		t.Fatalf("%v %v", err, st)
	}
}

func TestCardShape(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	ctx := context.Background()
	cases := map[string]struct {
		mut   func(*CardInput)
		field string
	}{
		"тем много":      {func(in *CardInput) { in.Topics = []string{"a", "b", "c", "d", "e", "f"} }, "topics"},
		"тема чужая":     {func(in *CardInput) { in.Topics = []string{"cars"} }, "topics"},
		"ссылка":         {func(in *CardInput) { in.Platforms = []PlatformInput{{URL: "ftp://x.ru", Followers: 1}} }, "platforms.0"},
		"подписчики":     {func(in *CardInput) { in.Platforms = []PlatformInput{{URL: "t.me/a", Followers: -5}} }, "platforms.0"},
		"площадок много": {func(in *CardInput) { in.Platforms = make([]PlatformInput, 7) }, "platforms"},
		"работ много": {func(in *CardInput) {
			in.Works = strings.Split(strings.Repeat("a,", 10), ",")
		}, "works"},
	}
	for name, c := range cases {
		in := fullCard()
		c.mut(&in)
		if f := fields(t, func() error { _, err := e.cards.Save(ctx, author, in); return err }()); f[c.field] == "" {
			t.Errorf("%s: %v", name, f)
		}
	}
	// подписчиков нет — к отправке нельзя
	in := fullCard()
	in.Platforms = []PlatformInput{{URL: "t.me/a"}}
	_, _ = e.cards.Save(ctx, author, in)
	if f := fields(t, func() error { _, err := e.cards.Submit(ctx, author); return err }()); f["platforms.0"] == "" {
		t.Errorf("подписчики: %v", f)
	}
}

func TestPlatformKind(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.youtube.com/@a": "youtube", "https://m.youtube.com/x": "youtube", "https://youtu.be/x": "youtube",
		"https://t.me/a": "telegram", "https://vk.com/a": "vk", "https://vkvideo.ru/a": "vk", "https://www.tiktok.com/@a": "tiktok",
		"https://dzen.ru/a": "dzen", "https://rutube.ru/a": "rutube", "https://notyoutube.com": "other", "https://my.site.ru": "other",
	} {
		if got := platformKind(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func TestModerationGuards(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	ctx := context.Background()
	d, _ := e.tasks.Create(ctx, biz, fullTask())
	if _, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, ModeratorID: 1, Approve: true}); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("черновик не публикуется: %v", err)
	}
	if _, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, Approve: true}); err == nil {
		t.Error("без модератора")
	}
	if _, err := e.mod.Decide(ctx, Decision{Subject: "user", ID: 1, ModeratorID: 1, Approve: true}); err == nil {
		t.Error("неизвестное")
	}
	if _, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: 404, ModeratorID: 1, Approve: true}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("нет: %v", err)
	}
	if !domain.CanTransition(domain.StatusPending, domain.StatusPublished, domain.ActorModerator) ||
		domain.CanTransition(domain.StatusPending, domain.StatusPublished, domain.ActorOwner) ||
		domain.CanTransition(domain.StatusDraft, domain.StatusPublished, domain.ActorModerator) ||
		!domain.CanTransition(domain.StatusPublished, domain.StatusDraft, domain.ActorOwner) ||
		!domain.CanTransition(domain.StatusPending, domain.StatusPublished, domain.ActorSystem) ||
		domain.CanTransition(domain.StatusPending, domain.StatusRejected, domain.ActorSystem) ||
		domain.CanTransition(domain.StatusDraft, domain.StatusPublished, domain.ActorSystem) ||
		domain.CanTransition(domain.StatusPending, domain.StatusPublished, 0) {
		t.Error("таблица переходов")
	}
}

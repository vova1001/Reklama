package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/repository"
)

func newUser(t *testing.T, a *repository.Auth, email string) domain.User {
	t.Helper()
	u, err := a.CreateUserWithIdentity(context.Background(), domain.NewUser{Name: "Тест"}, domain.Identity{Provider: "email", Subject: email})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func addMedia(t *testing.T, m *repository.Media, id string, owner int64, purpose string) {
	t.Helper()
	if _, err := m.CreateMedia(context.Background(), domain.Media{ID: id, OwnerID: owner, Purpose: purpose, Width: 10, Height: 10, Bytes: 100}); err != nil {
		t.Fatal(err)
	}
}

func TestListingsTasks(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	auth := repository.NewAuth(pool)
	r := repository.NewListings(pool)
	media := repository.NewMedia(pool)
	u := newUser(t, auth, "biz@x.ru")
	other := newUser(t, auth, "o@x.ru")
	addMedia(t, media, "cover00000000000000000", u.ID, domain.PurposeTaskCover)
	addMedia(t, media, "photo10000000000000000", u.ID, domain.PurposeTaskPhoto)
	addMedia(t, media, "photo20000000000000000", u.ID, domain.PurposeTaskPhoto)

	// черновик: пустой — можно; с фото и блоками — всё сохраняется и читается
	empty, err := r.CreateTask(ctx, domain.Task{OwnerID: u.ID, Access: domain.AccessOpen})
	if err != nil || empty.Status != domain.StatusDraft || empty.Category != "" || empty.PublishBy != nil || empty.Blocks == nil {
		t.Fatalf("%v %+v", err, empty)
	}
	day := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	future := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	in := domain.Task{
		OwnerID: u.ID, Title: "Сторис", City: "Казань", Category: "food", Formats: []string{"reels"}, Access: domain.AccessApproval,
		MinViews: 5000, RewardKopecks: 300000, PublishBy: &day, PublishTime: "18:30", DeadlineAt: &future,
		Blocks: []domain.Block{{Kind: domain.BlockList, Title: "Что показать", Items: []string{"Интерьер", "Меню"}}},
		Cover:  "cover00000000000000000", Photos: []string{"photo20000000000000000", "photo10000000000000000"},
	}
	tk, err := r.CreateTask(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.OwnTask(ctx, u.ID, tk.ID)
	if err != nil || got.Title != "Сторис" || !got.PublishBy.Equal(day) || got.Blocks[0].Items[1] != "Меню" ||
		got.PublishTime != "18:30" || !got.DeadlineAt.Equal(future) || got.Cover != in.Cover || got.Photos[0] != "photo20000000000000000" || got.Access != domain.AccessApproval {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := r.OwnTask(ctx, other.ID, tk.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("чужое: %v", err)
	}
	if _, err := r.CreateTask(ctx, domain.Task{OwnerID: u.ID, Access: "open", Category: "cars"}); err == nil {
		t.Error("сфера вне справочника (FK)")
	}
	// фото уже прикреплено к первому заданию — к другому не прикрепить (транзакция откатится)
	if _, err := r.UpdateTask(ctx, domain.Task{ID: empty.ID, OwnerID: u.ID, Access: "open", Photos: []string{"photo10000000000000000"}}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("чужое прикреплённое: %v", err)
	}
	// убрали фото из задания — оно открепилось
	in.ID, in.Photos = tk.ID, []string{"photo10000000000000000"}
	if _, err := r.UpdateTask(ctx, in); err != nil {
		t.Fatal(err)
	}
	ms, _ := media.OwnMedia(ctx, u.ID, []string{"photo20000000000000000", "photo10000000000000000"})
	for _, m := range ms {
		if (m.ID == "photo20000000000000000") != (m.Subject == "") {
			t.Errorf("прикрепление: %+v", m)
		}
	}
	if n, _ := r.CountTasks(ctx, u.ID); n != 2 {
		t.Errorf("число: %d", n)
	}
	list, _ := r.OwnTasks(ctx, u.ID, 10)
	if len(list) != 2 || list[0].ID != tk.ID {
		t.Errorf("порядок: %+v", list)
	}

	// статусы: только из ожидаемого; журнал
	sub, err := r.SetTaskStatus(ctx, domain.StatusChange{SubjectID: tk.ID, From: domain.StatusDraft, To: domain.StatusPending, ActorID: u.ID})
	if err != nil || sub.Status != domain.StatusPending || sub.SubmittedAt == nil {
		t.Fatalf("%v %+v", err, sub)
	}
	if _, err := r.SetTaskStatus(ctx, domain.StatusChange{SubjectID: tk.ID, From: domain.StatusDraft, To: domain.StatusPending, ActorID: u.ID}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("гонка: %v", err)
	}
	if _, err := r.UpdateTask(ctx, in); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("правка на проверке: %v", err)
	}
	if err := r.DeleteTask(ctx, u.ID, tk.ID); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("удаление на проверке: %v", err)
	}
	n, first, err := r.SubmitStats(ctx, u.ID, time.Now().Add(-time.Hour))
	if err != nil || n != 1 || first.IsZero() {
		t.Errorf("отправки: %v %d %v", err, n, first)
	}
	rej, err := r.SetTaskStatus(ctx, domain.StatusChange{SubjectID: tk.ID, From: domain.StatusPending, To: domain.StatusRejected, Reason: "Срок", ActorID: other.ID})
	if err != nil || rej.RejectReason != "Срок" {
		t.Fatalf("%v %+v", err, rej)
	}
	_, _ = r.SetTaskStatus(ctx, domain.StatusChange{SubjectID: tk.ID, From: domain.StatusRejected, To: domain.StatusPending, ActorID: u.ID})
	pub, err := r.SetTaskStatus(ctx, domain.StatusChange{SubjectID: tk.ID, From: domain.StatusPending, To: domain.StatusPublished, ActorID: other.ID})
	if err != nil || pub.PublishedAt == nil || pub.RejectReason != "" {
		t.Fatalf("%v %+v", err, pub)
	}
	h, _ := r.History(ctx, domain.SubjectTask, tk.ID)
	if len(h) != 4 || h[1].Reason != "Срок" || h[1].ActorID != other.ID || h[3].To != domain.StatusPublished {
		t.Errorf("журнал: %+v", h)
	}
	// опубликованное фото видно всем, черновое — нет
	if a, _ := media.Access(ctx, "photo10000000000000000"); !a.Public {
		t.Error("фото опубликованного — всем")
	}
	if a, _ := media.Access(ctx, "photo20000000000000000"); a.Public {
		t.Error("откреплённое — не всем")
	}

	// лента: название бизнеса и сферы, курсор
	_, _ = repository.NewProfiles(pool).SaveBusinessProfile(ctx, u.ID, domain.BusinessProfile{Name: "Кофейня", City: "Казань", Spheres: []string{"food"}})
	feed, err := r.PublishedTasks(ctx, domain.FeedFilter{}, nil, 10)
	if err != nil || len(feed) != 1 || feed[0].Brand != "Кофейня" || feed[0].CategoryTitle != "Еда и кафе" {
		t.Fatalf("%v %+v", err, feed)
	}
	next, _ := r.PublishedTasks(ctx, domain.FeedFilter{}, &domain.FeedCursor{PublishedAt: *feed[0].PublishedAt, ID: feed[0].ID}, 10)
	if len(next) != 0 {
		t.Errorf("после курсора: %+v", next)
	}
	// отбор: город без регистра, сфера, формат
	for f, want := range map[domain.FeedFilter]int{
		{City: "казань"}: 1, {City: "Москва"}: 0, {Category: "food", Format: "reels"}: 1, {Category: "beauty"}: 0, {Format: "tiktok"}: 0,
	} {
		if got, err := r.PublishedTasks(ctx, f, nil, 10); err != nil || len(got) != want {
			t.Errorf("отбор %+v: %v %d", f, err, len(got))
		}
	}
	if one, err := r.PublishedTask(ctx, tk.ID); err != nil || one.Brand != "Кофейня" || one.CategoryTitle != "Еда и кафе" {
		t.Errorf("страница задания: %v %+v", err, one)
	}
	if _, err := r.PublishedTask(ctx, empty.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("черновик не опубликован: %v", err)
	}

	// автоматическая проверка: цепочка «→ на проверке → опубликовано» одной транзакцией; «Вся Россия» — любому городу
	auto, _ := r.CreateTask(ctx, domain.Task{OwnerID: u.ID, Title: "Вся страна", Nationwide: true, Access: "open", Formats: []string{"tiktok"}, DeadlineAt: &future})
	chain := domain.SubmitChain(domain.ModerationAuto, domain.SubjectTask, auto.ID, domain.StatusDraft, u.ID)
	ap, err := r.SetTaskStatus(ctx, chain...)
	if err != nil || ap.Status != domain.StatusPublished || ap.SubmittedAt == nil || ap.PublishedAt == nil || ap.RejectReason != "" {
		t.Fatalf("авто: %v %+v", err, ap)
	}
	ah, _ := r.History(ctx, domain.SubjectTask, auto.ID)
	if len(ah) != 2 || ah[0].Auto || ah[0].ActorID != u.ID || !ah[1].Auto || ah[1].ActorID != 0 || ah[1].Reason != domain.AutoApproveNote {
		t.Errorf("журнал авто: %+v", ah)
	}
	if n, _, _ := r.SubmitStats(ctx, u.ID, time.Now().Add(-time.Hour)); n != 3 {
		t.Errorf("отправки считаются и в авто: %d", n)
	}
	if got, _ := r.PublishedTasks(ctx, domain.FeedFilter{City: "Тверь"}, nil, 10); len(got) != 1 || got[0].ID != auto.ID {
		t.Errorf("«Вся Россия»: %+v", got)
	}
	// цепочка с неверным вторым шагом откатывается целиком
	again, _ := r.CreateTask(ctx, domain.Task{OwnerID: u.ID, Access: "open"})
	bad := []domain.StatusChange{
		{SubjectID: again.ID, From: domain.StatusDraft, To: domain.StatusPending, ActorID: u.ID},
		{SubjectID: again.ID, From: domain.StatusRejected, To: domain.StatusPublished, Auto: true},
	}
	if _, err := r.SetTaskStatus(ctx, bad...); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("откат: %v", err)
	}
	if g, _ := r.OwnTask(ctx, u.ID, again.ID); g.Status != domain.StatusDraft {
		t.Errorf("частично: %+v", g)
	}
	if h, _ := r.History(ctx, domain.SubjectTask, again.ID); len(h) != 0 {
		t.Errorf("журнал после отката: %+v", h)
	}
	if _, err := r.SetTaskStatus(ctx); err == nil {
		t.Error("пустая цепочка")
	}
	// срок прошёл: из ленты и со страницы — сразу; фоновая задача — в «завершено» с журналом
	past := time.Now().Add(-time.Minute)
	old, _ := r.CreateTask(ctx, domain.Task{OwnerID: u.ID, Title: "Старое", Access: "open", DeadlineAt: &past})
	_, _ = r.SetTaskStatus(ctx, domain.SubmitChain(domain.ModerationAuto, domain.SubjectTask, old.ID, domain.StatusDraft, u.ID)...)
	if _, err := r.PublishedTask(ctx, old.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("просроченное на странице: %v", err)
	}
	for _, x := range func() []domain.Task { l, _ := r.PublishedTasks(ctx, domain.FeedFilter{}, nil, 50); return l }() {
		if x.ID == old.ID {
			t.Error("просроченное в ленте")
		}
	}
	ids, err := r.ExpireTasks(ctx, time.Now())
	if err != nil || len(ids) != 1 || ids[0] != old.ID {
		t.Fatalf("завершение: %v %v", err, ids)
	}
	if g, _ := r.OwnTask(ctx, u.ID, old.ID); g.Status != domain.StatusExpired {
		t.Errorf("статус: %v", g.Status)
	}
	if h, _ := r.History(ctx, domain.SubjectTask, old.ID); len(h) != 3 || h[2].To != domain.StatusExpired || !h[2].Auto {
		t.Errorf("журнал: %+v", h)
	}
	// снять с публикации
	if d, err := r.SetTaskStatus(ctx, domain.StatusChange{SubjectID: auto.ID, From: domain.StatusPublished, To: domain.StatusDraft, ActorID: u.ID}); err != nil || d.Status != domain.StatusDraft {
		t.Errorf("снять: %v", err)
	}

	// удаление черновика открепляет его фото
	addMedia(t, media, "photo30000000000000000", u.ID, domain.PurposeTaskPhoto)
	_, _ = r.UpdateTask(ctx, domain.Task{ID: empty.ID, OwnerID: u.ID, Access: "open", Photos: []string{"photo30000000000000000"}})
	if err := r.DeleteTask(ctx, u.ID, empty.ID); err != nil {
		t.Fatal(err)
	}
	if ms, _ := media.OwnMedia(ctx, u.ID, []string{"photo30000000000000000"}); ms[0].Subject != "" {
		t.Error("фото удалённого задания открепилось")
	}
	if err := r.DeleteTask(ctx, u.ID, empty.ID); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("повтор: %v", err)
	}
}

func TestListingsCards(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	auth := repository.NewAuth(pool)
	r := repository.NewListings(pool)
	media := repository.NewMedia(pool)
	u := newUser(t, auth, "c@x.ru")
	addMedia(t, media, "avatar0000000000000000", u.ID, domain.PurposeCardAvatar)
	addMedia(t, media, "work100000000000000000", u.ID, domain.PurposeCardWork)
	if _, err := r.Card(ctx, u.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("нет: %v", err)
	}
	c, err := r.SaveCard(ctx, domain.CreatorCard{
		UserID: u.ID, Name: "Алина", City: "Москва", Topics: []string{"beauty"},
		Platforms: []domain.Platform{{Kind: "youtube", URL: "https://youtube.com/@a", Followers: 1200}},
		Blocks:    []domain.Block{{Kind: domain.BlockText, Title: "О себе", Items: []string{"Привет"}}},
		Avatar:    "avatar0000000000000000", Works: []string{"work100000000000000000"},
	})
	if err != nil || c.Status != domain.StatusDraft || c.Platforms[0].Followers != 1200 || c.Avatar == "" || len(c.Works) != 1 {
		t.Fatalf("%v %+v", err, c)
	}
	c.Name = "Алина К"
	if c, err = r.SaveCard(ctx, c); err != nil || c.Name != "Алина К" {
		t.Fatalf("правка: %v %+v", err, c)
	}
	if _, err := r.SetCardStatus(ctx, domain.StatusChange{SubjectID: u.ID, From: domain.StatusDraft, To: domain.StatusPending, ActorID: u.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SaveCard(ctx, c); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("на проверке не правится: %v", err)
	}
	if a, _ := media.Access(ctx, "work100000000000000000"); a.Public {
		t.Error("до публикации — только владельцу")
	}
	_, _ = r.SetCardStatus(ctx, domain.StatusChange{SubjectID: u.ID, From: domain.StatusPending, To: domain.StatusPublished, ActorID: u.ID})
	if a, _ := media.Access(ctx, "work100000000000000000"); !a.Public || a.OwnerID != u.ID {
		t.Errorf("после публикации — всем: %+v", a)
	}
	h, _ := r.History(ctx, domain.SubjectCard, u.ID)
	if len(h) != 2 {
		t.Errorf("журнал: %+v", h)
	}

	// лента анкет: названия тем по порядку, отбор, курсор (user_id)
	u2 := newUser(t, auth, "c2@x.ru")
	if _, err := r.SaveCard(ctx, domain.CreatorCard{UserID: u2.ID, Name: "Борис", City: "Казань", Topics: []string{"sport", "food"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SetCardStatus(ctx, domain.SubmitChain(domain.ModerationAuto, domain.SubjectCard, u2.ID, domain.StatusDraft, u2.ID)...); err != nil {
		t.Fatal(err)
	}
	cards, err := r.PublishedCards(ctx, domain.FeedFilter{}, nil, 10)
	if err != nil || len(cards) != 2 || cards[0].UserID != u2.ID || len(cards[0].TopicTitles) != 2 || cards[0].TopicTitles[0] != "Спорт и фитнес" {
		t.Fatalf("лента анкет: %v %+v", err, cards)
	}
	page2, _ := r.PublishedCards(ctx, domain.FeedFilter{}, &domain.FeedCursor{PublishedAt: *cards[0].PublishedAt, ID: cards[0].UserID}, 10)
	if len(page2) != 1 || page2[0].UserID != u.ID {
		t.Errorf("курсор: %+v", page2)
	}
	for f, want := range map[domain.FeedFilter]int{{Category: "food"}: 1, {Category: "beauty"}: 1, {City: "москва"}: 1, {City: "Тверь"}: 0} {
		if got, _ := r.PublishedCards(ctx, f, nil, 10); len(got) != want {
			t.Errorf("отбор %+v: %d", f, len(got))
		}
	}
	if one, err := r.PublishedCard(ctx, u.ID); err != nil || one.Name != "Алина К" || one.TopicTitles[0] != "Красота и салоны" {
		t.Errorf("страница анкеты: %v %+v", err, one)
	}
	// снять с публикации — из ленты пропадает
	if _, err := r.SetCardStatus(ctx, domain.StatusChange{SubjectID: u2.ID, From: domain.StatusPublished, To: domain.StatusDraft, ActorID: u2.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PublishedCard(ctx, u2.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("снятая: %v", err)
	}
	if _, err := r.SetCardStatus(ctx); err == nil {
		t.Error("пустая цепочка")
	}
}

func TestCategoriesRepo(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	auth := repository.NewAuth(pool)
	r := repository.NewCategories(pool)
	u := newUser(t, auth, "cat@x.ru")
	other := newUser(t, auth, "cat2@x.ru")
	common, err := r.Common(ctx)
	if err != nil || len(common) != 12 {
		t.Fatalf("%v %d", err, len(common))
	}
	if c, err := r.FindByNorm(ctx, u.ID, "еда и кафе"); err != nil || c.Slug != "food" || c.Own() {
		t.Fatalf("норма из миграции: %v %+v", err, c)
	}
	c, err := r.CreateCustom(ctx, u.ID, "u-aaa", "Настолки", "настолки")
	if err != nil || c.Status != domain.CategoryPending || c.AuthorID != u.ID {
		t.Fatalf("%v %+v", err, c)
	}
	if _, err := r.CreateCustom(ctx, u.ID, "u-bbb", "НАСТОЛКИ", "настолки"); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("дубль у автора: %v", err)
	}
	if _, err := r.CreateCustom(ctx, other.ID, "u-ccc", "Настолки", "настолки"); err != nil {
		t.Errorf("у другого — можно: %v", err)
	}
	if n, custom, _ := r.Usable(ctx, u.ID, []string{"u-aaa", "food", "u-ccc"}); n != 2 || custom != 1 {
		t.Errorf("годные: %d %d", n, custom)
	}
	if home, _ := repository.NewHome(pool).Categories(ctx); len(home) != 12 {
		t.Error("своя на проверке не попала на главную")
	}
	if n, _ := r.CountPending(ctx, u.ID); n != 1 {
		t.Error("на проверке")
	}
	// одобрить → общая (и на главной); вторую такую же одобрить нельзя
	got, err := r.SetCategoryStatus(ctx, domain.StatusChange{SubjectID: c.ID, From: domain.CategoryPending, To: domain.CategoryApproved, ActorID: other.ID})
	if err != nil || got.Status != domain.CategoryApproved {
		t.Fatalf("%v %+v", err, got)
	}
	if home, _ := repository.NewHome(pool).Categories(ctx); len(home) != 13 {
		t.Error("одобренная — на главной")
	}
	dup, _ := r.FindByNorm(ctx, other.ID, "настолки")
	if dup.Status != domain.CategoryApproved {
		t.Errorf("теперь находится общая: %+v", dup)
	}
	own, _ := r.ByAuthor(ctx, other.ID)
	if _, err := r.SetCategoryStatus(ctx, domain.StatusChange{SubjectID: own[0].ID, From: domain.CategoryPending, To: domain.CategoryApproved}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("дубль общей: %v", err)
	}
	if _, err := r.SetCategoryStatus(ctx, domain.StatusChange{SubjectID: c.ID, From: domain.CategoryPending, To: domain.CategoryRejected}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("уже одобрена: %v", err)
	}
}

func TestMediaRepo(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	auth := repository.NewAuth(pool)
	m := repository.NewMedia(pool)
	u := newUser(t, auth, "m@x.ru")
	addMedia(t, m, "ava0000000000000000000", u.ID, domain.PurposeAvatar)
	addMedia(t, m, "ava1000000000000000000", u.ID, domain.PurposeAvatar)
	addMedia(t, m, "pht0000000000000000000", u.ID, domain.PurposeTaskPhoto)
	if n, b, _ := m.Usage(ctx, u.ID); n != 3 || b != 300 {
		t.Errorf("квота: %d %d", n, b)
	}
	if _, err := m.Access(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Error("нет")
	}
	if err := m.SetAvatar(ctx, u.ID, "pht0000000000000000000", "/x"); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("не аватар: %v", err)
	}
	if err := m.SetAvatar(ctx, u.ID, "ava0000000000000000000", "/media/ava0000000000000000000/320.webp"); err != nil {
		t.Fatal(err)
	}
	if a, _ := m.Access(ctx, "ava0000000000000000000"); !a.Public {
		t.Error("аватар — всем")
	}
	got, _ := auth.UserByID(ctx, u.ID)
	if got.AvatarURL != "/media/ava0000000000000000000/320.webp" {
		t.Errorf("адрес: %q", got.AvatarURL)
	}
	_ = m.SetAvatar(ctx, u.ID, "ava1000000000000000000", "/media/ava1000000000000000000/320.webp")
	if a, _ := m.Access(ctx, "ava0000000000000000000"); a.Public {
		t.Error("прежний аватар откреплён")
	}
	if err := m.DeleteLoose(ctx, u.ID, "ava1000000000000000000"); !errors.Is(err, domain.ErrNotFound) {
		t.Error("прикреплённое не удаляется")
	}
	if err := m.DeleteLoose(ctx, u.ID, "pht0000000000000000000"); err != nil {
		t.Fatal(err)
	}
	// сироты: только неприкреплённые старше срока
	ids, err := m.Orphans(ctx, time.Now().Add(time.Minute), 10)
	if err != nil || len(ids) != 1 || ids[0] != "ava0000000000000000000" {
		t.Fatalf("%v %v", err, ids)
	}
	if ids, _ := m.Orphans(ctx, time.Now().Add(-time.Hour), 10); len(ids) != 0 {
		t.Error("рано")
	}
	if err := m.DeleteMedia(ctx, []string{"ava0000000000000000000", "ava1000000000000000000"}); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := m.Usage(ctx, u.ID); n != 1 {
		t.Errorf("прикреплённый аватар не удаляется очисткой: %d", n)
	}
	if err := m.SetAvatar(ctx, u.ID, "", ""); err != nil {
		t.Fatal(err)
	}
}

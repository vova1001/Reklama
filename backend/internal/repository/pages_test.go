package repository_test

import (
	"errors"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/repository"
	"ohvat/backend/internal/service"
)

// Страницы на настоящей PostgreSQL: сохранение с файлами, адреса (прежний ведёт на новый и не достаётся другому),
// очередь проверки (страница и правка), решение модератора, кому видны файлы.
func TestPagesFlow(t *testing.T) {
	e := newAdminEnv(t)
	pages := repository.NewPages(e.pool)
	media := repository.NewMedia(e.pool)
	deps := service.ListingDeps{Categories: e.cats, Submits: e.list, Media: media, Clock: e.clock, Mode: domain.ModerationManual}
	svc := service.NewPages(pages, service.PagesDeps{ListingDeps: deps})
	e.mod.WithPages(pages)
	review := service.NewReview(service.ReviewDeps{Repo: e.admin, Content: modRepo{e.list, e.cats}, Moderator: e.mod, Notifier: e,
		Clock: e.clock, Pages: pages}, service.ReviewConfig{})
	biz := e.user(t, "biz@x.ru", domain.RoleNone)
	other := e.user(t, "other@x.ru", domain.RoleNone)
	e.user(t, "root@x.ru", domain.RoleAdmin) // модератора назначает администратор (последнего не снять)
	mod := e.user(t, "mod@x.ru", domain.RoleModerator)
	addMedia(t, media, "logo000000000000000000", biz.ID, domain.PurposePageLogo)
	addMedia(t, media, "photo10000000000000000", biz.ID, domain.PurposePagePhoto)
	addMedia(t, media, "photo20000000000000000", biz.ID, domain.PurposePagePhoto)
	addMedia(t, media, "video00000000000000000", biz.ID, domain.PurposePageVideo)

	in := service.PageInput{Title: "Кофейня Зерно", City: "Казань", Spheres: []string{"food"}, Logo: "logo000000000000000000",
		Blocks: []service.PageBlockInput{
			{Type: domain.PBAbout, Text: "Варим кофе"},
			{Type: domain.PBPhotos, Photos: []string{"photo10000000000000000"}},
			{Type: domain.PBVideo, Videos: []service.VideoInput{{Media: "video00000000000000000"}}},
			{Type: domain.PBContacts, Contacts: &domain.PageContacts{Address: "Баумана, 1", Socials: []string{"vk.com/zerno"}}},
		}}
	p, err := svc.Save(e.ctx, biz, domain.PageBusiness, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pages.Page(e.ctx, biz.ID, domain.PageBusiness)
	if err != nil || got.Slug != "kofeynya-zerno" || got.Blocks[2].Videos[0].Media != "video00000000000000000" ||
		got.Blocks[3].Contacts.Socials[0] != "https://vk.com/zerno" || got.Logo != "logo000000000000000000" {
		t.Fatalf("%+v %v", got, err)
	}
	if a, _ := media.Access(e.ctx, "photo10000000000000000"); a.Public {
		t.Fatal("draft photo public")
	}
	// чужому — файлы этой страницы не прикрепить
	addMedia(t, media, "ophoto0000000000000000", other.ID, domain.PurposePagePhoto)
	if _, err := svc.Save(e.ctx, other, domain.PageBusiness, service.PageInput{Title: "Кофейня Зерно",
		Blocks: []service.PageBlockInput{{Type: domain.PBPhotos, Photos: []string{"photo10000000000000000"}}}}); err == nil {
		t.Fatal("foreign photo attached")
	}
	op, err := svc.Save(e.ctx, other, domain.PageBusiness, service.PageInput{Title: "Кофейня Зерно"})
	if err != nil || op.Slug != "kofeynya-zerno-2" {
		t.Fatalf("second slug: %+v %v", op, err)
	}
	// адреса: смена, прежний — за страницей, чужой — нельзя
	if _, err := svc.SetSlug(e.ctx, biz, domain.PageBusiness, "zerno"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetSlug(e.ctx, other, domain.PageBusiness, "kofeynya-zerno"); err == nil {
		t.Fatal("old slug taken by other")
	}
	if _, err := svc.SetSlug(e.ctx, other, domain.PageBusiness, "zerno"); err == nil {
		t.Fatal("current slug taken by other")
	}

	// на проверку → в очереди → решение модератора → опубликовано, файлы видны всем
	if _, err := svc.Submit(e.ctx, biz, domain.PageBusiness); err != nil {
		t.Fatal(err)
	}
	id := itoa(p.ID)
	if _, err := review.Claim(e.ctx, mod, domain.KindPage, id); err != nil {
		t.Fatal(err)
	}
	item, err := review.Item(e.ctx, mod, domain.KindPage, id)
	if err != nil || !item.InQueue || item.Page == nil || item.Page.SphereTitles[0] != "Еда и кафе" {
		t.Fatalf("item: %+v %v", item, err)
	}
	if _, err := review.Decide(e.ctx, mod, domain.KindPage, id, service.DecisionInput{Action: domain.ActionApprove}); err != nil {
		t.Fatal(err)
	}
	e.clock.add(11 * time.Second)
	if n, err := review.ApplyDue(e.ctx); err != nil || n != 1 {
		t.Fatalf("apply: %d %v", n, err)
	}
	pub, err := svc.Public(e.ctx, domain.PageBusiness, "zerno")
	if err != nil || pub.Page.Status != domain.StatusPublished || pub.Page.SphereTitles[0] != "Еда и кафе" {
		t.Fatalf("public: %+v %v", pub, err)
	}
	if old, err := svc.Public(e.ctx, domain.PageBusiness, "kofeynya-zerno"); err != nil || old.MovedTo != "zerno" {
		t.Fatalf("moved: %+v %v", old, err)
	}
	for _, f := range []string{"logo000000000000000000", "photo10000000000000000", "video00000000000000000"} {
		if a, _ := media.Access(e.ctx, f); !a.Public {
			t.Fatalf("%s not public after publish", f)
		}
	}
	if m, err := pages.PublishedSlugs(e.ctx, domain.PageBusiness, []int64{biz.ID, other.ID}); err != nil || m[biz.ID] != "zerno" || len(m) != 1 {
		t.Fatalf("slugs: %v %v", m, err)
	}

	// правка: новая версия в очереди, старая видна; фото правки — не всем, пока не одобрят
	in.Blocks[1].Photos = []string{"photo20000000000000000"}
	in.Title = "Зерно"
	if _, err := svc.Save(e.ctx, biz, domain.PageBusiness, in); err != nil {
		t.Fatal(err)
	}
	if a, _ := media.Access(e.ctx, "photo20000000000000000"); a.Public {
		t.Fatal("revision photo public")
	}
	if _, err := svc.Submit(e.ctx, biz, domain.PageBusiness); err != nil {
		t.Fatal(err)
	}
	q, err := e.admin.QueueItem(e.ctx, domain.KindPageRevision, id)
	if err != nil || q.Title != "Зерно" || q.OwnerID != biz.ID {
		t.Fatalf("queue revision: %+v %v", q, err)
	}
	if pub, _ := svc.Public(e.ctx, domain.PageBusiness, "zerno"); pub.Page.Title != "Кофейня Зерно" {
		t.Fatalf("old version must be visible: %q", pub.Page.Title)
	}
	review.Claim(e.ctx, mod, domain.KindPageRevision, id)
	if _, err := review.Decide(e.ctx, mod, domain.KindPageRevision, id, service.DecisionInput{Action: domain.ActionApprove}); err != nil {
		t.Fatal(err)
	}
	e.clock.add(11 * time.Second)
	if n, err := review.ApplyDue(e.ctx); err != nil || n != 1 {
		t.Fatalf("apply revision: %d %v", n, err)
	}
	if pub, _ := svc.Public(e.ctx, domain.PageBusiness, "zerno"); pub.Page.Title != "Зерно" || pub.Page.Blocks[1].Photos[0] != "photo20000000000000000" {
		t.Fatalf("revision not applied: %+v", pub.Page)
	}
	if a, _ := media.Access(e.ctx, "photo20000000000000000"); !a.Public {
		t.Fatal("approved photo not public")
	}
	if a, _ := media.Access(e.ctx, "photo10000000000000000"); a.Public {
		t.Fatal("removed photo still public")
	}

	// открытые задания бизнеса — на его странице
	if ts, err := pages.OpenTasks(e.ctx, biz.ID, 12); err != nil || len(ts) != 0 {
		t.Fatalf("tasks: %v %v", ts, err)
	}
	// заблокированный владелец — страницы не видно
	if _, err := e.admin.BlockUser(e.ctx, biz.ID, "спам", e.clock.Now(), domain.AuditEntry{Action: domain.AuditUserBlock}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Public(e.ctx, domain.PageBusiness, "zerno"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("blocked visible: %v", err)
	}
}

func TestPagesSlugLimitAndCreator(t *testing.T) {
	e := newAdminEnv(t)
	pages := repository.NewPages(e.pool)
	u := e.user(t, "c@x.ru", domain.RoleNone)
	p, err := pages.CreatePage(e.ctx, domain.Page{UserID: u.ID, Kind: domain.PageCreator, Slug: "anna",
		Blocks: []domain.PageBlock{{Type: domain.PBWorks, Links: []string{"https://t.me/a/1"},
			Videos: []domain.PageVideo{{Source: domain.VideoVK, OID: "-1", ID: "2", Hash: "ab"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if v := p.Blocks[0].Videos[0]; v.EmbedURL() != "https://vk.com/video_ext.php?oid=-1&id=2&hash=ab" {
		t.Fatalf("%+v", v)
	}
	// тот же адрес у бизнеса — можно (виды не пересекаются), у автора — нет
	if _, err := pages.CreatePage(e.ctx, domain.Page{UserID: u.ID, Kind: domain.PageBusiness, Slug: "anna"}); err != nil {
		t.Fatal(err)
	}
	other := e.user(t, "d@x.ru", domain.RoleNone)
	if _, err := pages.CreatePage(e.ctx, domain.Page{UserID: other.ID, Kind: domain.PageCreator, Slug: "anna"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("dup slug: %v", err)
	}
	for i := 0; i < 9; i++ {
		if _, err := pages.SetPageSlug(e.ctx, p.ID, domain.PageCreator, "anna-"+itoa(int64(i)), 10); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := pages.SetPageSlug(e.ctx, p.ID, domain.PageCreator, "anna-new", 10); !errors.Is(err, domain.ErrSlugLimit) {
		t.Fatalf("limit: %v", err)
	}
	if got, err := pages.SetPageSlug(e.ctx, p.ID, domain.PageCreator, "anna", 10); err != nil || got.Slug != "anna" {
		t.Fatalf("back to own old: %+v %v", got, err)
	}
	if ok, _ := pages.SlugTaken(e.ctx, domain.PageCreator, "anna-3"); !ok {
		t.Fatal("old slug must stay taken")
	}
	if _, err := pages.Page(e.ctx, other.ID, domain.PageCreator); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("no page")
	}
}

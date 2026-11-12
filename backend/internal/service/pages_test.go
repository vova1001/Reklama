package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
)

type pagesEnv struct {
	*listingEnv
	pages *memPages
	svc   *Pages
	cards fakePublicCards
}

func newPagesEnv(mode domain.ModerationMode) *pagesEnv {
	e := &pagesEnv{listingEnv: newListingEnv(ListingLimits{SubmitsPerDay: 50})}
	e.pages = newMemPages(e.clock, e.repo, e.media)
	e.cards = fakePublicCards{}
	d := ListingDeps{Categories: e.cats, Submits: e.repo, Media: e.media, Clock: e.clock, Platforms: e.plats,
		Limits: ListingLimits{SubmitsPerDay: 50}, Mode: mode}
	e.svc = NewPages(e.pages, PagesDeps{ListingDeps: d, Cards: e.cards})
	e.mod = NewModeration(e.repo).WithPages(e.pages)
	return e
}

// mid — id файла (22 символа, как настоящий).
func mid(s string) string { return (s + strings.Repeat("A", 22))[:22] }

func (e *pagesEnv) file(owner int64, name, purpose string) string {
	id := mid(name)
	e.media.add(domain.Media{ID: id, OwnerID: owner, Purpose: purpose})
	return id
}

func bizPage(e *pagesEnv) PageInput {
	return PageInput{
		Title: " Кофейня  «Зерно» ", City: "Казань", Spheres: []string{"food"},
		Logo: e.file(biz.ID, "logo", domain.PurposePageLogo), Cover: e.file(biz.ID, "cover", domain.PurposePageCover),
		Blocks: []PageBlockInput{
			{Type: domain.PBAbout, Text: "Варим  кофе\r\n\n\n\nс 2015 года\x07"},
			{Type: domain.PBPhotos, Photos: []string{e.file(biz.ID, "ph1", domain.PurposePagePhoto), e.file(biz.ID, "ph2", domain.PurposePagePhoto)}},
			{Type: domain.PBVideo, Videos: []VideoInput{{URL: "https://youtu.be/dQw4w9WgXcQ"}}},
			{Type: domain.PBVideo, Videos: []VideoInput{{Media: e.file(biz.ID, "vid", domain.PurposePageVideo), Poster: e.file(biz.ID, "post", domain.PurposeVideoPoster)}}},
			{Type: domain.PBContacts, Contacts: &domain.PageContacts{City: "Казань", Address: "Баумана, 1", Hours: "8–22", Site: "zerno.ru", Phone: "+7 900 000-00-00", Socials: []string{"vk.com/zerno", " "}}},
			{Type: domain.PBFacts, Items: []string{"10 лет", " ", "3 кофейни"}},
			{Type: domain.PBText, Title: "Меню", Text: "Сырники"},
		},
	}
}

func TestPagesSaveNewAndShape(t *testing.T) {
	e := newPagesEnv(domain.ModerationAuto)
	ctx := context.Background()
	v, err := e.svc.Get(ctx, biz, domain.PageBusiness, "Кофейня Зерно")
	if err != nil || v.Page != nil || v.SuggestedSlug != "kofeynya-zerno" {
		t.Fatalf("get: %+v %v", v, err)
	}
	p, err := e.svc.Save(ctx, biz, domain.PageBusiness, bizPage(e))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if p.Slug != "kofeynya-zerno" || p.Title != "Кофейня «Зерно»" || p.Status != domain.StatusDraft {
		t.Fatalf("page: %+v", p)
	}
	if p.Blocks[0].Text != "Варим кофе\n\nс 2015 года" {
		t.Fatalf("text: %q", p.Blocks[0].Text)
	}
	if v := p.Blocks[2].Videos[0]; v.Source != domain.VideoYouTube || v.ID != "dQw4w9WgXcQ" || v.EmbedURL() != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ" {
		t.Fatalf("video: %+v", v)
	}
	c := p.Blocks[4].Contacts
	if c.Site != "https://zerno.ru" || len(c.Socials) != 1 || c.Socials[0] != "https://vk.com/zerno" {
		t.Fatalf("contacts: %+v", c)
	}
	if got := p.Blocks[5].Items; len(got) != 2 {
		t.Fatalf("facts: %v", got)
	}
	// файлы прикреплены к странице
	if md := e.media.items[mid("logo")]; md.Subject != "page" || md.SubjectID != p.ID {
		t.Fatalf("logo not attached: %+v", md)
	}
	// повторное сохранение — та же страница, адрес не меняется
	in := bizPage(e)
	in.Slug = "other"
	p2, err := e.svc.Save(ctx, biz, domain.PageBusiness, in)
	if err != nil || p2.ID != p.ID || p2.Slug != p.Slug {
		t.Fatalf("resave: %+v %v", p2, err)
	}
	// второй бизнес с тем же названием — адрес с номером
	other := domain.User{ID: 8}
	in = bizPage(e)
	in.Logo, in.Cover, in.Blocks = "", "", nil
	p3, err := e.svc.Save(ctx, other, domain.PageBusiness, in)
	if err != nil || p3.Slug != "kofeynya-zerno-2" {
		t.Fatalf("second: %+v %v", p3, err)
	}
	// желаемый адрес новой страницы
	in.Slug = "Moy-Adres"
	p4, err := e.svc.Save(ctx, domain.User{ID: 9}, domain.PageBusiness, in)
	if err != nil || p4.Slug != "moy-adres" {
		t.Fatalf("wanted slug: %+v %v", p4, err)
	}
}

func TestPagesShapeErrors(t *testing.T) {
	e := newPagesEnv(domain.ModerationAuto)
	ctx := context.Background()
	foreign := e.file(99, "foreign", domain.PurposePagePhoto)
	wrong := e.file(biz.ID, "wrongp", domain.PurposeTaskPhoto)
	cases := []struct {
		name  string
		in    PageInput
		field string
	}{
		{"чужой блок", PageInput{Blocks: []PageBlockInput{{Type: domain.PBChannels}}}, "blocks.0"},
		{"два «О компании»", PageInput{Blocks: []PageBlockInput{{Type: domain.PBAbout}, {Type: domain.PBAbout}}}, "blocks.1"},
		{"не видеоссылка", PageInput{Blocks: []PageBlockInput{{Type: domain.PBVideo, Videos: []VideoInput{{URL: "https://evil.com/x"}}}}}, "blocks.0"},
		{"два видео", PageInput{Blocks: []PageBlockInput{{Type: domain.PBVideo, Videos: []VideoInput{{URL: "youtu.be/dQw4w9WgXcQ"}, {URL: "youtu.be/dQw4w9WgXcQ"}}}}}, "blocks.0"},
		{"чужое фото", PageInput{Blocks: []PageBlockInput{{Type: domain.PBPhotos, Photos: []string{foreign}}}}, "blocks.0"},
		{"не то назначение", PageInput{Blocks: []PageBlockInput{{Type: domain.PBPhotos, Photos: []string{wrong}}}}, "blocks.0"},
		{"логотип — не логотип", PageInput{Logo: wrong}, "logo"},
		{"сайт", PageInput{Blocks: []PageBlockInput{{Type: domain.PBContacts, Contacts: &domain.PageContacts{Site: "javascript:alert(1)"}}}}, "blocks.0"},
		{"телефон", PageInput{Blocks: []PageBlockInput{{Type: domain.PBContacts, Contacts: &domain.PageContacts{Phone: "звоните"}}}}, "blocks.0"},
		{"длинный текст", PageInput{Blocks: []PageBlockInput{{Type: domain.PBText, Text: strings.Repeat("я", maxPageText+1)}}}, "blocks.0"},
		{"сфера не из списка", PageInput{Spheres: []string{"nope"}}, "spheres"},
		{"много блоков", PageInput{Blocks: make([]PageBlockInput, maxPageBlocks+1)}, "blocks"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := e.svc.Save(ctx, biz, domain.PageBusiness, c.in)
			if f := fields(t, err); f[c.field] == "" {
				t.Fatalf("want error in %s, got %v", c.field, f)
			}
		})
	}
	if _, err := e.svc.Save(ctx, biz, "shop", PageInput{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("bad kind: %v", err)
	}
}

func TestPagesSlug(t *testing.T) {
	e := newPagesEnv(domain.ModerationAuto)
	ctx := context.Background()
	if _, err := e.svc.SetSlug(ctx, biz, domain.PageBusiness, "zerno"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("no page: %v", err)
	}
	p, _ := e.svc.Save(ctx, biz, domain.PageBusiness, bizPage(e))
	if _, err := e.svc.Submit(ctx, biz, domain.PageBusiness); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"ab", "admin", "кофе", "a--b", "-ab", strings.Repeat("a", 41)} {
		if _, err := e.svc.SetSlug(ctx, biz, domain.PageBusiness, bad); fields(t, err)["slug"] == "" {
			t.Fatalf("slug %q accepted", bad)
		}
	}
	p2, err := e.svc.SetSlug(ctx, biz, domain.PageBusiness, " Zerno ")
	if err != nil || p2.Slug != "zerno" {
		t.Fatalf("set: %+v %v", p2, err)
	}
	// прежний адрес ведёт на новый
	pub, err := e.svc.Public(ctx, domain.PageBusiness, p.Slug)
	if err != nil || pub.MovedTo != "zerno" {
		t.Fatalf("moved: %+v %v", pub, err)
	}
	// занятый другим — ошибка поля
	e.svc.Save(ctx, domain.User{ID: 8}, domain.PageBusiness, PageInput{Title: "Другое"})
	if _, err := e.svc.SetSlug(ctx, domain.User{ID: 8}, domain.PageBusiness, p.Slug); fields(t, err)["slug"] == "" {
		t.Fatalf("taken old slug accepted: %v", err)
	}
	// свой прежний — можно вернуть
	if p3, err := e.svc.SetSlug(ctx, biz, domain.PageBusiness, p.Slug); err != nil || p3.Slug != p.Slug {
		t.Fatalf("back: %+v %v", p3, err)
	}
	// не больше 10 адресов
	var last error
	for i := 0; i < 12; i++ {
		_, last = e.svc.SetSlug(ctx, biz, domain.PageBusiness, "zerno-"+strings.Repeat("x", i+1))
	}
	if !strings.Contains(fields(t, last)["slug"], "10") {
		t.Fatalf("limit: %v", last)
	}
}

func TestPagesSubmitAutoAndPublic(t *testing.T) {
	e := newPagesEnv(domain.ModerationAuto)
	ctx := context.Background()
	if _, err := e.svc.Save(ctx, biz, domain.PageBusiness, PageInput{Blocks: []PageBlockInput{{Type: domain.PBAbout}}}); err != nil {
		t.Fatal(err)
	}
	f := fields(t, mustErr(e.svc.Submit(ctx, biz, domain.PageBusiness)))
	for _, k := range []string{"title", "city", "spheres", "blocks"} {
		if f[k] == "" {
			t.Fatalf("submit incomplete: no %s in %v", k, f)
		}
	}
	p, _ := e.svc.Save(ctx, biz, domain.PageBusiness, bizPage(e))
	if _, err := e.svc.Public(ctx, domain.PageBusiness, p.Slug); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("draft visible: %v", err)
	}
	e.pages.tasks[biz.ID] = []domain.Task{{ID: 1, Title: "А"}, {ID: 2, Title: "Б"}}
	out, err := e.svc.Submit(ctx, biz, domain.PageBusiness)
	if err != nil || out.Status != domain.StatusPublished {
		t.Fatalf("auto: %+v %v", out, err)
	}
	pub, err := e.svc.Public(ctx, domain.PageBusiness, strings.ToUpper(p.Slug))
	if err != nil || pub.MovedTo != "" || len(pub.Tasks) != 2 || pub.Tasks[0].Brand != "Кофейня «Зерно»" {
		t.Fatalf("public: %+v %v", pub, err)
	}
	// ссылки из ленты
	m, _ := e.svc.PublishedSlugs(ctx, domain.PageBusiness, []int64{biz.ID, biz.ID, 99})
	if m[biz.ID] != p.Slug || len(m) != 1 {
		t.Fatalf("slugs: %v", m)
	}
	// снять с публикации — пропадает
	if _, err := e.svc.Withdraw(ctx, biz, domain.PageBusiness); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Public(ctx, domain.PageBusiness, p.Slug); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("withdrawn visible: %v", err)
	}
	if _, err := e.svc.Public(ctx, domain.PageBusiness, strings.Repeat("a", 60)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("long slug")
	}
}

func mustErr(_ domain.Page, err error) error { return err }

func TestPagesManualReviewAndRevision(t *testing.T) {
	e := newPagesEnv(domain.ModerationManual)
	ctx := context.Background()
	p, _ := e.svc.Save(ctx, biz, domain.PageBusiness, bizPage(e))
	out, err := e.svc.Submit(ctx, biz, domain.PageBusiness)
	if err != nil || out.Status != domain.StatusPending {
		t.Fatalf("manual: %+v %v", out, err)
	}
	if _, err := e.svc.Save(ctx, biz, domain.PageBusiness, bizPage(e)); !errors.Is(err, ErrWrongStatus) {
		t.Fatalf("edit pending: %v", err)
	}
	// на доработку — замечание к полю страницы; к чужому полю — нельзя
	if _, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectPage, ID: p.ID, ModeratorID: 1, Action: domain.ActionRework, Notes: map[string]string{"minViews": "что-то"}}); fields(t, err)["notes"] == "" {
		t.Fatalf("foreign field note: %v", err)
	}
	st, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectPage, ID: p.ID, ModeratorID: 1, Action: domain.ActionRework, Notes: map[string]string{"cover": "Обложка не по теме"}})
	if err != nil || st != domain.StatusChangesRequested {
		t.Fatalf("rework: %v %v", st, err)
	}
	if _, err := e.svc.Save(ctx, biz, domain.PageBusiness, bizPage(e)); err != nil {
		t.Fatal(err)
	}
	e.svc.Submit(ctx, biz, domain.PageBusiness)
	if st, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectPage, ID: p.ID, ModeratorID: 1, Approve: true}); err != nil || st != domain.StatusPublished {
		t.Fatalf("approve: %v %v", st, err)
	}
	// правка опубликованной: прежняя видна, пока правку не одобрят
	in := bizPage(e)
	in.Title = "Зерно и молоко"
	rev, err := e.svc.Save(ctx, biz, domain.PageBusiness, in)
	if err != nil || rev.Revision == nil || rev.Title != "Кофейня «Зерно»" || rev.Revision.Title != "Зерно и молоко" {
		t.Fatalf("revision: %+v %v", rev, err)
	}
	if _, err := e.svc.Submit(ctx, biz, domain.PageBusiness); err != nil {
		t.Fatal(err)
	}
	pub, _ := e.svc.Public(ctx, domain.PageBusiness, p.Slug)
	if pub.Page.Title != "Кофейня «Зерно»" {
		t.Fatalf("old must be visible: %q", pub.Page.Title)
	}
	if _, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectPage, ID: p.ID, ModeratorID: 1, Approve: true, Revision: true}); err != nil {
		t.Fatal(err)
	}
	pub, _ = e.svc.Public(ctx, domain.PageBusiness, p.Slug)
	if pub.Page.Title != "Зерно и молоко" {
		t.Fatalf("revision not applied: %q", pub.Page.Title)
	}
	// правку можно отменить
	e.svc.Save(ctx, biz, domain.PageBusiness, bizPage(e))
	if d, err := e.svc.DiscardRevision(ctx, biz, domain.PageBusiness); err != nil || d.Revision != nil {
		t.Fatalf("discard: %+v %v", d, err)
	}
	if _, err := e.svc.DiscardRevision(ctx, biz, domain.PageBusiness); !errors.Is(err, ErrWrongStatus) {
		t.Fatalf("discard twice: %v", err)
	}
	// отклонение по жалобе (из опубликованного)
	if st, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectPage, ID: p.ID, ModeratorID: 1, Reason: "Недостоверные сведения"}); err != nil || st != domain.StatusRejected {
		t.Fatalf("reject: %v %v", st, err)
	}
	// без хранилища страниц модерация страниц недоступна
	if _, err := NewModeration(e.repo).Decide(ctx, Decision{Subject: domain.SubjectPage, ID: p.ID, ModeratorID: 1, Approve: true}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("no pages: %v", err)
	}
}

func TestPagesRevisionAutoAndWithdraw(t *testing.T) {
	e := newPagesEnv(domain.ModerationAuto)
	ctx := context.Background()
	e.svc.Save(ctx, biz, domain.PageBusiness, bizPage(e))
	e.svc.Submit(ctx, biz, domain.PageBusiness)
	in := bizPage(e)
	in.City = "Москва"
	e.svc.Save(ctx, biz, domain.PageBusiness, in)
	out, err := e.svc.Submit(ctx, biz, domain.PageBusiness)
	if err != nil || out.City != "Москва" || out.Revision != nil {
		t.Fatalf("auto revision: %+v %v", out, err)
	}
	in.City = "Сочи"
	e.svc.Save(ctx, biz, domain.PageBusiness, in)
	w, err := e.svc.Withdraw(ctx, biz, domain.PageBusiness)
	if err != nil || w.Status != domain.StatusDraft || w.City != "Сочи" {
		t.Fatalf("withdraw with revision: %+v %v", w, err)
	}
}

func TestPagesCreator(t *testing.T) {
	e := newPagesEnv(domain.ModerationAuto)
	ctx := context.Background()
	me := domain.User{ID: 21, Name: "Анна Смирнова"}
	e.plats[me.ID] = []domain.Platform{{Kind: "telegram", Title: "Канал", Followers: 12000, Verified: true}}
	e.cards[me.ID] = domain.CreatorCard{UserID: me.ID, Name: "Анна"}
	v, _ := e.svc.Get(ctx, me, domain.PageCreator, "")
	if v.SuggestedSlug != "anna-smirnova" {
		t.Fatalf("suggest: %q", v.SuggestedSlug)
	}
	in := PageInput{Title: "Анна", City: "Казань", Spheres: []string{"beauty"}, Blocks: []PageBlockInput{
		{Type: domain.PBAbout, Text: "Снимаю обзоры"},
		{Type: domain.PBChannels},
		{Type: domain.PBTopics, Items: []string{"food", "beauty", "food"}},
		{Type: domain.PBWorks, Text: "Лучшее", Links: []string{"t.me/anna/1"}, Videos: []VideoInput{{URL: "https://vk.com/video-123_456"}}},
		{Type: domain.PBReviews},
		{Type: domain.PBCard},
	}}
	p, err := e.svc.Save(ctx, me, domain.PageCreator, in)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Blocks[2].Items; len(got) != 2 {
		t.Fatalf("topics: %v", got)
	}
	if v := p.Blocks[3].Videos[0]; v.EmbedURL() != "https://vk.com/video_ext.php?oid=-123&id=456" {
		t.Fatalf("vk: %+v", v)
	}
	// бизнес-блок у автора — нельзя; тема не из списка — нельзя
	if _, err := e.svc.Save(ctx, me, domain.PageCreator, PageInput{Blocks: []PageBlockInput{{Type: domain.PBContacts}}}); fields(t, err)["blocks.0"] == "" {
		t.Fatal("contacts on creator")
	}
	if _, err := e.svc.Save(ctx, me, domain.PageCreator, PageInput{Blocks: []PageBlockInput{{Type: domain.PBTopics, Items: []string{"nope"}}}}); fields(t, err)["blocks.0"] == "" {
		t.Fatal("unknown topic")
	}
	if _, err := e.svc.Submit(ctx, me, domain.PageCreator); err != nil {
		t.Fatal(err)
	}
	pub, err := e.svc.Public(ctx, domain.PageCreator, p.Slug)
	if err != nil || len(pub.Platforms) != 1 || pub.Card == nil || pub.Tasks != nil {
		t.Fatalf("public creator: %+v %v", pub, err)
	}
	if got := pub.Page.Blocks[2].ItemTitles; len(got) != 2 || got[0] != "Еда и кафе" {
		t.Fatalf("topic titles: %v", got)
	}
	// у бизнеса страница этого же пользователя — своя, адреса не пересекаются
	if _, err := e.svc.Public(ctx, domain.PageBusiness, p.Slug); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("kinds must not mix")
	}
}

func TestPageCompleteAutoBlocksDontCount(t *testing.T) {
	p := domain.Page{Kind: domain.PageCreator, Title: "Анна", City: "Казань", Spheres: []string{"food"},
		Blocks: []domain.PageBlock{{Type: domain.PBChannels}, {Type: domain.PBCard}, {Type: domain.PBContacts, Contacts: &domain.PageContacts{}}}}
	if f := fields(t, pageComplete(p)); f["blocks"] == "" {
		t.Fatalf("auto blocks counted: %v", f)
	}
	p.Blocks = append(p.Blocks, domain.PageBlock{Type: domain.PBContacts, Contacts: &domain.PageContacts{Phone: "+7 900"}})
	if err := pageComplete(p); err != nil {
		t.Fatal(err)
	}
}

func TestCleanText(t *testing.T) {
	cases := map[string]string{
		"  а  \n\n\n\n б ": "а\n\nб",
		"\n\nа":            "а",
		"а\tб\r\nв\x00г":   "а б\nв г",
		"":                 "",
		"один\nдва\n\nтри": "один\nдва\n\nтри",
	}
	for in, want := range cases {
		if got := cleanText(in); got != want {
			t.Errorf("cleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTasksAndCardsCarryPageLinks(t *testing.T) {
	e := newPagesEnv(domain.ModerationAuto)
	ctx := context.Background()
	e.svc.Save(ctx, biz, domain.PageBusiness, bizPage(e))
	e.svc.Submit(ctx, biz, domain.PageBusiness)
	d := ListingDeps{Categories: e.cats, Submits: e.repo, Media: e.media, Clock: e.clock, Mode: domain.ModerationAuto, PageLinks: e.pages}
	tasks := NewTasks(e.repo, d)
	tk, err := tasks.Create(ctx, biz, fullTask())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.Submit(ctx, biz, tk.ID); err != nil {
		t.Fatal(err)
	}
	pub, err := tasks.Public(ctx, tk.ID)
	if err != nil || pub.BrandSlug != "kofeynya-zerno" {
		t.Fatalf("brand slug: %+v %v", pub.BrandSlug, err)
	}
	feed, err := tasks.Feed(ctx, FeedQuery{})
	if err != nil || len(feed.Tasks) != 1 || feed.Tasks[0].BrandSlug != "kofeynya-zerno" {
		t.Fatalf("feed slug: %+v %v", feed, err)
	}
	cards := NewCards(e.repo, d)
	if _, err := cards.Feed(ctx, FeedQuery{}); err != nil {
		t.Fatal(err)
	}
}

// ===== видео =====

func TestSniffVideo(t *testing.T) {
	mp4 := append([]byte{0, 0, 0, 0x20}, []byte("ftypisom\x00\x00\x02\x00")...)
	mov := append([]byte{0, 0, 0, 0x14}, []byte("ftypqt  ")...)
	webm := append([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x9F, 0x42, 0x82, 0x84}, []byte("webm")...)
	mkv := append([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x9F, 0x42, 0x82, 0x88}, []byte("matroska")...)
	cases := []struct {
		in   []byte
		want string
	}{{mp4, "video/mp4"}, {webm, "video/webm"}, {mov, ""}, {mkv, ""}, {[]byte("GIF89a"), ""}, {nil, ""}}
	for i, c := range cases {
		if got := SniffVideo(c.in); got != c.want {
			t.Errorf("%d: %q, want %q", i, got, c.want)
		}
	}
}

func TestUploadVideo(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
	repo, blobs := newMemMedia(clock), &memBlobs{data: map[string][]byte{}}
	svc := NewMedia(repo, blobs, &fakeImages{}, MediaConfig{VideoMaxBytes: 64, QuotaBytes: 100, VideoSlots: 1}, nil)
	ctx := context.Background()
	mp4 := append([]byte{0, 0, 0, 0x20}, []byte("ftypmp42 video data")...)

	m, err := svc.UploadVideo(ctx, biz, bytes.NewReader(mp4))
	if err != nil || m.Purpose != domain.PurposePageVideo || m.Bytes != int64(len(mp4)) {
		t.Fatalf("upload: %+v %v", m, err)
	}
	if _, ok := blobs.data[domain.VideoKey(m.ID)]; !ok {
		t.Fatal("blob not stored")
	}
	// владелец видит, гость — нет (не прикреплено)
	if _, _, err := svc.Open(ctx, biz.ID, m.ID, 0); err != nil {
		t.Fatalf("owner open: %v", err)
	}
	if _, _, err := svc.Open(ctx, 0, m.ID, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("guest open: %v", err)
	}
	if _, err := svc.UploadVideo(ctx, biz, bytes.NewReader(bytes.Repeat([]byte{1}, 65))); !errors.Is(err, ErrVideoTooLarge) {
		t.Fatalf("large: %v", err)
	}
	if _, err := svc.UploadVideo(ctx, biz, strings.NewReader("not a video")); !errors.Is(err, ErrVideoType) {
		t.Fatalf("type: %v", err)
	}
	if _, err := svc.UploadVideo(ctx, biz, strings.NewReader("")); fields(t, err)["file"] == "" {
		t.Fatalf("empty: %v", err)
	}
	// квота: 100 байт — два видео по 23 байта и ещё 63 уже не помещаются
	svc.UploadVideo(ctx, biz, bytes.NewReader(mp4))
	if _, err := svc.UploadVideo(ctx, biz, bytes.NewReader(append(mp4, make([]byte, 40)...))); !errors.Is(err, ErrMediaQuota) {
		t.Fatalf("quota: %v", err)
	}
	// обрыв связи
	if _, err := svc.UploadVideo(ctx, biz, errReader{}); !errors.Is(err, ErrUploadInterrupted) {
		t.Fatalf("interrupted: %v", err)
	}
	// все слоты заняты — busy
	svc.video <- struct{}{}
	if _, err := svc.UploadVideo(ctx, biz, bytes.NewReader(mp4)); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy: %v", err)
	}
	<-svc.video
	if svc.MaxVideoBytes() != 64 {
		t.Fatal("max")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

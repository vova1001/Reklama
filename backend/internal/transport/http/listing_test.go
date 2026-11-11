package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// fakeListings — подставные сервисы заданий, анкет, категорий и фото: err — ошибка любого метода, last — что передали.
type fakeListings struct {
	err    error
	last   map[string]any
	card   *domain.CreatorCard
	public bool
}

func (f *fakeListings) put(k string, v any) {
	if f.last == nil {
		f.last = map[string]any{}
	}
	f.last[k] = v
}

var day = time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)

func sampleTask(id int64, st domain.Status) domain.Task {
	return domain.Task{ID: id, OwnerID: 3, Status: st, Title: "Сторис", Formats: []string{"reels"}, Access: "open", PublishBy: &day,
		Blocks: []domain.Block{{Kind: "list", Title: "Что показать", Items: []string{"Меню"}}}, Cover: "c0000000000000000000000"[:22], Photos: []string{}}
}

func (f *fakeListings) List(context.Context, domain.User) ([]domain.Task, error) {
	return []domain.Task{sampleTask(1, domain.StatusDraft)}, f.err
}
func (f *fakeListings) Get(_ context.Context, _ domain.User, id int64) (domain.Task, error) {
	if id == 404 {
		return domain.Task{}, domain.ErrNotFound
	}
	return sampleTask(id, domain.StatusDraft), f.err
}
func (f *fakeListings) Create(_ context.Context, _ domain.User, in service.TaskInput) (domain.Task, error) {
	f.put("create", in)
	return sampleTask(5, domain.StatusDraft), f.err
}
func (f *fakeListings) Update(_ context.Context, _ domain.User, id int64, in service.TaskInput) (domain.Task, error) {
	f.put("update", in)
	return sampleTask(id, domain.StatusDraft), f.err
}
func (f *fakeListings) Delete(_ context.Context, _ domain.User, id int64) error {
	f.put("delete", id)
	return f.err
}
func (f *fakeListings) Submit(_ context.Context, _ domain.User, id int64) (domain.Task, error) {
	if f.err != nil {
		return domain.Task{}, f.err
	}
	return sampleTask(id, domain.StatusPending), nil
}
func (f *fakeListings) Withdraw(_ context.Context, _ domain.User, id int64) (domain.Task, error) {
	return sampleTask(id, domain.StatusDraft), f.err
}
func (f *fakeListings) Feed(_ context.Context, q service.FeedQuery) (service.FeedPage, error) {
	f.put("feed", q.After)
	f.put("filter", q.Filter)
	t := sampleTask(9, domain.StatusPublished)
	t.Brand, t.CategoryTitles = "Кофейня", []string{"Еда и кафе"}
	return service.FeedPage{Tasks: []domain.Task{t}, Next: &domain.FeedCursor{PublishedAt: day, ID: 9}}, f.err
}

func (f *fakeListings) Public(_ context.Context, id int64) (domain.Task, error) {
	if id == 404 {
		return domain.Task{}, domain.ErrNotFound
	}
	return sampleTask(id, domain.StatusPublished), f.err
}

// карточка — отдельный тип (у сервисов одинаковые имена методов)
type fakeCards struct{ f *fakeListings }

func (c fakeCards) Get(context.Context, domain.User) (*domain.CreatorCard, error) {
	return c.f.card, c.f.err
}
func (c fakeCards) Save(_ context.Context, _ domain.User, in service.CardInput) (domain.CreatorCard, error) {
	c.f.put("card", in)
	return domain.CreatorCard{Status: domain.StatusDraft, Name: in.Name}, c.f.err
}
func (c fakeCards) Submit(context.Context, domain.User) (domain.CreatorCard, error) {
	return domain.CreatorCard{Status: domain.StatusPending}, c.f.err
}
func (c fakeCards) Withdraw(context.Context, domain.User) (domain.CreatorCard, error) {
	return domain.CreatorCard{Status: domain.StatusDraft}, c.f.err
}

func (c fakeCards) Feed(_ context.Context, q service.FeedQuery) (service.CardPage, error) {
	c.f.put("cardFilter", q.Filter)
	card := domain.CreatorCard{UserID: 8, Status: domain.StatusPublished, Name: "Алина", Topics: []string{"food"}, TopicTitles: []string{"Еда"}, PublishedAt: &day}
	return service.CardPage{Cards: []domain.CreatorCard{card}, Next: &domain.FeedCursor{PublishedAt: day, ID: 8}}, c.f.err
}
func (c fakeCards) Public(_ context.Context, id int64) (domain.CreatorCard, error) {
	if id == 404 {
		return domain.CreatorCard{}, domain.ErrNotFound
	}
	return domain.CreatorCard{UserID: id, Status: domain.StatusPublished, Name: "Алина"}, c.f.err
}

type fakeCats struct{ f *fakeListings }

func (c fakeCats) List(context.Context, domain.User) (service.CategoryList, error) {
	return service.CategoryList{
		Common: []domain.CategoryInfo{{Slug: "food", Title: "Еда", Icon: "coffee", Status: "approved"}},
		Own:    []domain.CategoryInfo{{Slug: "u-1", Title: "Настолки", Icon: "tag", Status: "pending_review", AuthorID: 3}},
	}, c.f.err
}
func (c fakeCats) Similar(_ context.Context, _ domain.User, q string) ([]domain.CategoryInfo, error) {
	c.f.put("q", q)
	return []domain.CategoryInfo{}, c.f.err
}
func (c fakeCats) Create(_ context.Context, _ domain.User, title string) (domain.CategoryInfo, bool, error) {
	return domain.CategoryInfo{Slug: "u-2", Title: title, Status: "pending_review", AuthorID: 3}, title != "Еда", c.f.err
}

type fakeFormats struct{ f *fakeListings }

func (m fakeFormats) Similar(_ context.Context, _ domain.User, q string) ([]domain.Format, error) {
	m.f.put("fq", q)
	return []domain.Format{{ID: "reels", Title: "Reels", Builtin: true}, {ID: "Прямой эфир", Title: "Прямой эфир", Own: true}}, m.f.err
}

type fakeMedia struct{ f *fakeListings }

func (m fakeMedia) MaxBytes() int64 { return 1 << 10 }
func (m fakeMedia) Upload(_ context.Context, _ domain.User, in service.UploadInput) (domain.Media, error) {
	m.f.put("upload", in)
	if m.f.err != nil {
		return domain.Media{}, m.f.err
	}
	return domain.Media{ID: "m000000000000000000000", Purpose: in.Purpose, Width: 800, Height: 450}, nil
}
func (m fakeMedia) Delete(_ context.Context, _ domain.User, id string) error {
	m.f.put("mdel", id)
	return m.f.err
}
func (m fakeMedia) SetAvatar(_ context.Context, _ domain.User, id string) (string, error) {
	if id == "" {
		return "", m.f.err
	}
	return service.MediaURL(id, 320), m.f.err
}
func (m fakeMedia) Open(_ context.Context, viewer int64, id string, size int) (service.Blob, bool, error) {
	m.f.put("viewer", viewer)
	if id == "missing0000000000000000"[:22] {
		return service.Blob{}, false, domain.ErrNotFound
	}
	return service.Blob{ContentType: "image/webp", Data: []byte("RIFFwebp")}, m.f.public, m.f.err
}

func listSrv(f *fakeListings, opt Options) *Server {
	opt.Origin, opt.SecureCookies = testOrigin, true
	return New(Services{
		Home: fakeHome{}, Health: fakeDB{}, Auth: &fakeAuth{user: domain.User{ID: 3}}, Profiles: &fakeProfiles{},
		Tasks: f, Cards: fakeCards{f}, Categories: fakeCats{f}, Formats: fakeFormats{f}, Media: fakeMedia{f},
	}, opt)
}

func TestTaskEndpoints(t *testing.T) {
	f := &fakeListings{}
	s := listSrv(f, Options{})
	body := `{"title":"Сторис","city":"","nationwide":true,"categories":["food","beauty"],"formats":["reels","Прямой эфир"],"access":"approval",
		"minViews":5000,"rewardKopecks":300000,"publishBy":"2026-11-20",
		"blocks":[{"kind":"list","title":"Что показать","items":["Меню"]}],"cover":"","photos":["p1"]}`
	rec := serve(s, meReq(http.MethodPost, "/api/me/tasks", body))
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	in := f.last["create"].(service.TaskInput)
	if !in.Nationwide || in.RewardKopecks != 300000 || in.Blocks[0].Items[0] != "Меню" || in.Photos[0] != "p1" || in.PublishBy != "2026-11-20" ||
		len(in.Categories) != 2 || in.Formats[1] != "Прямой эфир" {
		t.Errorf("в сервис: %+v", in)
	}
	var out taskDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.ID != 5 || *out.PublishBy != "2026-11-20" || out.Cover.Src != "/media/c000000000000000000000/800.webp" || out.Photos == nil {
		t.Errorf("ответ: %s", rec.Body.String())
	}
	for _, c := range []struct {
		method, path string
		code         int
	}{
		{http.MethodGet, "/api/me/tasks", 200},
		{http.MethodGet, "/api/me/tasks/5", 200},
		{http.MethodGet, "/api/me/tasks/404", 404},
		{http.MethodGet, "/api/me/tasks/abc", 404},
		{http.MethodPut, "/api/me/tasks/5", 200},
		{http.MethodDelete, "/api/me/tasks/5", 204},
		{http.MethodPost, "/api/me/tasks/5/submit", 200},
		{http.MethodPost, "/api/me/tasks/5/withdraw", 200},
	} {
		b := ""
		if c.method == http.MethodPut {
			b = body
		}
		if rec := serve(s, meReq(c.method, c.path, b)); rec.Code != c.code {
			t.Errorf("%s %s: %d %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
	// лишнее поле, без входа, без CSRF на DELETE
	if rec := serve(s, meReq(http.MethodPost, "/api/me/tasks", `{"status":"published"}`)); rec.Code != 400 {
		t.Errorf("лишнее поле: %d", rec.Code)
	}
	if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/me/tasks", nil)); rec.Code != 401 {
		t.Errorf("без входа: %d", rec.Code)
	}
	r := httptest.NewRequest(http.MethodDelete, "/api/me/tasks/5", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "good"})
	if rec := serve(s, r); rec.Code != 403 {
		t.Errorf("DELETE без CSRF: %d", rec.Code)
	}
	// большое тело с блоками проходит (до 96 КБ), больше — нет
	big := `{"title":"` + strings.Repeat("я", 20000) + `"}`
	if rec := serve(s, meReq(http.MethodPost, "/api/me/tasks", big)); rec.Code != 201 {
		t.Errorf("40 КБ: %d", rec.Code)
	}
	huge := `{"title":"` + strings.Repeat("я", 60000) + `"}`
	if rec := serve(s, meReq(http.MethodPost, "/api/me/tasks", huge)); rec.Code != 400 {
		t.Errorf("120 КБ: %d", rec.Code)
	}
}

func TestListingErrors(t *testing.T) {
	cases := []struct {
		err    error
		code   int
		apiErr string
	}{
		{&service.ValidationError{Fields: map[string]string{"blocks.1": "Пункт — до 200 символов"}}, 400, "validation"},
		{service.ErrWrongStatus, 409, "wrong_status"},
		{service.ErrTooManyTasks, 409, "tasks_limit"},
		{&service.LimitError{Reason: "submits_daily", RetryAfter: 90 * time.Minute}, 429, "rate_limited"},
		{domain.ErrNotFound, 404, "not_found"},
		{service.ErrImageType, 415, "image_type"},
		{service.ErrImageTooLarge, 413, "image_too_large"},
		{service.ErrMediaQuota, 409, "media_quota"},
		{service.ErrDraftTooLarge, 413, "draft_too_large"},
		{service.ErrBusy, 503, "busy"},
		{context.Canceled, 503, "cancelled"},
		{errors.New("pg: secret"), 500, "internal"},
	}
	for _, c := range cases {
		s := listSrv(&fakeListings{err: c.err}, Options{})
		rec := serve(s, meReq(http.MethodPost, "/api/me/tasks/5/submit", ""))
		e := errOf(t, rec)
		if rec.Code != c.code || e.Code != c.apiErr || strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("%v: %d %s", c.err, rec.Code, rec.Body.String())
		}
		if c.apiErr == "rate_limited" && (rec.Header().Get("Retry-After") != "5400" || !strings.Contains(e.Message, "90 мин")) {
			t.Errorf("лимит отправок: %v %s", rec.Header(), e.Message)
		}
		if c.apiErr == "validation" && e.Fields["blocks.1"] == "" {
			t.Errorf("поле блока: %+v", e)
		}
	}
}

func TestFeedEndpoint(t *testing.T) {
	f := &fakeListings{}
	s := listSrv(f, Options{})
	rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	var out feedOut
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || len(out.Tasks) != 1 || out.Tasks[0].Brand != "Кофейня" || out.Next == nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = serve(s, httptest.NewRequest(http.MethodGet, "/api/tasks?cursor="+*out.Next, nil))
	if c := f.last["feed"].(*domain.FeedCursor); rec.Code != 200 || c.ID != 9 || !c.PublishedAt.Equal(day) {
		t.Errorf("курсор: %d %+v", rec.Code, c)
	}
	for _, bad := range []string{"!!", "MTpa", "YWJj", "MTow"} {
		if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/tasks?cursor="+bad, nil)); rec.Code != 400 {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	// отбор — в сервис как есть (проверяет сервис), огромный параметр обрезается
	serve(s, httptest.NewRequest(http.MethodGet, "/api/tasks?category=food&category=beauty&city=%D0%9A%D0%B0%D0%B7%D0%B0%D0%BD%D1%8C&format=reels&city2="+strings.Repeat("x", 300), nil))
	if f := f.last["filter"].(domain.FeedFilter); len(f.Categories) != 2 || f.Categories[1] != "beauty" || f.City != "Казань" || f.Formats[0] != "reels" {
		t.Errorf("отбор: %+v", f)
	}
	serve(s, httptest.NewRequest(http.MethodGet, "/api/tasks?city="+strings.Repeat("x", 300), nil))
	if f := f.last["filter"].(domain.FeedFilter); len(f.City) != 200 {
		t.Errorf("обрезка: %d", len(f.City))
	}
	serve(s, httptest.NewRequest(http.MethodGet, "/api/tasks?"+strings.Repeat("category="+strings.Repeat("x", 100)+"&", 20), nil))
	if f := f.last["filter"].(domain.FeedFilter); len(f.Categories) != 10 || len(f.Categories[0]) != 64 {
		t.Errorf("обрезка списка: %d", len(f.Categories))
	}
	// страница задания: опубликованное — всем, иначе 404
	if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/tasks/9", nil)); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"published"`) {
		t.Errorf("задание: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/tasks/404", nil)); rec.Code != 404 {
		t.Errorf("нет задания: %d", rec.Code)
	}
}

func TestCardFeedEndpoints(t *testing.T) {
	f := &fakeListings{}
	s := listSrv(f, Options{})
	// только после входа
	if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/cards", nil)); rec.Code != 401 {
		t.Errorf("без входа: %d", rec.Code)
	}
	rec := serve(s, meReq(http.MethodGet, "/api/cards?topic=food&topic=beauty&city=Москва&cursor=", ""))
	var out cardFeedOut
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || len(out.Cards) != 1 || out.Cards[0].ID != 8 || out.Cards[0].TopicTitles[0] != "Еда" || out.Next == nil || out.Cards[0].PublishedAt == nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if q := f.last["cardFilter"].(domain.FeedFilter); len(q.Categories) != 2 || q.Categories[0] != "food" || q.City != "Москва" {
		t.Errorf("отбор: %+v", q)
	}
	if rec := serve(s, meReq(http.MethodGet, "/api/cards?cursor=!!", "")); rec.Code != 400 {
		t.Errorf("курсор: %d", rec.Code)
	}
	if rec := serve(s, meReq(http.MethodGet, "/api/cards/8", "")); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":8`) {
		t.Errorf("анкета: %d %s", rec.Code, rec.Body.String())
	}
	for _, p := range []string{"/api/cards/404", "/api/cards/x"} {
		if rec := serve(s, meReq(http.MethodGet, p, "")); rec.Code != 404 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	f.err = &service.ValidationError{Fields: map[string]string{"format": "Выберите формат из списка"}}
	if rec := serve(s, meReq(http.MethodGet, "/api/cards?format=reels", "")); rec.Code != 400 {
		t.Errorf("ошибка отбора: %d", rec.Code)
	}
}

func TestCardAndCategoryEndpoints(t *testing.T) {
	f := &fakeListings{}
	s := listSrv(f, Options{})
	if rec := serve(s, meReq(http.MethodGet, "/api/me/card", "")); strings.TrimSpace(rec.Body.String()) != `{"card":null}` {
		t.Errorf("нет анкеты: %s", rec.Body.String())
	}
	rec := serve(s, meReq(http.MethodPut, "/api/me/card", `{"name":"Алина","city":"","topics":["food"],"platforms":[{"url":"t.me/a","followers":10}],"blocks":[],"avatar":"","works":[]}`))
	// площадки из прежних форм игнорируются: подписчики — только подтверждённые (Social)
	if rec.Code != 200 || f.last["card"].(service.CardInput).Name != "Алина" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	for _, p := range []string{"/api/me/card/submit", "/api/me/card/withdraw"} {
		if rec := serve(s, meReq(http.MethodPost, p, "")); rec.Code != 200 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	rec = serve(s, meReq(http.MethodGet, "/api/me/categories", ""))
	if !strings.Contains(rec.Body.String(), `"own":true`) || !strings.Contains(rec.Body.String(), `"slug":"food"`) {
		t.Errorf("категории: %s", rec.Body.String())
	}
	serve(s, meReq(http.MethodGet, "/api/me/categories/similar?q="+strings.Repeat("a", 300), ""))
	if q := f.last["q"].(string); len(q) != 200 {
		t.Errorf("запрос обрезан: %d", len(q))
	}
	rec = serve(s, meReq(http.MethodGet, "/api/me/formats/similar?q=%D1%8D%D1%84"+strings.Repeat("a", 300), ""))
	if rec.Code != 200 || len(f.last["fq"].(string)) != 200 || !strings.Contains(rec.Body.String(), `{"id":"reels","title":"Reels","builtin":true,"own":false}`) ||
		!strings.Contains(rec.Body.String(), `"own":true`) {
		t.Errorf("подсказки форматов: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/me/formats/similar?q=x", nil)); rec.Code != 401 {
		t.Errorf("форматы без входа: %d", rec.Code)
	}
	if rec := serve(s, meReq(http.MethodPost, "/api/me/categories", `{"title":"Настолки"}`)); rec.Code != 201 {
		t.Errorf("новая: %d", rec.Code)
	}
	if rec := serve(s, meReq(http.MethodPost, "/api/me/categories", `{"title":"Еда"}`)); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"created":false`) {
		t.Errorf("есть: %d %s", rec.Code, rec.Body.String())
	}
}

func multipartReq(t *testing.T, fields map[string]string, file []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	if file != nil {
		fw, _ := w.CreateFormFile("file", "photo.jpg")
		_, _ = fw.Write(file)
	}
	_ = w.Close()
	r := meReq(http.MethodPost, "/api/me/media", "")
	r.Body = httptestBody(buf.Bytes())
	r.ContentLength = int64(buf.Len())
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

func TestUploadEndpoint(t *testing.T) {
	f := &fakeListings{}
	s := listSrv(f, Options{UploadLimiter: newMemLimiter(0.001, 3)})
	rec := serve(s, multipartReq(t, map[string]string{"purpose": "task_cover", "crop": "0.1,0,0.8,0.9"}, []byte("jpeg")))
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"src":"/media/m000000000000000000000/800.webp"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	in := f.last["upload"].(service.UploadInput)
	if in.Purpose != "task_cover" || string(in.Data) != "jpeg" || in.Crop == nil || in.Crop.W != 0.8 {
		t.Errorf("в сервис: %+v", in)
	}
	if rec := serve(s, multipartReq(t, map[string]string{"purpose": "avatar"}, bytes.Repeat([]byte("x"), 2<<10))); rec.Code != 413 {
		t.Errorf("больше предела: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(s, multipartReq(t, map[string]string{"purpose": "avatar", "crop": "1,2"}, []byte("x"))); rec.Code != 400 && rec.Code != 429 {
		t.Errorf("обрезка: %d", rec.Code)
	}
	// лимит загрузок по пользователю
	if rec := serve(s, multipartReq(t, map[string]string{"purpose": "avatar"}, []byte("x"))); rec.Code != 429 {
		t.Errorf("лимит: %d", rec.Code)
	}
	s = listSrv(f, Options{})
	if rec := serve(s, multipartReq(t, map[string]string{"purpose": "avatar"}, nil)); rec.Code != 400 || errOf(t, rec).Fields["file"] == "" {
		t.Errorf("без файла: %d", rec.Code)
	}
	r := meReq(http.MethodPost, "/api/me/media", `{"purpose":"avatar"}`)
	r.Header.Set("Content-Type", "application/json")
	if rec := serve(s, r); rec.Code != 400 {
		t.Errorf("не multipart: %d", rec.Code)
	}
	if rec := serve(s, meReq(http.MethodDelete, "/api/me/media/abc", "")); rec.Code != 204 || f.last["mdel"] != "abc" {
		t.Errorf("удалить: %d", rec.Code)
	}
	if rec := serve(s, meReq(http.MethodPut, "/api/me/avatar", `{"mediaId":"m000000000000000000000"}`)); !strings.Contains(rec.Body.String(), "/320.webp") {
		t.Errorf("аватар: %s", rec.Body.String())
	}
	if rec := serve(s, meReq(http.MethodPut, "/api/me/avatar", `{"mediaId":null}`)); strings.TrimSpace(rec.Body.String()) != `{"avatarUrl":""}` {
		t.Errorf("убрать: %s", rec.Body.String())
	}
}

func TestServeMedia(t *testing.T) {
	f := &fakeListings{}
	s := listSrv(f, Options{})
	// закрытое — владельцу, с приватным кэшем
	r := httptest.NewRequest(http.MethodGet, "/media/m000000000000000000000/800.webp", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "good"})
	rec := serve(s, r)
	if rec.Code != 200 || f.last["viewer"] != int64(3) || rec.Header().Get("Cache-Control") != "private, max-age=3600" ||
		rec.Header().Get("Content-Type") != "image/webp" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	etag := rec.Header().Get("ETag")
	// опубликованное — гостю, долгий кэш, 304 по ETag
	f.public = true
	rec = serve(s, httptest.NewRequest(http.MethodGet, "/media/m000000000000000000000/800.webp", nil))
	if rec.Code != 200 || f.last["viewer"] != int64(0) || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("гость: %d %v", rec.Code, rec.Header())
	}
	r = httptest.NewRequest(http.MethodGet, "/media/m000000000000000000000/800.webp", nil)
	r.Header.Set("If-None-Match", etag)
	if rec := serve(s, r); rec.Code != 304 || rec.Body.Len() != 0 {
		t.Errorf("304: %d", rec.Code)
	}
	for _, p := range []string{"/media/missing000000000000000/800.webp", "/media/m000000000000000000000/800.png", "/media/m000000000000000000000/x.webp"} {
		if rec := serve(s, httptest.NewRequest(http.MethodGet, p, nil)); rec.Code != 404 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

func httptestBody(b []byte) *nopCloser { return &nopCloser{bytes.NewReader(b)} }

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

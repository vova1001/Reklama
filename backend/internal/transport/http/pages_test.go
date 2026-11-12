package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// fakePages — подставной сервис страниц и видео: err — ошибка любого метода, last — что передали.
type fakePages struct {
	err  error
	last map[string]any
}

func (f *fakePages) put(k string, v any) {
	if f.last == nil {
		f.last = map[string]any{}
	}
	f.last[k] = v
}

func samplePage(kind domain.PageKind) domain.Page {
	return domain.Page{ID: 4, UserID: 3, Kind: kind, Slug: "zerno", Status: domain.StatusDraft, Title: "Зерно", City: "Казань",
		Spheres: []string{"food"}, Logo: "l000000000000000000000", Blocks: []domain.PageBlock{
			{Type: domain.PBVideo, Videos: []domain.PageVideo{{Source: domain.VideoYouTube, ID: "dQw4w9WgXcQ"}}},
			{Type: domain.PBVideo, Videos: []domain.PageVideo{{Source: domain.VideoFile, Media: "v000000000000000000000", Poster: "p000000000000000000000"}}},
			{Type: domain.PBContacts, Contacts: &domain.PageContacts{Site: "https://zerno.ru"}},
		}}
}

func (f *fakePages) Get(_ context.Context, _ domain.User, kind domain.PageKind, name string) (service.PageView, error) {
	f.put("name", name)
	if kind == domain.PageCreator {
		return service.PageView{SuggestedSlug: "anna"}, f.err
	}
	p := samplePage(kind)
	rev := p
	rev.Title = "Новое"
	p.Status, p.Revision, p.RevisionStatus = domain.StatusPublished, &rev, domain.StatusPending
	return service.PageView{Page: &p, SuggestedSlug: p.Slug}, f.err
}
func (f *fakePages) Save(_ context.Context, _ domain.User, kind domain.PageKind, in service.PageInput) (domain.Page, error) {
	f.put("save", in)
	return samplePage(kind), f.err
}
func (f *fakePages) SetSlug(_ context.Context, _ domain.User, kind domain.PageKind, slug string) (domain.Page, error) {
	f.put("slug", slug)
	if slug == "taken" {
		return domain.Page{}, &service.ValidationError{Fields: map[string]string{"slug": "Этот адрес уже занят — выберите другой"}}
	}
	return samplePage(kind), f.err
}
func (f *fakePages) Submit(_ context.Context, _ domain.User, kind domain.PageKind) (domain.Page, error) {
	return samplePage(kind), f.err
}
func (f *fakePages) Withdraw(_ context.Context, _ domain.User, kind domain.PageKind) (domain.Page, error) {
	return samplePage(kind), f.err
}
func (f *fakePages) DiscardRevision(_ context.Context, _ domain.User, kind domain.PageKind) (domain.Page, error) {
	if kind != domain.PageBusiness {
		return domain.Page{}, domain.ErrNotFound
	}
	return samplePage(kind), f.err
}
func (f *fakePages) Public(_ context.Context, kind domain.PageKind, slug string) (service.PublicPage, error) {
	switch slug {
	case "old":
		return service.PublicPage{MovedTo: "zerno"}, nil
	case "zerno":
		p := samplePage(kind)
		out := service.PublicPage{Page: p}
		if kind == domain.PageBusiness {
			out.Tasks = []domain.Task{sampleTask(9, domain.StatusPublished)}
		} else {
			out.Platforms = []domain.Platform{}
		}
		return out, nil
	}
	return service.PublicPage{}, domain.ErrNotFound
}
func (f *fakePages) MaxVideoBytes() int64 { return 1 << 10 }
func (f *fakePages) UploadVideo(_ context.Context, _ domain.User, body io.Reader) (domain.Media, error) {
	b, err := io.ReadAll(body)
	if err != nil {
		return domain.Media{}, errors.Join(service.ErrUploadInterrupted, err)
	}
	if f.err != nil {
		return domain.Media{}, f.err
	}
	if len(b) > 1<<10 {
		return domain.Media{}, service.ErrVideoTooLarge
	}
	if service.SniffVideo(b) == "" {
		return domain.Media{}, service.ErrVideoType
	}
	return domain.Media{ID: "v000000000000000000000", Bytes: int64(len(b))}, nil
}

func pagesSrv(f *fakePages, lf *fakeListings) *Server {
	return New(Services{
		Home: fakeHome{}, Health: fakeDB{}, Auth: &fakeAuth{user: domain.User{ID: 3}}, Profiles: &fakeProfiles{},
		Media: fakeMedia{lf}, Pages: f, Videos: f,
	}, Options{Origin: testOrigin, SecureCookies: true})
}

func TestPageEndpoints(t *testing.T) {
	f := &fakePages{}
	s := pagesSrv(f, &fakeListings{})
	body := `{"slug":"","title":"Зерно","city":"Казань","spheres":["food"],"logo":"","cover":"",
		"blocks":[{"type":"video","title":"","text":"","items":[],"photos":[],"videos":[{"media":"","poster":"","url":"youtu.be/dQw4w9WgXcQ"}],"links":[],"contacts":null},
		{"type":"contacts","contacts":{"city":"Казань","address":"","hours":"","site":"zerno.ru","phone":"","socials":["vk.com/z"]}}]}`
	rec := serve(s, meReq(http.MethodPut, "/api/me/pages/business", body))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	in := f.last["save"].(service.PageInput)
	if in.Title != "Зерно" || in.Blocks[0].Videos[0].URL != "youtu.be/dQw4w9WgXcQ" || in.Blocks[1].Contacts.Socials[0] != "vk.com/z" {
		t.Errorf("в сервис: %+v", in)
	}
	var out pageOut
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	p := out.Page
	if p == nil || p.Slug != "zerno" || p.Logo.Src != "/media/l000000000000000000000/800.webp" ||
		p.Blocks[0].Videos[0].EmbedURL != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ" ||
		p.Blocks[0].Videos[0].URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" ||
		p.Blocks[1].Videos[0].Src != "/media/v000000000000000000000/video" || p.Blocks[1].Videos[0].Poster.Thumb == "" ||
		p.Blocks[2].Contacts.Site != "https://zerno.ru" {
		t.Errorf("ответ: %s", rec.Body.String())
	}
	// своя страница с правкой; новая — адрес-подсказка
	rec = serve(s, meReq(http.MethodGet, "/api/me/pages/business?name=Зерно", ""))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"revision":{"status":"pending_review"`) || f.last["name"] != "Зерно" {
		t.Errorf("get: %s", rec.Body.String())
	}
	rec = serve(s, meReq(http.MethodGet, "/api/me/pages/creator", ""))
	if rec.Code != 200 || rec.Body.String() != "{\"page\":null,\"suggestedSlug\":\"anna\"}\n" {
		t.Errorf("get new: %s", rec.Body.String())
	}
	for _, c := range []struct {
		method, path, body string
		code               int
	}{
		{http.MethodPut, "/api/me/pages/business/slug", `{"slug":"zerno"}`, 200},
		{http.MethodPut, "/api/me/pages/business/slug", `{"slug":"taken"}`, 400},
		{http.MethodPut, "/api/me/pages/business/slug", `{"slug":"x","extra":1}`, 400},
		{http.MethodPost, "/api/me/pages/business/submit", "", 200},
		{http.MethodPost, "/api/me/pages/creator/withdraw", "", 200},
		{http.MethodDelete, "/api/me/pages/business/revision", "", 200},
		{http.MethodDelete, "/api/me/pages/creator/revision", "", 404},
		{http.MethodPut, "/api/me/pages/business", `{"status":"published"}`, 400},
	} {
		if rec := serve(s, meReq(c.method, c.path, c.body)); rec.Code != c.code {
			t.Errorf("%s %s: %d %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
	// без входа — нельзя; статус сменился — 409
	if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/me/pages/business", nil)); rec.Code != 401 {
		t.Errorf("без входа: %d", rec.Code)
	}
	f.err = service.ErrWrongStatus
	if rec := serve(s, meReq(http.MethodPost, "/api/me/pages/business/submit", "")); rec.Code != 409 {
		t.Errorf("статус: %d", rec.Code)
	}
}

func TestPublicPageEndpoint(t *testing.T) {
	s := pagesSrv(&fakePages{}, &fakeListings{})
	// без входа — видно всем, с кэшем
	rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/pages/business/zerno", nil))
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=30" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	var out publicPageOut
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Page == nil || len(out.Tasks) != 1 || out.Platforms != nil || out.MovedTo != "" {
		t.Errorf("business: %s", rec.Body.String())
	}
	rec = serve(s, httptest.NewRequest(http.MethodGet, "/api/pages/creator/zerno", nil))
	if !strings.Contains(rec.Body.String(), `"platforms":[]`) {
		t.Errorf("creator: %s", rec.Body.String())
	}
	rec = serve(s, httptest.NewRequest(http.MethodGet, "/api/pages/business/old", nil))
	if rec.Code != 200 || rec.Body.String() != "{\"movedTo\":\"zerno\"}\n" {
		t.Errorf("moved: %s", rec.Body.String())
	}
	if rec := serve(s, httptest.NewRequest(http.MethodGet, "/api/pages/business/none", nil)); rec.Code != 404 {
		t.Errorf("none: %d", rec.Code)
	}
	// без сервиса страниц — маршрутов нет
	off := New(Services{Home: fakeHome{}, Health: fakeDB{}, Auth: &fakeAuth{}, Profiles: &fakeProfiles{}}, Options{Origin: testOrigin})
	if rec := serve(off, httptest.NewRequest(http.MethodGet, "/api/pages/business/zerno", nil)); rec.Code != 404 {
		t.Errorf("off: %d", rec.Code)
	}
}

func videoReq(t *testing.T, file []byte, field string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("note", "x")
	if file != nil {
		fw, _ := w.CreateFormFile(field, "clip.mp4")
		_, _ = fw.Write(file)
	}
	_ = w.Close()
	r := meReq(http.MethodPost, "/api/me/media/video", "")
	r.Body = httptestBody(buf.Bytes())
	r.ContentLength = int64(buf.Len())
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

func TestVideoUploadEndpoint(t *testing.T) {
	f := &fakePages{}
	s := pagesSrv(f, &fakeListings{})
	mp4 := append([]byte{0, 0, 0, 0x20}, []byte("ftypisom data")...)
	rec := serve(s, videoReq(t, mp4, "file"))
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"src":"/media/v000000000000000000000/video"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	cases := []struct {
		file  []byte
		field string
		code  int
		ecode string
	}{
		{[]byte("GIF89a"), "file", 415, "video_type"},
		{bytes.Repeat([]byte{1}, 3<<10), "file", 413, "video_too_large"},
		{nil, "file", 400, "validation"},
		{mp4, "other", 400, "validation"},
	}
	for _, c := range cases {
		rec := serve(s, videoReq(t, c.file, c.field))
		if rec.Code != c.code || errOf(t, rec).Code != c.ecode {
			t.Errorf("%s: %d %s", c.ecode, rec.Code, rec.Body.String())
		}
	}
	f.err = service.ErrBusy
	if rec := serve(s, videoReq(t, mp4, "file")); rec.Code != 503 || rec.Header().Get("Retry-After") != "30" {
		t.Errorf("busy: %d", rec.Code)
	}
	f.err = service.ErrMediaQuota
	if rec := serve(s, videoReq(t, mp4, "file")); rec.Code != 409 {
		t.Errorf("quota: %d", rec.Code)
	}
	f.err = errors.New("db down")
	if rec := serve(s, videoReq(t, mp4, "file")); rec.Code != 500 {
		t.Errorf("internal: %d", rec.Code)
	}
	r := meReq(http.MethodPost, "/api/me/media/video", "{}")
	r.Header.Set("Content-Type", "application/json")
	if rec := serve(s, r); rec.Code != 400 {
		t.Errorf("not multipart: %d", rec.Code)
	}
	// оборванное тело
	r = videoReq(t, mp4, "file")
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader([]byte("--x")), errBody{}))
	if rec := serve(s, r); rec.Code != 400 {
		t.Errorf("broken: %d", rec.Code)
	}
}

type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestVideoServe(t *testing.T) {
	lf := &fakeListings{public: true}
	s := pagesSrv(&fakePages{}, lf)
	r := httptest.NewRequest(http.MethodGet, "/media/m000000000000000000000/video", nil)
	r.Header.Set("Range", "bytes=0-3")
	rec := serve(s, r)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "RIFF" || rec.Header().Get("Accept-Ranges") != "bytes" ||
		rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || rec.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("range: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	if lf.last["viewer"] != int64(0) {
		t.Errorf("viewer: %v", lf.last["viewer"])
	}
	if rec := serve(s, httptest.NewRequest(http.MethodGet, "/media/m000000000000000000000/0.webp", nil)); rec.Code != 404 {
		t.Errorf("size 0 webp: %d", rec.Code)
	}
}

func TestTaskAndCardCarrySlugs(t *testing.T) {
	tk := sampleTask(1, domain.StatusPublished)
	tk.BrandSlug = "zerno"
	if d := toTaskDTO(tk); d.BrandSlug != "zerno" {
		t.Error("brandSlug")
	}
	c := domain.CreatorCard{UserID: 2, PageSlug: "anna"}
	if d := toCardDTO(&c); d.PageSlug != "anna" {
		t.Error("pageSlug")
	}
}

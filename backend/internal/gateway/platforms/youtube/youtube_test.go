package youtube

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/gateway/platforms/httpx"
	"ohvat/backend/internal/service"
)

// fakeGoogle — подставной Google OAuth и YouTube Data API (настоящий Google в тестах не вызывается).
type fakeGoogle struct {
	mu         sync.Mutex
	tokenCode  int
	tokenBody  string
	apiCode    int
	apiBody    string
	forms      []url.Values
	auth       []string
	queries    []url.Values
	revoked    []string
	revokeCode int
}

func (f *fakeGoogle) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.forms = append(f.forms, r.PostForm)
		code, body := f.tokenCode, f.tokenBody
		f.mu.Unlock()
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.revoked = append(f.revoked, r.PostForm.Get("token"))
		code := f.revokeCode
		f.mu.Unlock()
		if code == 0 {
			code = 200
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
	})
	mux.HandleFunc("/yt/channels", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.queries = append(f.queries, r.URL.Query())
		code, body := f.apiCode, f.apiBody
		f.mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("User-Agent"), httpx.UserAgent) {
			code, body = 400, `{}`
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	})
	return mux
}

const channelOK = `{"items":[{"id":"UCabc","snippet":{"title":"Алина","customUrl":"@alina"},
 "statistics":{"viewCount":"900000","subscriberCount":"12000","hiddenSubscriberCount":false,"videoCount":"40"}}]}`

func newTest(t *testing.T) (*Verifier, *fakeGoogle) {
	t.Helper()
	f := &fakeGoogle{tokenCode: 200, apiCode: 200, apiBody: channelOK,
		tokenBody: `{"access_token":"AT","expires_in":3599,"refresh_token":"RT","scope":"` + Scope + `","token_type":"Bearer"}`}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	v := New(Config{ClientID: "cid", ClientSecret: "csecret", HTTP: httpx.New(httpx.Options{Hosts: []string{host}, AllowHTTP: true, Site: "https://ohvat.test"}),
		TokenURL: srv.URL + "/token", RevokeURL: srv.URL + "/revoke", APIURL: srv.URL + "/yt"})
	return v, f
}

func TestStartURL(t *testing.T) {
	v, _ := newTest(t)
	if v.Platform() != domain.PlatformYouTube || v.Method() != domain.MethodOAuth {
		t.Fatal("площадка")
	}
	st, _ := v.Start(context.Background(), service.VerifyStart{State: "S", Challenge: "C", RedirectURL: "https://ohvat.test/api/social/youtube/callback"})
	u, _ := url.Parse(st.URL)
	q := u.Query()
	if u.Host != "accounts.google.com" || q.Get("scope") != Scope || q.Get("access_type") != "offline" || q.Get("state") != "S" ||
		q.Get("code_challenge") != "C" || q.Get("code_challenge_method") != "S256" || q.Get("include_granted_scopes") != "true" ||
		q.Get("client_id") != "cid" || q.Get("redirect_uri") != "https://ohvat.test/api/social/youtube/callback" {
		t.Errorf("%s", st.URL)
	}
}

func TestCheck(t *testing.T) {
	v, f := newTest(t)
	r, err := v.Check(context.Background(), service.VerifyCheck{Code: "CODE", Verifier: "VER", RedirectURL: "https://ohvat.test/cb"})
	if err != nil {
		t.Fatal(err)
	}
	form := f.forms[0]
	if form.Get("code") != "CODE" || form.Get("code_verifier") != "VER" || form.Get("grant_type") != "authorization_code" ||
		form.Get("client_secret") != "csecret" || form.Get("redirect_uri") != "https://ohvat.test/cb" {
		t.Errorf("обмен: %v", form)
	}
	if f.auth[0] != "Bearer AT" || f.queries[0].Get("mine") != "true" || f.queries[0].Get("part") != "snippet,statistics" {
		t.Errorf("channels.list: %v %v", f.auth, f.queries)
	}
	c := r.Channels[0]
	if r.Subject != "UCabc" || r.Tokens.Access != "AT" || r.Tokens.Refresh != "RT" || r.Tokens.AccessExpiresAt.Before(time.Now().Add(50*time.Minute)) ||
		c.ExternalID != "UCabc" || c.Handle != "alina" || c.URL != "https://www.youtube.com/@alina" || *c.Stats.Followers != 12000 ||
		*c.Stats.Views != 900000 || *c.Stats.Videos != 40 || c.Stats.FollowersHidden {
		t.Errorf("%+v %+v", r, c)
	}
	// подписчики скрыты — пометка, без числа; без @имени — ссылка на канал по id
	f.apiBody = `{"items":[{"id":"UCx","snippet":{"title":"X"},"statistics":{"hiddenSubscriberCount":true,"viewCount":"1","videoCount":"2"}}]}`
	ch, err := v.Refresh(context.Background(), service.RefreshInput{Account: domain.SocialAccount{ExternalID: "UCx"}, Tokens: &service.Tokens{Access: "AT2"}})
	if err != nil || !ch.Stats.FollowersHidden || ch.Stats.Followers != nil || ch.URL != "https://www.youtube.com/channel/UCx" {
		t.Fatalf("%v %+v", err, ch)
	}
	if q := f.queries[1]; q.Get("id") != "UCx" || q.Get("mine") != "" || f.auth[1] != "Bearer AT2" {
		t.Errorf("обновление по id: %v", q)
	}
}

func TestCheckErrors(t *testing.T) {
	ctx := context.Background()
	in := service.VerifyCheck{Code: "c", Verifier: "v"}
	cases := map[string]struct {
		mut  func(*fakeGoogle)
		want error
	}{
		"нет области":   {func(f *fakeGoogle) { f.tokenBody = `{"access_token":"AT","refresh_token":"RT","scope":"openid"}` }, service.ErrSocialDenied},
		"нет канала":    {func(f *fakeGoogle) { f.apiBody = `{"items":[]}` }, service.ErrSocialNoChannel},
		"invalid_grant": {func(f *fakeGoogle) { f.tokenCode, f.tokenBody = 400, `{"error":"invalid_grant"}` }, service.ErrSocialRevoked},
		"сбой Google":   {func(f *fakeGoogle) { f.tokenCode = 503 }, service.ErrSocialUnavailable},
		"квота": {func(f *fakeGoogle) {
			f.apiCode, f.apiBody = 403, `{"error":{"code":403,"errors":[{"reason":"quotaExceeded"}]}}`
		}, service.ErrSocialUnavailable},
		"нет прав": {func(f *fakeGoogle) {
			f.apiCode, f.apiBody = 403, `{"error":{"code":403,"errors":[{"reason":"insufficientPermissions"}]}}`
		}, service.ErrSocialRevoked},
		"401": {func(f *fakeGoogle) { f.apiCode, f.apiBody = 401, `{}` }, service.ErrSocialRevoked},
	}
	for name, c := range cases {
		v, f := newTest(t)
		c.mut(f)
		if _, err := v.Check(ctx, in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// нет refresh-токена / пустой токен — ошибка без утечки
	v, f := newTest(t)
	f.tokenBody = `{"access_token":"AT","scope":"` + Scope + `"}`
	if _, err := v.Check(ctx, in); err == nil {
		t.Error("без refresh-токена")
	}
	f.tokenBody = `{}`
	if _, err := v.Check(ctx, in); err == nil || strings.Contains(err.Error(), "csecret") {
		t.Errorf("пустой: %v", err)
	}
	if _, err := v.Refresh(ctx, service.RefreshInput{}); !errors.Is(err, service.ErrSocialRevoked) {
		t.Errorf("без токенов: %v", err)
	}
	f.apiBody = `{"items":[]}`
	if _, err := v.Refresh(ctx, service.RefreshInput{Account: domain.SocialAccount{ExternalID: "UCx"}, Tokens: &service.Tokens{Access: "a"}}); !errors.Is(err, service.ErrSocialNoChannel) {
		t.Errorf("канал удалён: %v", err)
	}
}

func TestRenewAndRevoke(t *testing.T) {
	ctx := context.Background()
	v, f := newTest(t)
	now := time.Now()
	fresh := service.Tokens{Access: "old", Refresh: "RT", AccessExpiresAt: now.Add(30 * time.Minute)}
	if out, changed, err := v.Renew(ctx, fresh, now); err != nil || changed || out.Access != "old" || len(f.forms) != 0 {
		t.Fatalf("ещё годен: %v %v", changed, err)
	}
	f.tokenBody = `{"access_token":"new","expires_in":3600}`
	out, changed, err := v.Renew(ctx, service.Tokens{Access: "old", Refresh: "RT", AccessExpiresAt: now.Add(time.Minute)}, now)
	if err != nil || !changed || out.Access != "new" || out.Refresh != "RT" || !out.AccessExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("%v %+v", err, out)
	}
	if fm := f.forms[0]; fm.Get("grant_type") != "refresh_token" || fm.Get("refresh_token") != "RT" {
		t.Errorf("%v", fm)
	}
	f.tokenCode, f.tokenBody = 400, `{"error":"invalid_grant"}`
	if _, _, err := v.Renew(ctx, service.Tokens{Refresh: "RT"}, now); !errors.Is(err, service.ErrSocialRevoked) {
		t.Errorf("отозван: %v", err)
	}
	if _, _, err := v.Renew(ctx, service.Tokens{}, now); !errors.Is(err, service.ErrSocialRevoked) {
		t.Errorf("без refresh: %v", err)
	}
	if err := v.Revoke(ctx, service.Tokens{Access: "A", Refresh: "R"}); err != nil || f.revoked[0] != "R" {
		t.Errorf("%v %v", err, f.revoked)
	}
	f.revokeCode = 400 // уже отозван — не ошибка
	if err := v.Revoke(ctx, service.Tokens{Access: "A"}); err != nil || f.revoked[1] != "A" {
		t.Errorf("%v %v", err, f.revoked)
	}
}

package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/service"
)

func TestAllowListAndNoRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
		case "/big":
			_, _ = w.Write([]byte(strings.Repeat("x", MaxResponse+100)))
		default:
			if r.Header.Get("User-Agent") != UserAgent+" (+https://ohvat.test)" {
				w.WriteHeader(400)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	c := New(Options{Hosts: []string{host}, AllowHTTP: true, Site: "https://ohvat.test"})
	ctx := context.Background()

	res, err := c.Get(ctx, srv.URL+"/ok", nil)
	var v struct{ OK bool }
	if err != nil || !res.OK() || res.JSON(&v) != nil || !v.OK {
		t.Fatalf("%v %+v", err, res)
	}
	// редирект не выполняется (на внутренний адрес не уйдём)
	res, err = c.Get(ctx, srv.URL+"/redirect", nil)
	if err != nil || res.Status != http.StatusFound {
		t.Fatalf("редирект: %v %d", err, res.Status)
	}
	// ответ обрезается
	if res, _ := c.Get(ctx, srv.URL+"/big", nil); len(res.Body) != MaxResponse {
		t.Errorf("размер: %d", len(res.Body))
	}
	// чужой хост, http без разрешения, логин в адресе — запрещены
	for _, raw := range []string{"http://169.254.169.254/", "http://localhost:1/", "https://evil.example/" + host, "http://u:p@" + host + "/"} {
		if _, err := c.Get(ctx, raw, nil); !errors.Is(err, service.ErrSocialUnavailable) || !strings.Contains(err.Error(), "не разрешён") {
			t.Errorf("%s: %v", raw, err)
		}
	}
	strict := New(Options{Hosts: []string{host}})
	if _, err := strict.Get(ctx, srv.URL+"/ok", nil); err == nil {
		t.Error("http без AllowHTTP")
	}
	if strict.Allowed(nil) {
		t.Error("nil")
	}
	if _, err := c.PostForm(ctx, srv.URL+"/ok", url.Values{"a": {"b"}}, map[string]string{"X": "1"}); err != nil {
		t.Error(err)
	}
	if _, err := c.Get(ctx, "://bad", nil); err == nil {
		t.Error("плохой адрес")
	}
	if _, err := c.PostForm(ctx, "://bad", nil, nil); err == nil {
		t.Error("плохой адрес")
	}
}

func TestErrorsWithoutSecrets(t *testing.T) {
	c := New(Options{Hosts: []string{"127.0.0.1:1"}, AllowHTTP: true, Timeout: time.Second})
	_, err := c.Get(context.Background(), "http://127.0.0.1:1/x?access_token=SECRET&code=C0DE", nil)
	if err == nil || !errors.Is(err, service.ErrSocialUnavailable) || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "C0DE") {
		t.Fatalf("%v", err)
	}
	if s := Redact("x?access_token=abc&code=123&client_secret=zz&state=ok"); s != "x?access_token=***&code=***&client_secret=***&state=ok" {
		t.Error(s)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Get(ctx, "http://127.0.0.1:1/", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("отмена: %v", err)
	}
}

func TestFailAndHelpers(t *testing.T) {
	for _, c := range []struct {
		r       Response
		revoked bool
		want    error
	}{
		{Response{Status: 429}, false, service.ErrSocialUnavailable},
		{Response{Status: 502}, true, service.ErrSocialUnavailable},
		{Response{Status: 401}, false, service.ErrSocialRevoked},
		{Response{Status: 400}, true, service.ErrSocialRevoked},
	} {
		if err := Fail("p", "x", c.r, c.revoked); !errors.Is(err, c.want) {
			t.Errorf("%d: %v", c.r.Status, err)
		}
	}
	if err := Fail("p", "x", Response{Status: 404}, false); errors.Is(err, service.ErrSocialRevoked) || errors.Is(err, service.ErrSocialUnavailable) {
		t.Error(err)
	}
	if OAuthError([]byte(`{"error":"invalid_grant"}`)) != "invalid_grant" || OAuthError([]byte(`{"error":{"code":1}}`)) != "" || OAuthError([]byte("x")) != "" {
		t.Error("OAuthError")
	}
	now := time.Now()
	if !Seconds(now, 0).IsZero() || !Seconds(now, 60).Equal(now.Add(time.Minute)) {
		t.Error("Seconds")
	}
	if (Response{Body: []byte("x")}).JSON(&struct{}{}) == nil {
		t.Error("не JSON")
	}
}

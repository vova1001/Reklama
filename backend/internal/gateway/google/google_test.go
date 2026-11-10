package google

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAuthURL(t *testing.T) {
	c := New("cid", "sec", "https://ohvat.ru/api/auth/google/callback")
	u, err := url.Parse(c.AuthURL("st", "ch"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != "cid" || q.Get("state") != "st" || q.Get("code_challenge") != "ch" || q.Get("code_challenge_method") != "S256" ||
		q.Get("redirect_uri") != "https://ohvat.ru/api/auth/google/callback" || q.Get("response_type") != "code" || !strings.Contains(q.Get("scope"), "email") {
		t.Errorf("%s", u)
	}
	if q.Get("client_secret") != "" {
		t.Error("секрет не должен попадать в адрес")
	}
}

func TestExchange(t *testing.T) {
	var form url.Values
	tokenStatus := 200
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		w.WriteHeader(tokenStatus)
		_, _ = w.Write([]byte(`{"access_token":"at"}`))
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"sub":"g1","email":"a@gmail.com","email_verified":true,"name":"Anna","picture":"https://p"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New("cid", "sec", "https://ohvat.ru/cb")
	c.TokenURL, c.UserinfoURL = srv.URL+"/token", srv.URL+"/userinfo"
	p, err := c.Exchange(context.Background(), "code1", "ver1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Subject != "g1" || !p.EmailVerified || p.Email != "a@gmail.com" || p.Name != "Anna" {
		t.Errorf("%+v", p)
	}
	if form.Get("code") != "code1" || form.Get("code_verifier") != "ver1" || form.Get("client_secret") != "sec" || form.Get("grant_type") != "authorization_code" {
		t.Errorf("форма: %v", form)
	}
	tokenStatus = 400
	if _, err := c.Exchange(context.Background(), "x", "y"); err == nil {
		t.Error("ошибка обмена")
	}
	c.UserinfoURL = srv.URL + "/nope"
	tokenStatus = 200
	if _, err := c.Exchange(context.Background(), "x", "y"); err == nil {
		t.Error("ошибка профиля")
	}
}

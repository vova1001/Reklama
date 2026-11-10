// Package google — клиент OAuth 2.0 Google (Authorization Code + PKCE). Реализует service.GoogleProvider.
package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ohvat/backend/internal/service"
)

// Адреса Google (https://developers.google.com/identity/protocols/oauth2/web-server).
const (
	AuthEndpoint     = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenEndpoint    = "https://oauth2.googleapis.com/token"
	UserinfoEndpoint = "https://openidconnect.googleapis.com/v1/userinfo"
)

// Client — OAuth-клиент Google.
type Client struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string // ${PUBLIC_URL}/api/auth/google/callback
	HTTP         *http.Client
	// для тестов
	TokenURL    string
	UserinfoURL string
}

// New создаёт клиента с таймаутом исходящих запросов 10 с.
func New(id, secret, redirect string) *Client {
	return &Client{ClientID: id, ClientSecret: secret, RedirectURL: redirect, HTTP: &http.Client{Timeout: 10 * time.Second},
		TokenURL: TokenEndpoint, UserinfoURL: UserinfoEndpoint}
}

// AuthURL — куда отправить человека: выбор аккаунта Google, затем возврат на RedirectURL с code и state.
func (c *Client) AuthURL(state, challenge string) string {
	v := url.Values{
		"client_id":             {c.ClientID},
		"redirect_uri":          {c.RedirectURL},
		"response_type":         {"code"},
		"scope":                 {"openid email profile"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"prompt":                {"select_account"},
	}
	return AuthEndpoint + "?" + v.Encode()
}

// Exchange меняет код на токен (с PKCE verifier) и читает профиль.
func (c *Client) Exchange(ctx context.Context, code, verifier string) (service.GoogleProfile, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"redirect_uri":  {c.RedirectURL},
		"grant_type":    {"authorization_code"},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return service.GoogleProfile{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := c.doJSON(req, &tok); err != nil {
		return service.GoogleProfile{}, fmt.Errorf("google token: %w", err)
	}
	if tok.AccessToken == "" {
		return service.GoogleProfile{}, fmt.Errorf("google token: пустой access_token")
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, c.UserinfoURL, nil)
	if err != nil {
		return service.GoogleProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	var u struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := c.doJSON(req, &u); err != nil {
		return service.GoogleProfile{}, fmt.Errorf("google userinfo: %w", err)
	}
	return service.GoogleProfile{Subject: u.Sub, Email: u.Email, EmailVerified: u.EmailVerified, Name: u.Name, Picture: u.Picture}, nil
}

func (c *Client) doJSON(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return err
	}
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("%s", res.Status) // тело ответа не пишем: в нём могут быть данные человека
	}
	return json.Unmarshal(body, out)
}

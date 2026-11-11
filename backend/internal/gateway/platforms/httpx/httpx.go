// Package httpx — исходящие запросы к API площадок: только https и только разрешённые хосты (защита от SSRF —
// адреса задаёт код, а не пользователь, и даже их проверяет транспорт), без переходов по редиректам, с таймаутом,
// ограничением размера ответа и честным User-Agent. Ошибки — без адреса запроса и тела (в них бывают токены).
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"ohvat/backend/internal/service"
)

// UserAgent — кто ходит в API площадок (адрес сайта добавляет Options.Site).
const UserAgent = "OhvatVerifier/1.0"

// MaxResponse — больше этого ответ не читается.
const MaxResponse = 1 << 20

// Options — настройки клиента.
type Options struct {
	Hosts     []string      // разрешённые хосты (точное совпадение)
	Timeout   time.Duration // на один запрос, 15 с
	AllowHTTP bool          // только для тестов (httptest без TLS)
	Site      string        // PUBLIC_URL — в User-Agent, чтобы площадка знала, кто обращается
	Transport http.RoundTripper
}

// Client — HTTP к площадкам.
type Client struct {
	hc    *http.Client
	hosts map[string]bool
	http  bool
	ua    string
}

// ErrHostNotAllowed — запрос не к разрешённому хосту площадки.
var ErrHostNotAllowed = errors.New("httpx: хост не разрешён")

// New создаёт клиента.
func New(o Options) *Client {
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Second
	}
	c := &Client{hosts: map[string]bool{}, http: o.AllowHTTP, ua: UserAgent}
	if o.Site != "" {
		c.ua += " (+" + o.Site + ")"
	}
	for _, h := range o.Hosts {
		c.hosts[strings.ToLower(h)] = true
	}
	tr := o.Transport
	if tr == nil {
		tr = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       90 * time.Second,
		}
	}
	c.hc = &http.Client{
		Timeout:   o.Timeout,
		Transport: guard{c: c, next: tr},
		// редиректы не выполняем: API площадок отвечают сразу, а переход мог бы увести на чужой хост
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return c
}

// Allowed — адрес на разрешённом хосте и по https.
func (c *Client) Allowed(u *url.URL) bool {
	if u == nil || u.User != nil {
		return false
	}
	if u.Scheme != "https" && !(c.http && u.Scheme == "http") {
		return false
	}
	return c.hosts[strings.ToLower(u.Host)]
}

type guard struct {
	c    *Client
	next http.RoundTripper
}

func (g guard) RoundTrip(r *http.Request) (*http.Response, error) {
	if !g.c.Allowed(r.URL) {
		return nil, ErrHostNotAllowed
	}
	return g.next.RoundTrip(r)
}

// Response — ответ площадки: код и тело (до MaxResponse).
type Response struct {
	Status int
	Body   []byte
	Header http.Header
}

// Do выполняет запрос. Ошибка сети — service.ErrSocialUnavailable (с причиной без адреса).
func (c *Client) Do(req *http.Request) (Response, error) {
	req.Header.Set("User-Agent", c.ua)
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("%w: %s %s: %w", service.ErrSocialUnavailable, req.Method, req.URL.Host, clean(err))
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, MaxResponse))
	if err != nil {
		return Response{}, fmt.Errorf("%w: %s: чтение ответа: %w", service.ErrSocialUnavailable, req.URL.Host, clean(err))
	}
	return Response{Status: res.StatusCode, Body: body, Header: res.Header}, nil
}

// Get — GET с заголовками.
func (c *Client) Get(ctx context.Context, raw string, header map[string]string) (Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return Response{}, errors.New("httpx: неверный адрес")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return c.Do(req)
}

// PostForm — POST application/x-www-form-urlencoded.
func (c *Client) PostForm(ctx context.Context, raw string, form url.Values, header map[string]string) (Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, raw, strings.NewReader(form.Encode()))
	if err != nil {
		return Response{}, errors.New("httpx: неверный адрес")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return c.Do(req)
}

// JSON — разобрать тело.
func (r Response) JSON(out any) error {
	if err := json.Unmarshal(r.Body, out); err != nil {
		return fmt.Errorf("ответ не JSON (HTTP %d)", r.Status)
	}
	return nil
}

// Temporary — стоит повторить позже: лимит площадки или её сбой.
func (r Response) Temporary() bool { return r.Status == http.StatusTooManyRequests || r.Status >= 500 }

// OK — 2xx.
func (r Response) OK() bool { return r.Status/100 == 2 }

// clean — ошибка без адреса запроса (в нём бывают токены).
func clean(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, ErrHostNotAllowed):
		return ErrHostNotAllowed
	}
	return errors.New(Redact(err.Error()))
}

// Redact убирает из строки значения параметров с токенами (access_token=…, code=…, client_secret=…).
func Redact(s string) string { return reSecret.ReplaceAllString(s, "$1=***") }

var reSecret = regexp.MustCompile(`(?i)\b(access_token|refresh_token|client_secret|code|token)=[^&\s"]+`)

// Fail — ошибка площадки по ответу: временная (429, 5xx) — ErrSocialUnavailable; отказ в доступе (401/403 или
// invalid_grant) — ErrSocialRevoked; прочее — с кодом ответа для журнала. what — что делали.
func Fail(platform, what string, r Response, revoked bool) error {
	switch {
	case r.Temporary():
		return fmt.Errorf("%w: %s %s: HTTP %d", service.ErrSocialUnavailable, platform, what, r.Status)
	case revoked || r.Status == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s %s: HTTP %d", service.ErrSocialRevoked, platform, what, r.Status)
	}
	return fmt.Errorf("%s %s: HTTP %d", platform, what, r.Status)
}

// OAuthError — поле error в ответе OAuth (invalid_grant и т. п.).
func OAuthError(body []byte) string {
	var e struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil {
		return ""
	}
	if s, ok := e.Error.(string); ok {
		return s
	}
	return ""
}

// Seconds — срок жизни в секундах от now; 0 — нет срока.
func Seconds(now time.Time, sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(sec) * time.Second)
}

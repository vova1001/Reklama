// Package youtube — подтверждение канала YouTube: Google OAuth 2.0 (Authorization Code + PKCE, только область
// youtube.readonly, запрашивается отдельно при подключении — не при входе) и YouTube Data API v3
// channels.list (mine=true при подключении, id=… при обновлении). Реализует service.PlatformVerifier и TokenKeeper.
//
// Документация: https://developers.google.com/identity/protocols/oauth2/web-server,
// https://developers.google.com/youtube/v3/docs/channels/list.
package youtube

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/gateway/platforms/httpx"
	"ohvat/backend/internal/service"
)

// Адреса Google.
const (
	AuthEndpoint   = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenEndpoint  = "https://oauth2.googleapis.com/token"
	RevokeEndpoint = "https://oauth2.googleapis.com/revoke"
	APIBase        = "https://www.googleapis.com/youtube/v3"
	Scope          = "https://www.googleapis.com/auth/youtube.readonly"
)

// Hosts — куда ходит адаптер.
var Hosts = []string{"oauth2.googleapis.com", "www.googleapis.com"}

// Config — ключи Google (те же, что для входа) и адреса (в тестах — подставной сервер).
type Config struct {
	ClientID     string
	ClientSecret string
	HTTP         *httpx.Client
	TokenURL     string
	RevokeURL    string
	APIURL       string
}

// Verifier — YouTube.
type Verifier struct{ c Config }

// New создаёт адаптер.
func New(c Config) *Verifier {
	if c.TokenURL == "" {
		c.TokenURL = TokenEndpoint
	}
	if c.RevokeURL == "" {
		c.RevokeURL = RevokeEndpoint
	}
	if c.APIURL == "" {
		c.APIURL = APIBase
	}
	return &Verifier{c: c}
}

// Platform — youtube.
func (*Verifier) Platform() domain.SocialPlatform { return domain.PlatformYouTube }

// Method — OAuth.
func (*Verifier) Method() domain.SocialMethod { return domain.MethodOAuth }

// Start — страница разрешения Google: только youtube.readonly, офлайн-доступ (refresh-токен для ежедневного
// обновления), prompt=consent — иначе Google не выдаст refresh-токен повторно.
func (v *Verifier) Start(_ context.Context, in service.VerifyStart) (service.VerifyStep, error) {
	q := url.Values{
		"client_id":              {v.c.ClientID},
		"redirect_uri":           {in.RedirectURL},
		"response_type":          {"code"},
		"scope":                  {Scope},
		"access_type":            {"offline"},
		"include_granted_scopes": {"true"},
		"prompt":                 {"consent select_account"},
		"state":                  {in.State},
		"code_challenge":         {in.Challenge},
		"code_challenge_method":  {"S256"},
	}
	return service.VerifyStep{URL: AuthEndpoint + "?" + q.Encode()}, nil
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// Check — обмен кода и канал автора (channels.list mine=true).
func (v *Verifier) Check(ctx context.Context, in service.VerifyCheck) (service.VerifyResult, error) {
	now := time.Now().UTC()
	res, err := v.c.HTTP.PostForm(ctx, v.c.TokenURL, url.Values{
		"code": {in.Code}, "client_id": {v.c.ClientID}, "client_secret": {v.c.ClientSecret},
		"redirect_uri": {in.RedirectURL}, "grant_type": {"authorization_code"}, "code_verifier": {in.Verifier},
	}, nil)
	if err != nil {
		return service.VerifyResult{}, err
	}
	if !res.OK() {
		return service.VerifyResult{}, httpx.Fail("youtube", "обмен кода", res, httpx.OAuthError(res.Body) == "invalid_grant")
	}
	var t tokenResp
	if err := res.JSON(&t); err != nil || t.AccessToken == "" {
		return service.VerifyResult{}, fmt.Errorf("youtube: обмен кода: пустой токен")
	}
	// при детальном согласии автор мог снять галочку YouTube
	if !strings.Contains(" "+t.Scope+" ", " "+Scope+" ") {
		return service.VerifyResult{}, fmt.Errorf("%w: youtube: область youtube.readonly не выдана", service.ErrSocialDenied)
	}
	tok := service.Tokens{Access: t.AccessToken, Refresh: t.RefreshToken, AccessExpiresAt: httpx.Seconds(now, t.ExpiresIn), Scope: t.Scope}
	chans, err := v.channels(ctx, tok.Access, url.Values{"mine": {"true"}})
	if err != nil {
		return service.VerifyResult{}, err
	}
	if len(chans) == 0 {
		return service.VerifyResult{}, fmt.Errorf("%w: youtube: у аккаунта нет канала", service.ErrSocialNoChannel)
	}
	if tok.Refresh == "" {
		// без refresh-токена ежедневное обновление невозможно; prompt=consent его выдаёт всегда
		return service.VerifyResult{}, fmt.Errorf("youtube: Google не выдал refresh-токен")
	}
	return service.VerifyResult{Subject: chans[0].ExternalID, Channels: chans, Tokens: &tok}, nil
}

// Refresh — цифры канала по id.
func (v *Verifier) Refresh(ctx context.Context, in service.RefreshInput) (service.VerifiedChannel, error) {
	if in.Tokens == nil {
		return service.VerifiedChannel{}, service.ErrSocialRevoked
	}
	chans, err := v.channels(ctx, in.Tokens.Access, url.Values{"id": {in.Account.ExternalID}})
	if err != nil {
		return service.VerifiedChannel{}, err
	}
	if len(chans) == 0 {
		return service.VerifiedChannel{}, fmt.Errorf("%w: youtube: канал удалён", service.ErrSocialNoChannel)
	}
	return chans[0], nil
}

type channelsResp struct {
	Items []struct {
		ID      string `json:"id"`
		Snippet struct {
			Title     string `json:"title"`
			CustomURL string `json:"customUrl"`
		} `json:"snippet"`
		Statistics struct {
			ViewCount             string `json:"viewCount"`
			SubscriberCount       string `json:"subscriberCount"`
			HiddenSubscriberCount bool   `json:"hiddenSubscriberCount"`
			VideoCount            string `json:"videoCount"`
		} `json:"statistics"`
	} `json:"items"`
	Error *struct {
		Code   int `json:"code"`
		Errors []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
	} `json:"error"`
}

func (v *Verifier) channels(ctx context.Context, access string, q url.Values) ([]service.VerifiedChannel, error) {
	q.Set("part", "snippet,statistics")
	q.Set("maxResults", "5")
	res, err := v.c.HTTP.Get(ctx, v.c.APIURL+"/channels?"+q.Encode(), map[string]string{"Authorization": "Bearer " + access})
	if err != nil {
		return nil, err
	}
	var body channelsResp
	_ = res.JSON(&body)
	if !res.OK() {
		reason := ""
		if body.Error != nil && len(body.Error.Errors) > 0 {
			reason = body.Error.Errors[0].Reason
		}
		switch reason {
		case "quotaExceeded", "rateLimitExceeded", "userRateLimitExceeded":
			return nil, fmt.Errorf("%w: youtube: квота API (%s)", service.ErrSocialUnavailable, reason)
		case "insufficientPermissions", "forbidden", "authError":
			return nil, fmt.Errorf("%w: youtube: %s", service.ErrSocialRevoked, reason)
		}
		return nil, httpx.Fail("youtube", "channels.list", res, res.Status == http.StatusForbidden)
	}
	out := make([]service.VerifiedChannel, 0, len(body.Items))
	for _, it := range body.Items {
		if it.ID == "" {
			continue
		}
		ch := service.VerifiedChannel{ExternalID: it.ID, Kind: domain.SocialChannel, Title: it.Snippet.Title,
			URL: "https://www.youtube.com/channel/" + url.PathEscape(it.ID)}
		if h := strings.TrimPrefix(it.Snippet.CustomURL, "@"); h != "" {
			ch.Handle = h
			ch.URL = "https://www.youtube.com/@" + url.PathEscape(h)
		}
		st := it.Statistics
		ch.Stats.FollowersHidden = st.HiddenSubscriberCount
		if !st.HiddenSubscriberCount {
			ch.Stats.Followers = num(st.SubscriberCount)
		}
		ch.Stats.Views, ch.Stats.Videos = num(st.ViewCount), num(st.VideoCount)
		out = append(out, ch)
	}
	return out, nil
}

// Renew — новый токен доступа по refresh-токену, если до конца текущего меньше 2 минут
// (Google не меняет refresh-токен; invalid_grant — автор отозвал доступ).
func (v *Verifier) Renew(ctx context.Context, t service.Tokens, now time.Time) (service.Tokens, bool, error) {
	if !t.AccessExpiresAt.IsZero() && t.AccessExpiresAt.Sub(now) > 2*time.Minute {
		return t, false, nil
	}
	if t.Refresh == "" {
		return t, false, service.ErrSocialRevoked
	}
	res, err := v.c.HTTP.PostForm(ctx, v.c.TokenURL, url.Values{
		"client_id": {v.c.ClientID}, "client_secret": {v.c.ClientSecret}, "refresh_token": {t.Refresh}, "grant_type": {"refresh_token"},
	}, nil)
	if err != nil {
		return t, false, err
	}
	if !res.OK() {
		return t, false, httpx.Fail("youtube", "обновление токена", res, httpx.OAuthError(res.Body) == "invalid_grant")
	}
	var r tokenResp
	if err := res.JSON(&r); err != nil || r.AccessToken == "" {
		return t, false, fmt.Errorf("youtube: обновление токена: пустой ответ")
	}
	t.Access, t.AccessExpiresAt, t.IssuedAt = r.AccessToken, httpx.Seconds(now, r.ExpiresIn), now
	if r.RefreshToken != "" {
		t.Refresh = r.RefreshToken
	}
	return t, true, nil
}

// Revoke — отозвать доступ у Google (refresh-токен отзывает и выданные по нему токены доступа).
func (v *Verifier) Revoke(ctx context.Context, t service.Tokens) error {
	tok := t.Refresh
	if tok == "" {
		tok = t.Access
	}
	res, err := v.c.HTTP.PostForm(ctx, v.c.RevokeURL, url.Values{"token": {tok}}, nil)
	if err != nil {
		return err
	}
	if !res.OK() && httpx.OAuthError(res.Body) != "invalid_token" {
		return httpx.Fail("youtube", "отзыв доступа", res, false)
	}
	return nil
}

func num(s string) *int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return nil
	}
	return &n
}

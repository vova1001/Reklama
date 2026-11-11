package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// SocialService — подтверждение соцсетей автора.
type SocialService interface {
	Overview(ctx context.Context, u domain.User) ([]service.PlatformView, error)
	Start(ctx context.Context, u domain.User, p domain.SocialPlatform) (service.SocialStart, error)
	Callback(ctx context.Context, u domain.User, p domain.SocialPlatform, in service.SocialCallback) ([]domain.SocialAccount, error)
	RefreshNow(ctx context.Context, u domain.User, id int64) (domain.SocialAccount, error)
	Unlink(ctx context.Context, u domain.User, id int64) error
}

const socialCookie = "ohvat_social" // state OAuth: возврат с площадки принимается только в том же браузере

// socialReturn — куда вернуть автора после площадки: раздел «Соцсети» анкеты с итогом.
const socialReturn = "/app/card"

type socialAccountDTO struct {
	ID              int64      `json:"id"`
	Kind            string     `json:"kind"`
	Handle          string     `json:"handle"`
	Title           string     `json:"title"`
	URL             string     `json:"url"`
	Status          string     `json:"status"`
	Followers       *int64     `json:"followers"`
	FollowersHidden bool       `json:"followersHidden"`
	Views           *int64     `json:"views"`
	Videos          *int64     `json:"videos"`
	Likes           *int64     `json:"likes"`
	FailReason      string     `json:"failReason"`
	VerifiedAt      *time.Time `json:"verifiedAt"`
	RefreshedAt     *time.Time `json:"refreshedAt"`
}

type socialPlatformDTO struct {
	Platform  string             `json:"platform"`
	Available bool               `json:"available"`
	Method    string             `json:"method"`
	Status    string             `json:"status"`
	Accounts  []socialAccountDTO `json:"accounts"`
}

type socialOut struct {
	Platforms []socialPlatformDTO `json:"platforms"`
}

type socialStartOut struct {
	URL string `json:"url"`
}

type socialAccountOut struct {
	Account socialAccountDTO `json:"account"`
}

func toSocialAccount(a domain.SocialAccount) socialAccountDTO {
	return socialAccountDTO{ID: a.ID, Kind: a.Kind, Handle: a.Handle, Title: a.Title, URL: a.URL, Status: string(a.Status),
		Followers: a.Stats.Followers, FollowersHidden: a.Stats.FollowersHidden, Views: a.Stats.Views, Videos: a.Stats.Videos,
		Likes: a.Stats.Likes, FailReason: a.FailReason, VerifiedAt: utcPtr(a.VerifiedAt), RefreshedAt: utcPtr(a.RefreshedAt)}
}

// GET /api/me/social — площадки, статусы, подтверждённые аккаунты.
func (s *Server) getSocial(w http.ResponseWriter, r *http.Request) {
	vs, err := s.social.Overview(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := socialOut{Platforms: make([]socialPlatformDTO, 0, len(vs))}
	for _, v := range vs {
		p := socialPlatformDTO{Platform: string(v.Platform), Available: v.Available, Method: string(v.Method), Status: string(v.Status),
			Accounts: make([]socialAccountDTO, 0, len(v.Accounts))}
		for _, a := range v.Accounts {
			p.Accounts = append(p.Accounts, toSocialAccount(a))
		}
		out.Platforms = append(out.Platforms, p)
	}
	writeJSON(w, http.StatusOK, out)
}

func platformParam(r *http.Request) (domain.SocialPlatform, bool) {
	p := domain.SocialPlatform(chi.URLParam(r, "platform"))
	return p, p.Valid()
}

// POST /api/me/social/{platform}/start — адрес разрешения площадки (или ссылка на бота); state OAuth — в cookie.
func (s *Server) postSocialStart(w http.ResponseWriter, r *http.Request) {
	p, ok := platformParam(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "Такой площадки нет.")
		return
	}
	st, err := s.social.Start(r.Context(), currentUser(r), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if st.State != "" {
		http.SetCookie(w, &http.Cookie{Name: socialCookie, Value: st.State, Path: "/api/social", MaxAge: 600,
			HttpOnly: true, Secure: s.opt.SecureCookies, SameSite: http.SameSiteLaxMode})
	}
	writeJSON(w, http.StatusOK, socialStartOut{URL: st.URL})
}

// GET /api/social/{platform}/callback — возврат с площадки: state из cookie = state в адресе, сессия — того же
// автора, обмен кода → аккаунты. Итог — переходом в анкету (?social=<площадка>&result=…).
func (s *Server) getSocialCallback(w http.ResponseWriter, r *http.Request) {
	p, ok := platformParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	back := func(result string) {
		http.Redirect(w, r, socialReturn+"?"+url.Values{"social": {string(p)}, "result": {result}}.Encode()+"#social", http.StatusSeeOther)
	}
	q := r.URL.Query()
	c, cerr := r.Cookie(socialCookie)
	s.clearCookie(w, socialCookie, "/api/social")
	sess, err := r.Cookie(sessionCookie)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	u, err := s.auth.Session(r.Context(), sess.Value)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	state := q.Get("state")
	if cerr != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		slog.WarnContext(r.Context(), "соцсети: state не совпал или cookie истекла", "platform", p)
		back("state")
		return
	}
	_, err = s.social.Callback(r.Context(), u, p, service.SocialCallback{
		State: state, Code: q.Get("code"), Error: q.Get("error"), DeviceID: q.Get("device_id"),
	})
	if err != nil {
		back(socialResult(r, p, err))
		return
	}
	back("ok")
}

// socialResult — код итога для страницы анкеты; неожиданное — в журнал.
func socialResult(r *http.Request, p domain.SocialPlatform, err error) string {
	var le *service.LimitError
	switch {
	case errors.Is(err, service.ErrSocialState):
		return "state"
	case errors.Is(err, service.ErrSocialDenied):
		return "denied"
	case errors.Is(err, service.ErrSocialNoChannel):
		return "no_channel"
	case errors.Is(err, service.ErrSocialTooMany):
		return "too_many"
	case errors.Is(err, service.ErrSocialDisabled):
		return "disabled"
	case errors.As(err, &le):
		return "limit"
	case errors.Is(err, service.ErrSocialUnavailable):
		slog.WarnContext(r.Context(), "соцсети: площадка недоступна", "platform", p, "err", err)
		return "unavailable"
	}
	slog.ErrorContext(r.Context(), "соцсети: подключение", "platform", p, "err", err)
	return "error"
}

func accountParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "Не найдено. Обновите страницу.")
		return 0, false
	}
	return id, true
}

// POST /api/me/social/accounts/{id}/refresh — проверить ещё раз сейчас.
func (s *Server) postSocialRefresh(w http.ResponseWriter, r *http.Request) {
	id, ok := accountParam(w, r)
	if !ok {
		return
	}
	a, err := s.social.RefreshNow(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, socialAccountOut{Account: toSocialAccount(a)})
}

// DELETE /api/me/social/accounts/{id} — отвязать (последний аккаунт доступа — токен отзывается).
func (s *Server) deleteSocialAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := accountParam(w, r)
	if !ok {
		return
	}
	if err := s.social.Unlink(r.Context(), currentUser(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

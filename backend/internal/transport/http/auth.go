package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// GET /api/auth/config — включённые способы входа и CSRF-токен (cookie ставится тут же).
func (s *Server) getAuthConfig(w http.ResponseWriter, r *http.Request) {
	tok, err := s.csrfToken(w, r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	m := s.auth.Methods()
	out := authConfigDTO{Email: m.Email, Google: m.Google, CSRFToken: tok}
	if m.TelegramBotID != "" {
		out.Telegram = &telegramDTO{BotID: m.TelegramBotID, BotName: m.TelegramBotName}
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/auth/email/start — отправить код на почту.
func (s *Server) postEmailStart(w http.ResponseWriter, r *http.Request) {
	var in emailStartIn
	if !decode(w, r, &in) {
		return
	}
	sent, err := s.auth.StartEmail(r.Context(), in.Email, in.Role, in.Consent, clientIP(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, codeSentDTO{
		Email: sent.Email, ResendAfter: int(sent.ResendAfter.Seconds()), ExpiresIn: int(sent.ExpiresIn.Seconds()),
	})
}

// POST /api/auth/email/verify — проверить код и войти.
func (s *Server) postEmailVerify(w http.ResponseWriter, r *http.Request) {
	var in emailVerifyIn
	if !decode(w, r, &in) {
		return
	}
	res, err := s.auth.VerifyEmail(r.Context(), in.Email, in.Code)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.loggedIn(w, res)
}

// POST /api/auth/telegram — данные Telegram Login (подписанные Telegram) и роль.
func (s *Server) postTelegram(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role    string                     `json:"role"`
		Consent bool                       `json:"consent"`
		User    map[string]json.RawMessage `json:"user"`
	}
	if !decode(w, r, &in) {
		return
	}
	data := make(map[string]string, len(in.User))
	for k, raw := range in.User {
		v, ok := scalar(raw)
		if !ok {
			s.fail(w, r, service.ErrTelegramInvalid)
			return
		}
		data[k] = v
	}
	res, err := s.auth.LoginTelegram(r.Context(), data, in.Role, in.Consent)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.loggedIn(w, res)
}

// GET /api/auth/google/start?role=&consent=1 — переход в Google; state, PKCE verifier и роль — в короткой httpOnly-cookie.
func (s *Server) getGoogleStart(w http.ResponseWriter, r *http.Request) {
	role := r.URL.Query().Get("role")
	if !domain.ValidRole(role) {
		role = ""
	}
	st, err := s.auth.StartGoogle(r.URL.Query().Get("consent") == "1")
	if err != nil {
		var ve *service.ValidationError
		switch {
		case errors.Is(err, service.ErrProviderDisabled):
			http.Redirect(w, r, "/login?error=google_off", http.StatusSeeOther)
			return
		case errors.As(err, &ve):
			http.Redirect(w, r, "/login?error=consent", http.StatusSeeOther)
			return
		}
		slog.ErrorContext(r.Context(), "Google: начало входа", "err", err)
		http.Redirect(w, r, "/login?error=google", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: st.State + "." + st.Verifier + "." + role, Path: "/api/auth/google", MaxAge: 600,
		HttpOnly: true, Secure: s.opt.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, st.URL, http.StatusFound)
}

// GET /api/auth/google/callback — возврат из Google: сверка state, обмен кода, вход, переход в кабинет или анкету.
func (s *Server) getGoogleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, cerr := r.Cookie(oauthCookie)
	s.clearCookie(w, oauthCookie, "/api/auth/google")
	if q.Get("error") != "" {
		http.Redirect(w, r, "/login?error=google_cancel", http.StatusSeeOther)
		return
	}
	var state, verifier, role string
	if cerr == nil {
		parts := strings.SplitN(c.Value, ".", 3)
		if len(parts) == 3 {
			state, verifier, role = parts[0], parts[1], parts[2]
		}
	}
	got := q.Get("state")
	if state == "" || got == "" || subtle.ConstantTimeCompare([]byte(state), []byte(got)) != 1 {
		slog.WarnContext(r.Context(), "Google: state не совпал или cookie истекла")
		http.Redirect(w, r, "/login?error=google_state", http.StatusSeeOther)
		return
	}
	res, err := s.auth.LoginGoogle(r.Context(), q.Get("code"), verifier, role)
	if err != nil {
		if !errors.Is(err, service.ErrGoogleFailed) {
			slog.ErrorContext(r.Context(), "Google: вход", "err", err)
		}
		http.Redirect(w, r, "/login?error=google", http.StatusSeeOther)
		return
	}
	s.setSession(w, res.Token, res.ExpiresAt)
	http.Redirect(w, r, nextPath(res.User), http.StatusSeeOther)
}

// POST /api/auth/logout — выход: сессия удаляется в базе, cookie стирается.
func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.auth.Logout(r.Context(), c.Value); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	s.clearCookie(w, sessionCookie, "/")
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/me — кто вошёл.
func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	acc, err := s.auth.Account(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, meDTO{User: toUserDTO(acc.User), Providers: nonNilStr(acc.Providers)})
}

// PUT /api/me/profile — анкета первого входа.
func (s *Server) putProfile(w http.ResponseWriter, r *http.Request) {
	var in profileIn
	if !decode(w, r, &in) {
		return
	}
	u, err := s.auth.SaveProfile(r.Context(), currentUser(r), domain.Profile{
		Role: in.Role, Name: in.Name, City: in.City, Spheres: in.Spheres, Channels: in.Channels,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, loginDTO{User: toUserDTO(u), Next: nextPath(u)})
}

// loggedIn — сессия в cookie и ответ «кто вошёл и куда дальше».
func (s *Server) loggedIn(w http.ResponseWriter, res service.LoginResult) {
	s.setSession(w, res.Token, res.ExpiresAt)
	writeJSON(w, http.StatusOK, loginDTO{User: toUserDTO(res.User), Next: nextPath(res.User), Created: res.Created})
}

// nextPath — после входа: анкета, если она не заполнена, иначе кабинет.
func nextPath(u domain.User) string {
	if !u.Onboarded() {
		return "/login?step=profile"
	}
	return "/account"
}

// decode читает JSON тела (до 16 КБ, без лишних полей); ошибка — ответ 400.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Не удалось прочитать данные формы. Обновите страницу и попробуйте снова.")
		return false
	}
	return true
}

// scalar — строка или число из JSON как текст (Telegram присылает id и auth_date числами).
func scalar(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	var n json.Number
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&n); err == nil {
		if _, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
			return n.String(), true
		}
	}
	return "", false
}

// fail переводит ошибку сервиса в ответ; внутренние ошибки — в журнал, наружу — общий текст.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var (
		ve *service.ValidationError
		le *service.LimitError
		ce *service.CodeInvalidError
	)
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, errorDTO{Error: errorBody{Code: "validation", Message: "Проверьте поля формы.", Fields: ve.Fields}})
	case errors.As(err, &le):
		sec := int(math.Ceil(le.RetryAfter.Seconds()))
		if sec < 1 {
			sec = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(sec))
		writeJSON(w, http.StatusTooManyRequests, errorDTO{Error: errorBody{Code: "rate_limited", Message: limitMessage(le.Reason, sec), RetryAfter: sec}})
	case errors.As(err, &ce):
		left := ce.Left
		writeJSON(w, http.StatusBadRequest, errorDTO{Error: errorBody{Code: "code_invalid", Message: codeInvalidMessage(left), AttemptsLeft: &left}})
	case errors.Is(err, service.ErrCodeExpired):
		writeError(w, http.StatusBadRequest, "code_expired", "Код устарел или уже использован. Запросите новый.")
	case errors.Is(err, service.ErrTooManyAttempts):
		zero := 0
		writeJSON(w, http.StatusTooManyRequests, errorDTO{Error: errorBody{Code: "too_many_attempts", Message: "Попытки закончились. Запросите новый код.", AttemptsLeft: &zero}})
	case errors.Is(err, service.ErrUnauthorized):
		s.clearCookie(w, sessionCookie, "/")
		writeError(w, http.StatusUnauthorized, "unauthorized", "Войдите, чтобы продолжить.")
	case errors.Is(err, service.ErrTelegramInvalid):
		writeError(w, http.StatusBadRequest, "telegram_invalid", "Telegram не подтвердил вход. Попробуйте ещё раз.")
	case errors.Is(err, service.ErrProviderDisabled):
		writeError(w, http.StatusServiceUnavailable, "provider_disabled", "Этот способ входа сейчас недоступен. Выберите другой.")
	case errors.Is(err, service.ErrMailUnavailable):
		writeError(w, http.StatusServiceUnavailable, "mail_unavailable", "Не получилось отправить письмо. Попробуйте через минуту или войдите другим способом.")
	case errors.Is(err, service.ErrRoleLocked):
		writeError(w, http.StatusConflict, "role_locked", "Роль уже выбрана — для другой роли нужен отдельный аккаунт.")
	default:
		slog.ErrorContext(r.Context(), "ошибка запроса", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Что-то пошло не так. Попробуйте позже.")
	}
}

func limitMessage(reason string, sec int) string {
	switch reason {
	case "email_cooldown":
		return fmt.Sprintf("Новый код можно запросить через %d с.", sec)
	case "email_hourly":
		return fmt.Sprintf("На эту почту ушло много писем. Попробуйте через %s или войдите другим способом.", minutes(sec))
	default:
		return fmt.Sprintf("Слишком много запросов с вашего адреса. Попробуйте через %s.", minutes(sec))
	}
}

func minutes(sec int) string {
	m := (sec + 59) / 60
	return fmt.Sprintf("%d мин", m)
}

func codeInvalidMessage(left int) string {
	return fmt.Sprintf("Неверный код. Осталось попыток: %d.", left)
}

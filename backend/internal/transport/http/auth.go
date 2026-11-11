package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

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
		out.Telegram = &telegramDTO{BotID: m.TelegramBotID, BotName: m.TelegramBotName, Bot: m.TelegramBot}
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/auth/email/start — отправить код на почту.
func (s *Server) postEmailStart(w http.ResponseWriter, r *http.Request) {
	var in emailStartIn
	if !decode(w, r, &in) {
		return
	}
	sent, err := s.auth.StartEmail(r.Context(), in.Email, in.Consent, clientIP(r))
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

// POST /api/auth/telegram — данные Telegram Login (подписанные Telegram).
func (s *Server) postTelegram(w http.ResponseWriter, r *http.Request) {
	var in struct {
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
	res, err := s.auth.LoginTelegram(r.Context(), data, in.Consent)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.loggedIn(w, res)
}

// POST /api/auth/telegram/bot/start — ссылка на бота; секрет браузера — в httpOnly-cookie (только этот браузер
// сможет забрать вход).
func (s *Server) postBotStart(w http.ResponseWriter, r *http.Request) {
	var in botStartIn
	if !decode(w, r, &in) {
		return
	}
	st, err := s.auth.StartTelegramBot(r.Context(), in.Consent, clientIP(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: botCookie, Value: st.BrowserSecret, Path: botCookiePath, MaxAge: int((st.ExpiresIn + 2*time.Minute).Seconds()),
		HttpOnly: true, Secure: s.opt.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusCreated, botStartDTO{URL: st.URL, ExpiresIn: int(st.ExpiresIn.Seconds())})
}

// POST /api/auth/telegram/bot/status — ждать подтверждения в боте (до BotWait); подтверждено — сессия.
func (s *Server) postBotStatus(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(botCookie)
	if err != nil {
		s.fail(w, r, service.ErrBotLoginExpired)
		return
	}
	st, res, err := s.auth.WaitTelegramBot(r.Context(), c.Value, s.opt.BotWait)
	if r.Context().Err() != nil {
		return // страницу закрыли — отвечать некому
	}
	if err != nil {
		if errors.Is(err, service.ErrBotLoginExpired) || errors.Is(err, service.ErrBotLoginCancelled) {
			s.clearCookie(w, botCookie, botCookiePath)
		}
		s.fail(w, r, err)
		return
	}
	if st != service.BotLoginDone {
		writeJSON(w, http.StatusOK, botStatusDTO{Status: string(service.BotLoginWaiting)})
		return
	}
	s.clearCookie(w, botCookie, botCookiePath)
	s.setSession(w, res.Token, res.ExpiresAt)
	u := toUserDTO(res.User)
	writeJSON(w, http.StatusOK, botStatusDTO{Status: string(service.BotLoginDone), User: &u, Next: nextPath(res.User), Created: res.Created})
}

// GET /api/auth/google/start?consent=1 — переход в Google; state и PKCE verifier — в короткой httpOnly-cookie.
func (s *Server) getGoogleStart(w http.ResponseWriter, r *http.Request) {
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
		Name: oauthCookie, Value: st.State + "." + st.Verifier, Path: "/api/auth/google", MaxAge: 600,
		HttpOnly: true, Secure: s.opt.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, st.URL, http.StatusFound)
}

// GET /api/auth/google/callback — возврат из Google: сверка state, обмен кода, вход, переход в приложение.
func (s *Server) getGoogleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, cerr := r.Cookie(oauthCookie)
	s.clearCookie(w, oauthCookie, "/api/auth/google")
	if q.Get("error") != "" {
		http.Redirect(w, r, "/login?error=google_cancel", http.StatusSeeOther)
		return
	}
	var state, verifier string
	if cerr == nil {
		// «state.verifier»; третья часть (роль) — от прежней версии, игнорируется
		parts := strings.SplitN(c.Value, ".", 3)
		if len(parts) >= 2 {
			state, verifier = parts[0], parts[1]
		}
	}
	got := q.Get("state")
	if state == "" || got == "" || subtle.ConstantTimeCompare([]byte(state), []byte(got)) != 1 {
		slog.WarnContext(r.Context(), "Google: state не совпал или cookie истекла")
		http.Redirect(w, r, "/login?error=google_state", http.StatusSeeOther)
		return
	}
	res, err := s.auth.LoginGoogle(r.Context(), q.Get("code"), verifier)
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

// loggedIn — сессия в cookie и ответ «кто вошёл и куда дальше».
func (s *Server) loggedIn(w http.ResponseWriter, res service.LoginResult) {
	s.setSession(w, res.Token, res.ExpiresAt)
	writeJSON(w, http.StatusOK, loginDTO{User: toUserDTO(res.User), Next: nextPath(res.User), Created: res.Created})
}

// AppPath — куда вести после входа: сразу в приложение (профиль заполняется, когда понадобится).
const AppPath = "/app"

func nextPath(domain.User) string { return AppPath }

// decode читает JSON тела (до 16 КБ, без лишних полей); ошибка — ответ 400.
func decode(w http.ResponseWriter, r *http.Request, v any) bool { return decodeN(w, r, v, maxBody) }

// decodeN — тело JSON не больше n байт, без неизвестных полей.
func decodeN(w http.ResponseWriter, r *http.Request, v any, n int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, n))
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
	case errors.Is(err, service.ErrBotLoginExpired):
		writeError(w, http.StatusGone, "login_expired", "Ссылка для входа устарела. Нажмите «Войти через Telegram» ещё раз.")
	case errors.Is(err, service.ErrBotLoginCancelled):
		writeError(w, http.StatusGone, "login_cancelled", "Вход отменён в Telegram.")
	case errors.Is(err, service.ErrSocialDisabled):
		writeError(w, http.StatusServiceUnavailable, "social_disabled", "Подключение этой площадки появится скоро.")
	case errors.Is(err, service.ErrSocialTooMany):
		writeError(w, http.StatusConflict, "social_too_many", "Соцсетей слишком много. Отвяжите ненужные.")
	case errors.Is(err, service.ErrSocialUnavailable):
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusServiceUnavailable, errorDTO{Error: errorBody{Code: "social_unavailable", Message: "Площадка не ответила. Попробуйте через минуту — проверим и сами.", RetryAfter: 60}})
	case isNotFound(err):
		writeError(w, http.StatusNotFound, "not_found", "Не найдено. Обновите страницу.")
	case errors.Is(err, service.ErrWrongStatus):
		writeError(w, http.StatusConflict, "wrong_status", "Статус уже изменился. Обновите страницу.")
	case errors.Is(err, service.ErrTooManyTasks):
		writeError(w, http.StatusConflict, "tasks_limit", "Заданий слишком много. Удалите ненужные черновики.")
	case errors.Is(err, service.ErrImageType):
		writeError(w, http.StatusUnsupportedMediaType, "image_type", "Подойдёт фото JPEG, PNG, WebP или HEIC.")
	case errors.Is(err, service.ErrImageTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "image_too_large", "Фото слишком большое: до 10 МБ и 40 Мп.")
	case errors.Is(err, service.ErrDraftTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "draft_too_large", "Черновик слишком большой — сократите текст.")
	case errors.Is(err, service.ErrMediaQuota):
		writeError(w, http.StatusConflict, "media_quota", "Место для фото закончилось. Удалите ненужные.")
	case errors.Is(err, service.ErrBusy):
		w.Header().Set("Retry-After", "30")
		writeJSON(w, http.StatusServiceUnavailable, errorDTO{Error: errorBody{Code: "busy", Message: "Много загрузок. Попробуйте через минуту.", RetryAfter: 30}})
	case errors.Is(err, context.Canceled):
		slog.InfoContext(r.Context(), "запрос отменён клиентом", "path", r.URL.Path)
		writeError(w, http.StatusServiceUnavailable, "cancelled", "Запрос отменён.")
	default:
		slog.ErrorContext(r.Context(), "ошибка запроса", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Что-то пошло не так. Попробуйте позже.")
	}
}

// isNotFound — нет такого (или чужое — неотличимо).
func isNotFound(err error) bool { return errors.Is(err, domain.ErrNotFound) }

func limitMessage(reason string, sec int) string {
	switch reason {
	case "email_cooldown":
		return fmt.Sprintf("Новый код можно запросить через %d с.", sec)
	case "submits_daily":
		return fmt.Sprintf("На сегодня отправок на проверку достаточно. Попробуйте через %s.", minutes(sec))
	case "social_hourly":
		return fmt.Sprintf("Подключений много. Попробуйте через %s.", minutes(sec))
	case "social_refresh":
		return fmt.Sprintf("Цифры только что обновлены. Проверить снова можно через %s.", minutes(sec))
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

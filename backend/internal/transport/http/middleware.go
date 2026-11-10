package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/logx"
	"ohvat/backend/internal/service"
)

const (
	sessionCookie = "ohvat_session"
	csrfCookie    = "ohvat_csrf"
	oauthCookie   = "ohvat_oauth"
	botCookie     = "ohvat_tglogin"
	botCookiePath = "/api/auth/telegram/bot"
	deviceCookie  = "ohvat_device"
	csrfHeader    = "X-CSRF-Token"
	maxBody       = 16 << 10
)

// requestID — id запроса (из X-Request-Id или новый) в контекст журнала и в ответ.
func requestID(next http.Handler) http.Handler {
	return middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := middleware.GetReqID(r.Context())
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(logx.WithRequestID(r.Context(), id)))
	}))
}

// logRequests — строка журнала на запрос: метод, путь (без строки запроса — в ней коды Google), статус, время.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if r.URL.Path == "/healthz" {
			return
		}
		slog.InfoContext(r.Context(), "http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
			"ms", time.Since(start).Milliseconds())
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.ErrorContext(r.Context(), "паника в обработчике", "panic", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal", "Что-то пошло не так. Попробуйте позже.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// noStore — ответы API с личными данными не кэшируются.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// clientIP — адрес клиента: X-Real-IP ставит только наш прокси (nginx/Caddy) и перезаписывает присланный;
// напрямую бэкенд снаружи недоступен. Иначе — адрес соединения.
func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" && net.ParseIP(ip) != nil {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// client — сведения о клиенте в контекст сервиса (адрес, браузер, устройство) и cookie устройства
// (случайный id на 400 дней, httpOnly): по нему вход с нового устройства даёт уведомление в Telegram.
func (s *Server) client(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dev := ""
		if c, err := r.Cookie(deviceCookie); err == nil && len(c.Value) == 43 {
			dev = c.Value
		} else if strings.HasPrefix(r.URL.Path, "/api/auth/") {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err == nil {
				dev = base64.RawURLEncoding.EncodeToString(b)
				http.SetCookie(w, &http.Cookie{Name: deviceCookie, Value: dev, Path: "/api/auth", MaxAge: 400 * 24 * 3600,
					HttpOnly: true, Secure: s.opt.SecureCookies, SameSite: http.SameSiteLaxMode})
			}
		}
		ua := r.UserAgent()
		if len(ua) > 512 {
			ua = ua[:512]
		}
		ctx := service.WithClient(r.Context(), service.ClientInfo{IP: clientIP(r), UserAgent: ua, DeviceID: dev})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// csrf — защита изменяющих запросов: заголовок X-CSRF-Token должен совпасть с cookie (сравнение за постоянное
// время), а Origin (если браузер его прислал) — с адресом сайта из PUBLIC_URL.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != s.opt.Origin {
			writeError(w, http.StatusForbidden, "csrf", "Запрос отклонён. Обновите страницу и попробуйте снова.")
			return
		}
		c, err := r.Cookie(csrfCookie)
		h := r.Header.Get(csrfHeader)
		if err != nil || c.Value == "" || h == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(h)) != 1 {
			writeError(w, http.StatusForbidden, "csrf", "Сессия страницы устарела. Обновите страницу и попробуйте снова.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// csrfToken — токен из cookie или новый (cookie ставится сразу).
func (s *Server) csrfToken(w http.ResponseWriter, r *http.Request) (string, error) {
	if c, err := r.Cookie(csrfCookie); err == nil && len(c.Value) == 43 {
		return c.Value, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: tok, Path: "/", HttpOnly: true, Secure: s.opt.SecureCookies, SameSite: http.SameSiteLaxMode})
	return tok, nil
}

type userKey struct{}

// requireUser — только для вошедших: пользователь сессии в контексте, иначе 401.
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Войдите, чтобы продолжить.")
			return
		}
		u, err := s.auth.Session(r.Context(), c.Value)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	})
}

func currentUser(r *http.Request) domain.User {
	u, _ := r.Context().Value(userKey{}).(domain.User)
	return u
}

func (s *Server) setSession(w http.ResponseWriter, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: exp, MaxAge: int(time.Until(exp).Seconds()),
		HttpOnly: true, Secure: s.opt.SecureCookies, SameSite: http.SameSiteLaxMode})
}

func (s *Server) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: path, MaxAge: -1, HttpOnly: true, Secure: s.opt.SecureCookies, SameSite: http.SameSiteLaxMode})
}

// Package httpapi — транспорт: маршруты, обработчики по api/openapi.yaml, middleware.
// Обработчик только разбирает запрос, зовёт сервис через интерфейс и пишет ответ; логики и SQL здесь нет.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// HomeService — что транспорту нужно от сервиса главной.
type HomeService interface {
	Get(ctx context.Context) (service.HomeView, error)
}

// HealthService — проверка живости.
type HealthService interface {
	Check(ctx context.Context) error
}

// AuthService — что транспорту нужно от сервиса входа.
type AuthService interface {
	Methods() service.Methods
	StartEmail(ctx context.Context, email, role string, consent bool, ip string) (service.CodeSent, error)
	VerifyEmail(ctx context.Context, email, code string) (service.LoginResult, error)
	LoginTelegram(ctx context.Context, data map[string]string, role string, consent bool) (service.LoginResult, error)
	StartGoogle(consent bool) (service.GoogleStart, error)
	LoginGoogle(ctx context.Context, code, verifier, role string) (service.LoginResult, error)
	Session(ctx context.Context, token string) (domain.User, error)
	Logout(ctx context.Context, token string) error
	Account(ctx context.Context, u domain.User) (service.Account, error)
	SaveProfile(ctx context.Context, u domain.User, p domain.Profile) (domain.User, error)
}

// Options — настройки транспорта (из config).
type Options struct {
	Origin        string // схема+хост сайта из PUBLIC_URL — для проверки Origin
	SecureCookies bool   // PUBLIC_URL на https
	Limiter       *RateLimiter
}

// Server — HTTP API.
type Server struct {
	home   HomeService
	health HealthService
	auth   AuthService
	opt    Options
}

// New создаёт API.
func New(home HomeService, health HealthService, auth AuthService, opt Options) *Server {
	if opt.Limiter == nil {
		opt.Limiter = NewRateLimiter(1, 10)
	}
	return &Server{home: home, health: health, auth: auth, opt: opt}
}

// Routes — все маршруты API.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(requestID, logRequests, recoverer)
	r.Get("/healthz", s.getHealth)
	r.Route("/api", func(r chi.Router) {
		r.Use(s.csrf)
		r.Get("/home", s.getHome)
		r.Route("/auth", func(r chi.Router) {
			r.Use(noStore)
			r.Get("/config", s.getAuthConfig)
			r.Post("/logout", s.postLogout)
			r.Group(func(r chi.Router) {
				r.Use(s.opt.Limiter.Middleware)
				r.Post("/email/start", s.postEmailStart)
				r.Post("/email/verify", s.postEmailVerify)
				r.Post("/telegram", s.postTelegram)
				r.Get("/google/start", s.getGoogleStart)
				r.Get("/google/callback", s.getGoogleCallback)
			})
		})
		r.Route("/me", func(r chi.Router) {
			r.Use(noStore, s.requireUser)
			r.Get("/", s.getMe)
			r.Put("/profile", s.putProfile)
		})
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "Такого адреса нет.")
		})
		r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Этот метод не поддерживается.")
		})
	})
	return r
}

func (s *Server) getHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.health.Check(ctx); err != nil {
		slog.ErrorContext(r.Context(), "healthz: база недоступна", "err", err)
		writeError(w, http.StatusServiceUnavailable, "db_unavailable", "База данных недоступна.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) getHome(w http.ResponseWriter, r *http.Request) {
	v, err := s.home.Get(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "главная: данные", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Что-то пошло не так. Попробуйте позже.")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, http.StatusOK, toHomeDTO(v))
}

// writeJSON отправляет ответ JSON.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("ответ JSON", "err", err)
	}
}

// writeError — единый формат ошибки (components/schemas/Error).
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorDTO{Error: errorBody{Code: code, Message: msg}})
}

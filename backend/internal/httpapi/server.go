// Package httpapi — транспорт: маршруты, обработчики по api/openapi.yaml, middleware. Логика — в service.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"ohvat/backend/internal/service"
)

// HomeService — что транспорту нужно от сервиса главной.
type HomeService interface {
	Get(ctx context.Context) (service.HomeView, error)
}

// Pinger проверяет базу для /healthz.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Server — HTTP API.
type Server struct {
	home HomeService
	db   Pinger
}

// New создаёт API.
func New(home HomeService, db Pinger) *Server { return &Server{home: home, db: db} }

// Routes — все маршруты API.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, logRequests, recoverer)
	r.Get("/healthz", s.health)
	r.Route("/api", func(r chi.Router) {
		r.Get("/home", s.getHome)
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "Такого адреса нет.")
		})
		r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Этот метод не поддерживается.")
		})
	})
	return r
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		slog.Error("healthz: база недоступна", "err", err)
		writeError(w, http.StatusServiceUnavailable, "db_unavailable", "База данных недоступна.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) getHome(w http.ResponseWriter, r *http.Request) {
	v, err := s.home.Get(r.Context())
	if err != nil {
		slog.Error("главная: данные", "err", err, "request_id", middleware.GetReqID(r.Context()))
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

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if r.URL.Path == "/healthz" {
			return
		}
		slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
			"ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context()))
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("паника в обработчике", "panic", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal", "Что-то пошло не так. Попробуйте позже.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

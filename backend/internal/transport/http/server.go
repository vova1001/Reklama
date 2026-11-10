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
	StartEmail(ctx context.Context, email string, consent bool, ip string) (service.CodeSent, error)
	VerifyEmail(ctx context.Context, email, code string) (service.LoginResult, error)
	LoginTelegram(ctx context.Context, data map[string]string, consent bool) (service.LoginResult, error)
	StartGoogle(consent bool) (service.GoogleStart, error)
	LoginGoogle(ctx context.Context, code, verifier string) (service.LoginResult, error)
	Session(ctx context.Context, token string) (domain.User, error)
	Logout(ctx context.Context, token string) error
	LogoutEverywhere(ctx context.Context, userID int64) (int64, error)
	StartTelegramBot(ctx context.Context, consent bool, ip string) (service.TelegramBotStart, error)
	WaitTelegramBot(ctx context.Context, browserSecret string, wait time.Duration) (service.BotLoginStatus, service.LoginResult, error)
}

// ProfileService — режим аккаунта и профили режимов.
type ProfileService interface {
	Overview(ctx context.Context, u domain.User) (service.Overview, error)
	SwitchMode(ctx context.Context, u domain.User, mode string) (service.Overview, error)
	SaveBasics(ctx context.Context, u domain.User, b domain.Basics) (domain.User, error)
	Business(ctx context.Context, u domain.User) (*domain.BusinessProfile, error)
	SaveBusiness(ctx context.Context, u domain.User, p domain.BusinessProfile) (domain.BusinessProfile, error)
	Creator(ctx context.Context, u domain.User) (*domain.CreatorProfile, error)
	SaveCreator(ctx context.Context, u domain.User, p domain.CreatorProfile) (domain.CreatorProfile, error)
}

// TaskService — задания бизнеса и лента.
type TaskService interface {
	List(ctx context.Context, u domain.User) ([]domain.Task, error)
	Get(ctx context.Context, u domain.User, id int64) (domain.Task, error)
	Create(ctx context.Context, u domain.User, in service.TaskInput) (domain.Task, error)
	Update(ctx context.Context, u domain.User, id int64, in service.TaskInput) (domain.Task, error)
	Delete(ctx context.Context, u domain.User, id int64) error
	Submit(ctx context.Context, u domain.User, id int64) (domain.Task, error)
	Withdraw(ctx context.Context, u domain.User, id int64) (domain.Task, error)
	Feed(ctx context.Context, after *domain.FeedCursor, limit int) (service.FeedPage, error)
}

// CardService — анкета автора.
type CardService interface {
	Get(ctx context.Context, u domain.User) (*domain.CreatorCard, error)
	Save(ctx context.Context, u domain.User, in service.CardInput) (domain.CreatorCard, error)
	Submit(ctx context.Context, u domain.User) (domain.CreatorCard, error)
	Withdraw(ctx context.Context, u domain.User) (domain.CreatorCard, error)
}

// CategoryService — справочник и свои категории.
type CategoryService interface {
	List(ctx context.Context, u domain.User) (service.CategoryList, error)
	Similar(ctx context.Context, u domain.User, q string) ([]domain.CategoryInfo, error)
	Create(ctx context.Context, u domain.User, title string) (domain.CategoryInfo, bool, error)
}

// MediaService — фото: загрузка, удаление, аватар, просмотр с правами.
type MediaService interface {
	MaxBytes() int64
	Upload(ctx context.Context, u domain.User, in service.UploadInput) (domain.Media, error)
	Delete(ctx context.Context, u domain.User, id string) error
	SetAvatar(ctx context.Context, u domain.User, id string) (string, error)
	Open(ctx context.Context, viewerID int64, id string, size int) (service.Blob, bool, error)
}

// Services — сервисы, которые обслуживает транспорт.
type Services struct {
	Home       HomeService
	Health     HealthService
	Auth       AuthService
	Profiles   ProfileService
	Tasks      TaskService
	Cards      CardService
	Categories CategoryService
	Media      MediaService
}

// Options — настройки транспорта (из config).
type Options struct {
	Origin        string // схема+хост сайта из PUBLIC_URL — для проверки Origin
	SecureCookies bool   // PUBLIC_URL на https
	// Лимиты (токен-бакеты); nil — без лимита. APILimiter — весь /api по адресу; AuthLimiter — вход по адресу
	// (строже); UserLimiter — /api/me* по пользователю.
	APILimiter  Limiter
	AuthLimiter Limiter
	UserLimiter Limiter
	// UploadLimiter — загрузки фото по пользователю.
	UploadLimiter Limiter
	BotWait       time.Duration // сколько ждать подтверждения в боте в одном запросе (long-poll), 25 с
	// TelegramWebhook — приём обновлений бота (transport/telegram); nil — бот выключен.
	// Свой секрет вместо CSRF и свой лимит, поэтому вне группы /api.
	TelegramWebhook http.Handler
}

// Server — HTTP API.
type Server struct {
	home       HomeService
	health     HealthService
	auth       AuthService
	profiles   ProfileService
	tasks      TaskService
	cards      CardService
	categories CategoryService
	media      MediaService
	opt        Options
}

// New создаёт API.
func New(svc Services, opt Options) *Server {
	if opt.BotWait <= 0 {
		opt.BotWait = 25 * time.Second
	}
	return &Server{
		home: svc.Home, health: svc.Health, auth: svc.Auth, profiles: svc.Profiles, tasks: svc.Tasks, cards: svc.Cards,
		categories: svc.Categories, media: svc.Media, opt: opt,
	}
}

// Routes — все маршруты API.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(requestID, logRequests, recoverer)
	r.Get("/healthz", s.getHealth)
	r.With(limit(s.opt.APILimiter, byIP("media:"))).Get("/media/{id}/{file}", s.getMedia)
	if s.opt.TelegramWebhook != nil {
		r.Post("/api/telegram/webhook", s.opt.TelegramWebhook.ServeHTTP)
	}
	r.Route("/api", func(r chi.Router) {
		r.Use(limit(s.opt.APILimiter, byIP("ip:")), s.client, s.csrf)
		r.Get("/home", s.getHome)
		r.Get("/tasks", s.getFeed)
		r.Route("/auth", func(r chi.Router) {
			r.Use(noStore)
			r.Get("/config", s.getAuthConfig)
			r.Post("/logout", s.postLogout)
			r.Group(func(r chi.Router) {
				r.Use(limit(s.opt.AuthLimiter, byIP("auth:")))
				r.Post("/email/start", s.postEmailStart)
				r.Post("/email/verify", s.postEmailVerify)
				r.Post("/telegram", s.postTelegram)
				r.Post("/telegram/bot/start", s.postBotStart)
				r.Post("/telegram/bot/status", s.postBotStatus)
				r.Get("/google/start", s.getGoogleStart)
				r.Get("/google/callback", s.getGoogleCallback)
			})
		})
		r.Route("/me", func(r chi.Router) {
			r.Use(noStore, s.requireUser, limit(s.opt.UserLimiter, byUser("user:")))
			r.Get("/", s.getMe)
			r.Patch("/mode", s.patchMode)
			r.Put("/profile", s.putBasics)
			r.Get("/profile/business", s.getBusiness)
			r.Put("/profile/business", s.putBusiness)
			r.Get("/profile/creator", s.getCreator)
			r.Put("/profile/creator", s.putCreator)
			r.Post("/logout-all", s.postLogoutAll)
			r.Put("/avatar", s.putAvatar)

			r.Get("/tasks", s.listMyTasks)
			r.Post("/tasks", s.createTask)
			r.Get("/tasks/{id}", s.getMyTask)
			r.Put("/tasks/{id}", s.updateTask)
			r.Delete("/tasks/{id}", s.deleteTask)
			r.Post("/tasks/{id}/submit", s.submitTask)
			r.Post("/tasks/{id}/withdraw", s.withdrawTask)

			r.Get("/card", s.getCard)
			r.Put("/card", s.putCard)
			r.Post("/card/submit", s.submitCard)
			r.Post("/card/withdraw", s.withdrawCard)

			r.Get("/categories", s.getCategories)
			r.Get("/categories/similar", s.getSimilarCategories)
			r.Post("/categories", s.createCategory)

			r.With(limit(s.opt.UploadLimiter, byUser("upload:"))).Post("/media", s.uploadMedia)
			r.Delete("/media/{id}", s.deleteMedia)
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

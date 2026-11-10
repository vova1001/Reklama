// Сервер API «Охват»: настройки из окружения, подключение к БД, миграции, сборка слоёв, HTTP и фоновые задачи.
// Слои: transport/http → service → repository (и адаптеры внешних сервисов в gateway/), связь — через интерфейсы.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"ohvat/backend/internal/config"
	"ohvat/backend/internal/gateway/google"
	"ohvat/backend/internal/gateway/mail"
	"ohvat/backend/internal/logx"
	"ohvat/backend/internal/repository"
	"ohvat/backend/internal/service"
	httpapi "ohvat/backend/internal/transport/http"
	"ohvat/backend/migrations"
)

func main() {
	log := slog.New(logx.NewHandler(slog.NewJSONHandler(os.Stdout, nil)))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		slog.Error("сервер остановлен", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	for _, w := range cfg.Warnings {
		log.Warn("настройки: " + w)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := migrations.Up(pool); err != nil {
		return err
	}

	// внешние сервисы: включаются ключами из окружения
	var mailer service.Mailer
	var queue *mail.Queue
	provider, reason := cfg.MailReady()
	if provider == "" {
		log.Warn("вход по почте выключен: " + reason)
	} else {
		queue = mail.NewQueue(newSender(cfg, provider, log), mail.Options{Workers: cfg.MailWorkers}, log)
		mailer = queue
		log.Info("вход по почте включён", "mail", provider)
	}
	var gp service.GoogleProvider
	if cfg.GoogleEnabled() {
		gp = google.New(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURL())
	}

	auth := service.NewAuth(service.AuthDeps{
		Users: repository.NewAuth(pool), Codes: repository.NewAuth(pool), Sessions: repository.NewAuth(pool),
		Mailer: mailer, Google: gp, Log: log,
	}, service.AuthConfig{
		Secret: []byte(cfg.AuthSecret), CodeTTL: cfg.CodeTTL, MaxAttempts: cfg.CodeAttempts, ResendAfter: cfg.CodeResend,
		PerEmailHour: cfg.CodePerEmail, PerIPHour: cfg.CodePerIP, SessionTTL: cfg.SessionTTL,
		TelegramToken: cfg.TelegramBotToken, TelegramBotName: cfg.TelegramBotName, EmailEnabled: mailer != nil,
	})
	limiter := httpapi.NewRateLimiter(cfg.RateRPS, cfg.RateBurst)
	api := httpapi.New(
		service.NewHome(repository.NewHome(pool)),
		service.NewHealth(repository.NewHealth(pool)),
		auth,
		httpapi.Options{Origin: cfg.Origin(), SecureCookies: cfg.SecureCookies(), Limiter: limiter},
	)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	// HTTP, очередь писем, очистка кодов и сессий, очистка лимитов — под одним контекстом; остановка — по сигналу
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		log.Info("сервер запущен", "addr", cfg.HTTPAddr, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(sh)
	})
	if queue != nil {
		g.Go(func() error { queue.Run(gctx); return nil })
	}
	g.Go(func() error { auth.RunCleanup(gctx, 10*time.Minute); return nil })
	g.Go(func() error { limiter.Run(gctx, time.Minute); return nil })
	err = g.Wait()
	log.Info("сервер остановлен: запросы и письма завершены")
	return err
}

// newSender — способ отправки писем по MAIL_PROVIDER.
func newSender(cfg config.Config, provider string, log *slog.Logger) mail.Sender {
	switch provider {
	case config.MailResend:
		return mail.Resend{APIKey: cfg.ResendAPIKey, From: cfg.MailFrom}
	case config.MailSMTP:
		return mail.SMTP{Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser, Password: cfg.SMTPPassword, From: cfg.SMTPFrom}
	default:
		return mail.Log{Logger: log}
	}
}

// connect ждёт базу до 60 секунд — при первом запуске PostgreSQL поднимается дольше сервера.
func connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	pc.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = pool.Ping(pctx)
		cancel()
		if err == nil {
			return pool, nil
		}
		if time.Now().After(deadline) {
			pool.Close()
			return nil, err
		}
		slog.Info("жду базу данных…")
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

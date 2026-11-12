// Сервер API «Охват»: настройки из окружения, подключение к БД, миграции, сборка слоёв, HTTP и фоновые задачи.
// Слои: transport/http → service → repository (и адаптеры внешних сервисов в gateway/), связь — через интерфейсы.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	"ohvat/backend/internal/config"
	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/gateway/google"
	"ohvat/backend/internal/gateway/imaging"
	"ohvat/backend/internal/gateway/keepalive"
	"ohvat/backend/internal/gateway/mail"
	"ohvat/backend/internal/gateway/platforms/httpx"
	tgverify "ohvat/backend/internal/gateway/platforms/telegram"
	"ohvat/backend/internal/gateway/platforms/youtube"
	"ohvat/backend/internal/gateway/qrcode"
	"ohvat/backend/internal/gateway/storage"
	tggw "ohvat/backend/internal/gateway/telegram"
	"ohvat/backend/internal/logx"
	"ohvat/backend/internal/ratelimit"
	"ohvat/backend/internal/repository"
	"ohvat/backend/internal/secretbox"
	"ohvat/backend/internal/service"
	httpapi "ohvat/backend/internal/transport/http"
	tgtransport "ohvat/backend/internal/transport/telegram"
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

	authRepo := repository.NewAuth(pool)
	tgRepo := repository.NewTelegram(pool)

	// бот Telegram: клиент Bot API → очередь отправки (лимиты Telegram) → уведомления и сервис бота
	var (
		tgClient *tggw.Client
		outbox   *tggw.Outbox
		notifier service.Notifier
		botRepo  service.TelegramLoginRepository
	)
	var bot *service.Bot
	if cfg.BotEnabled() {
		tgClient = tggw.New(cfg.TelegramBotToken, tggw.Options{BaseURL: cfg.TelegramAPIURL})
		outbox = tggw.NewOutbox(tgClient, tggw.OutboxOptions{RPS: cfg.TelegramSendRPS, OnBlocked: func(chatID int64) { bot.ChatBlocked(chatID) }}, log)
		notifier = service.NewBotNotifier(tgRepo, outbox, cfg.PublicURL, log)
		botRepo = tgRepo
		log.Info("бот Telegram включён", "bot", cfg.TelegramBotName, "mode", cfg.TelegramMode)
	} else if cfg.TelegramBotToken != "" {
		log.Warn("бот Telegram не принимает сообщения — вход через бота выключен (виджет работает)")
	}

	auth := service.NewAuth(service.AuthDeps{
		Users: authRepo, Codes: authRepo, Sessions: authRepo, Staff: authRepo,
		Mailer: mailer, Google: gp, BotLogins: botRepo, Devices: tgRepo, Notifier: notifier, Log: log,
	}, service.AuthConfig{
		AdminEmails: cfg.AdminEmails,
		Secret:      []byte(cfg.AuthSecret), CodeTTL: cfg.CodeTTL, MaxAttempts: cfg.CodeAttempts, ResendAfter: cfg.CodeResend,
		PerEmailHour: cfg.CodePerEmail, PerIPHour: cfg.CodePerIP, SessionTTL: cfg.SessionTTL,
		TelegramToken: cfg.TelegramBotToken, TelegramBotName: cfg.TelegramBotName, EmailEnabled: mailer != nil,
		TelegramBot: cfg.BotEnabled(), BotLoginTTL: cfg.TelegramLoginTTL, BotLoginPerIP: cfg.TelegramLoginPerIP,
	})
	profiles := service.NewProfiles(repository.NewProfiles(pool), authRepo)

	// задания, анкеты, свои категории, фото: обработка — пул воркеров, файлы — PostgreSQL или S3 (MEDIA_STORE)
	listRepo := repository.NewListings(pool)
	catRepo := repository.NewCategories(pool)
	mediaRepo := repository.NewMedia(pool)
	imgPool := imaging.NewPool(imaging.Processor{}, imaging.PoolOptions{Workers: cfg.MediaWorkers}, log)
	defer imgPool.Close()
	go imaging.Warm()
	media := service.NewMedia(mediaRepo, newBlobStore(cfg, pool, log), imgPool, service.MediaConfig{
		MaxBytes: int64(cfg.MediaMaxMB) << 20, QuotaBytes: int64(cfg.MediaQuotaMB) << 20, MaxFiles: cfg.MediaMaxFiles,
	}, log)
	social, err := newSocial(cfg, pool, authRepo, tgClient, log)
	if err != nil {
		return err
	}
	// админка: режим проверки — в базе (MODERATION_MODE — по умолчанию), решения по своим форматам
	adminRepo := repository.NewAdmin(pool)
	settings := service.NewSettings(adminRepo, domain.ModerationMode(cfg.Moderation), nil, log)
	deps := service.ListingDeps{
		Categories: catRepo, Submits: listRepo, Media: mediaRepo, Platforms: social,
		Limits: service.ListingLimits{MaxTasks: cfg.TasksMax, SubmitsPerDay: cfg.SubmitsPerDay},
		Mode:   domain.ModerationMode(cfg.Moderation), Modes: settings, FormatRules: adminRepo,
	}
	log.Info("проверка заданий и анкет", "default_mode", cfg.Moderation, "admins_from_env", len(cfg.AdminEmails))
	tasks := service.NewTasks(listRepo, deps)
	cards := service.NewCards(listRepo, deps)
	content := moderationRepo{listRepo, catRepo}
	moderation := service.NewModeration(content)
	adm, err := newAdmin(cfg, pool, adminRepo, authRepo, content, moderation, social, queue, notifier, log)
	if err != nil {
		return err
	}

	// лимиты: Redis (REDIS_URL) — общие для всех серверов, при сбое Redis — в памяти; без Redis — в памяти
	lims := newLimiters(ctx, cfg, log)
	defer lims.close()

	// черновики форм редактора: Redis (REDIS_URL), при сбое и без него — PostgreSQL
	draftsPG := repository.NewDraftsPG(pool)
	var draftStore service.DraftStore = draftsPG
	if lims.rdb != nil {
		draftStore = service.FallbackDrafts{Primary: repository.NewDraftsRedis(lims.rdb), Secondary: draftsPG, Log: log}
		log.Info("черновики форм: Redis, запасное — PostgreSQL")
	} else {
		log.Info("черновики форм: PostgreSQL")
	}
	drafts := service.NewDrafts(draftStore, service.DraftConfig{MaxBytes: cfg.DraftMaxKB << 10}, nil)

	var hook *tgtransport.Handler
	opts := httpapi.Options{
		Origin: cfg.Origin(), SecureCookies: cfg.SecureCookies(),
		APILimiter: lims.api, AuthLimiter: lims.auth, UserLimiter: lims.user, UploadLimiter: lims.upload, DraftLimiter: lims.draft,
		AdminLimiter: lims.admin, MFALimiter: lims.mfa, ComplaintLimiter: lims.complaint,
	}
	if cfg.BotEnabled() {
		bot = service.NewBot(service.BotDeps{Auth: auth, Users: authRepo, Chats: tgRepo, Messenger: outbox, Social: social, Log: log},
			service.BotConfig{SiteURL: cfg.PublicURL, BotName: cfg.TelegramBotName})
		hook = tgtransport.New(bot, tgtransport.Options{Secret: cfg.TelegramSecret, Limiter: lims.webhook, Log: log})
		opts.TelegramWebhook = hook
	}
	api := httpapi.New(httpapi.Services{
		Home: service.NewHome(repository.NewHome(pool)), Health: service.NewHealth(repository.NewHealth(pool)),
		Auth: auth, Profiles: profiles, Tasks: tasks, Cards: cards, Categories: service.NewCategories(catRepo), Formats: service.NewFormats(listRepo), Media: media, Drafts: drafts, Social: social,
		TwoFactor: adm.twoFactor, Review: adm.review, Complaints: adm.complaints, StaffUsers: adm.users, Audit: service.NewAuditLog(adminRepo),
		Settings: settings, Dashboard: service.NewDashboard(adminRepo, nil),
	}, opts)
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
	g.Go(func() error { media.RunCleanup(gctx, time.Hour); return nil })
	g.Go(func() error { tasks.RunExpiry(gctx, 5*time.Minute, log); return nil })
	g.Go(func() error { drafts.RunCleanup(gctx, draftsPG, time.Hour, log); return nil })
	g.Go(func() error { social.RunRefresher(gctx, 10*time.Minute); return nil })
	g.Go(func() error { adm.review.Run(gctx); return nil })
	if cfg.KeepAliveURL != "" { // самопинг через публичный адрес: бесплатный хостинг не усыпляет сервис
		ka := service.KeepAlive{Prober: keepalive.New(cfg.KeepAliveURL), Every: 10 * time.Minute, Timeout: 30 * time.Second, Log: log}
		g.Go(func() error { ka.Run(gctx); return nil })
		log.Info("самопинг включён", "url", cfg.KeepAliveURL+"/healthz", "every", "10m")
	}
	for _, m := range lims.memory {
		g.Go(func() error { m.Run(gctx, time.Minute); return nil })
	}
	if cfg.BotEnabled() {
		g.Go(func() error { outbox.Run(gctx); return nil })
		g.Go(func() error { hook.RunSweep(gctx, time.Minute); return nil })
		g.Go(func() error {
			return tgClient.SetupWithRetry(gctx, tggw.SetupOptions{BotName: cfg.TelegramBotName, WebhookURL: cfg.TelegramWebhookURL(),
				WebhookSecret: cfg.TelegramSecret, Polling: cfg.TelegramMode == config.TelegramPolling}, log)
		})
		if cfg.TelegramMode == config.TelegramPolling {
			g.Go(func() error { tgtransport.NewPoller(tgClient, hook, log).Run(gctx); return nil })
		}
	}
	err = g.Wait()
	log.Info("сервер остановлен: запросы, письма и сообщения бота завершены")
	return err
}

// limiters — ограничители частоты по правилам; memory — бакеты в памяти (их чистит фоновая задача).
type limiters struct {
	api, auth, user, webhook, upload, draft ratelimit.Limiter
	admin, mfa, complaint                   ratelimit.Limiter
	memory                                  []*ratelimit.Memory
	rdb                                     *redis.Client
}

func (l *limiters) close() {
	if l.rdb != nil {
		_ = l.rdb.Close()
	}
}

// newLimiters — правила лимитов из настроек. REDIS_URL задан, но Redis недоступен при старте — сервер
// запускается с лимитами в памяти (и пишет предупреждение), а не падает.
func newLimiters(ctx context.Context, cfg config.Config, log *slog.Logger) *limiters {
	out := &limiters{}
	if cfg.RedisURL != "" {
		rdb, err := ratelimit.Connect(ctx, cfg.RedisURL)
		if err != nil {
			log.Warn("лимиты: Redis недоступен — считаю в памяти процесса", "err", err)
		} else {
			out.rdb = rdb
			log.Info("лимиты: Redis")
		}
	}
	mk := func(prefix string, r ratelimit.Rate) ratelimit.Limiter {
		mem := ratelimit.NewMemory(r)
		out.memory = append(out.memory, mem)
		if out.rdb == nil {
			return mem
		}
		return &ratelimit.Fallback{Primary: ratelimit.NewRedis(out.rdb, "ohvat:rl:"+prefix, r), Secondary: mem, Log: log}
	}
	out.api = mk("api:", ratelimit.Rate{RPS: cfg.APIRPS, Burst: cfg.APIBurst})
	out.auth = mk("auth:", ratelimit.Rate{RPS: cfg.RateRPS, Burst: cfg.RateBurst})
	out.user = mk("user:", ratelimit.Rate{RPS: cfg.UserRPS, Burst: cfg.UserBurst})
	out.webhook = mk("tg:", ratelimit.Rate{RPS: cfg.WebhookRPS, Burst: cfg.WebhookBurst})
	out.upload = mk("up:", ratelimit.Rate{RPS: cfg.UploadRPS, Burst: cfg.UploadBurst})
	out.draft = mk("draft:", ratelimit.Rate{RPS: cfg.DraftRPS, Burst: cfg.DraftBurst})
	out.admin = mk("adm:", ratelimit.Rate{RPS: cfg.AdminRPS, Burst: cfg.AdminBurst})
	out.mfa = mk("mfa:", ratelimit.Rate{RPS: cfg.MFARPS, Burst: cfg.MFABurst})
	out.complaint = mk("cmp:", ratelimit.Rate{RPS: cfg.ComplaintRPS, Burst: cfg.ComplaintBurst})
	return out
}

// newSocial — подтверждение соцсетей: Telegram (бот-админ канала или группы) — если бот включён; YouTube —
// ключи Google + TOKEN_ENC_KEY. VK, TikTok, Instagram, Threads — интерфейс готов, адаптеры позже («Скоро»).
func newSocial(cfg config.Config, pool *pgxpool.Pool, users *repository.Auth, tg *tggw.Client, log *slog.Logger) (*service.Social, error) {
	var vs []service.PlatformVerifier
	var cipher service.TokenCipher
	if cfg.TokenEncKey != "" {
		box, err := secretbox.New(cfg.TokenEncKey, cfg.TokenEncKeyOld)
		if err != nil {
			return nil, err
		}
		cipher = box
	}
	if cfg.BotEnabled() && tg != nil {
		vs = append(vs, tgverify.New(tg, cfg.TelegramBotName))
	}
	if cfg.YouTubeEnabled() {
		vs = append(vs, youtube.New(youtube.Config{ClientID: cfg.GoogleClientID, ClientSecret: cfg.GoogleClientSecret,
			HTTP: httpx.New(httpx.Options{Hosts: youtube.Hosts, Site: cfg.PublicURL})}))
	}
	on := make([]string, 0, len(vs))
	for _, v := range vs {
		on = append(on, string(v.Platform()))
	}
	log.Info("соцсети: подтверждение", "platforms", on, "refresh", cfg.SocialRefresh.String())
	return service.NewSocial(service.SocialDeps{Repo: repository.NewSocial(pool), Users: users, Cipher: cipher, Verifiers: vs, Log: log},
		service.SocialConfig{SiteURL: cfg.PublicURL, RefreshEvery: cfg.SocialRefresh, StartsPerHour: cfg.SocialPerHour}), nil
}

// moderationRepo — хранилище для модерации и карточки проверки: задания и анкеты + категории.
type moderationRepo struct {
	*repository.Listings
	*repository.Categories
}

// admin — сервисы админки.
type admin struct {
	twoFactor  *service.TwoFactor
	review     *service.Review
	complaints *service.Complaints
	users      *service.StaffUsers
}

// newAdmin — админка: 2FA (секреты — AES-256-GCM: TOKEN_ENC_KEY или ключ из AUTH_SECRET), очередь проверки с
// решениями и уведомлениями (почта + Telegram), жалобы, пользователи.
func newAdmin(cfg config.Config, pool *pgxpool.Pool, repo *repository.Admin, users *repository.Auth, content moderationRepo,
	mod *service.Moderation, social *service.Social, queue *mail.Queue, bot service.Notifier, log *slog.Logger) (admin, error) {
	key, old := cfg.TwoFactorKeys(service.TOTPKeyFromSecret)
	if key == "" {
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return admin{}, err
		}
		key = base64.StdEncoding.EncodeToString(k)
		log.Warn("2FA: нет TOKEN_ENC_KEY и AUTH_SECRET — ключ на время работы процесса, после перезапуска 2FA настраивается заново")
	}
	box, err := secretbox.New(key, old...)
	if err != nil {
		return admin{}, err
	}
	twoFA := service.NewTwoFactor(service.TwoFactorDeps{Repo: repo, Sessions: users, Cipher: box, QR: qrcode.PNG{}, Log: log},
		service.TwoFactorConfig{Secret: []byte(cfg.AuthSecret)})
	notices := &service.OwnerNotices{Users: users, Bot: bot, SiteURL: cfg.PublicURL, Log: log}
	if queue != nil { // без способа отправки писем — только Telegram
		notices.Mail = queue
	}
	review := service.NewReview(service.ReviewDeps{Repo: repo, Content: content, Moderator: mod, Platforms: social, Notifier: notices, Log: log},
		service.ReviewConfig{})
	staff := service.NewStaffUsers(repo, userContent{users: users, profiles: repository.NewProfiles(pool), listings: content.Listings,
		social: repository.NewSocial(pool)}, nil, func(err error) bool { return errors.Is(err, repository.ErrLastAdmin) })
	complaints := service.NewComplaints(repo, complaintTargets{content.Listings, repo}, mod, staff, nil, service.ComplaintsConfig{})
	return admin{twoFactor: twoFA, review: review, complaints: complaints, users: staff}, nil
}

// complaintTargets — на что жалуются: опубликованное (задания и анкеты) и пользователи.
type complaintTargets struct {
	*repository.Listings
	*repository.Admin
}

// userContent — всё о пользователе для карточки в админке.
type userContent struct {
	users    *repository.Auth
	profiles *repository.Profiles
	listings *repository.Listings
	social   *repository.Social
}

func (c userContent) Providers(ctx context.Context, id int64) ([]string, error) {
	return c.users.Providers(ctx, id)
}
func (c userContent) OwnTasks(ctx context.Context, id int64, n int) ([]domain.Task, error) {
	return c.listings.OwnTasks(ctx, id, n)
}
func (c userContent) Card(ctx context.Context, id int64) (domain.CreatorCard, error) {
	return c.listings.Card(ctx, id)
}
func (c userContent) Accounts(ctx context.Context, id int64) ([]domain.SocialAccount, error) {
	return c.social.Accounts(ctx, id)
}
func (c userContent) Profiles(ctx context.Context, id int64) (domain.Profiles, error) {
	var out domain.Profiles
	if b, err := c.profiles.BusinessProfile(ctx, id); err == nil {
		out.Business = &b
	} else if !errors.Is(err, domain.ErrNotFound) {
		return out, err
	}
	if p, err := c.profiles.CreatorProfile(ctx, id); err == nil {
		out.Creator = &p
	} else if !errors.Is(err, domain.ErrNotFound) {
		return out, err
	}
	return out, nil
}

// newBlobStore — где лежат файлы фото: S3-совместимое (MEDIA_STORE=s3) или PostgreSQL (на хостинге без диска).
func newBlobStore(cfg config.Config, pool *pgxpool.Pool, log *slog.Logger) service.BlobStore {
	if cfg.MediaStore == config.MediaS3 {
		log.Info("фото: S3", "endpoint", cfg.StorageEndpoint, "bucket", cfg.StorageBucket)
		return storage.NewS3(cfg.StorageEndpoint, cfg.StorageBucket, cfg.StorageRegion, cfg.StorageKey, cfg.StorageSecret)
	}
	log.Info("фото: PostgreSQL")
	return storage.NewPostgres(pool)
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

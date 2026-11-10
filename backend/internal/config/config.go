// Package config — настройки сервера только из переменных окружения (список — .env.example и render.yaml).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Режимы.
const (
	EnvProduction  = "production"
	EnvDevelopment = "development"
)

// Способы отправки писем.
const (
	MailResend = "resend"
	MailSMTP   = "smtp"
	MailLog    = "log"
)

// Приём сообщений бота.
const (
	TelegramWebhook = "webhook" // setWebhook на ${PUBLIC_URL}/api/telegram/webhook при старте (нужен https)
	TelegramPolling = "polling" // getUpdates в фоне (локально); вебхук при этом снимается
	TelegramOff     = "off"     // бот не принимает сообщения (вход через бота выключен)
)

// Config — настройки API.
type Config struct {
	AppEnv      string // APP_ENV: production | development (по умолчанию development)
	DatabaseURL string // DATABASE_URL — обязательно
	HTTPAddr    string // HTTP_ADDR, по умолчанию :8080
	PublicURL   string // PUBLIC_URL — единственный источник адреса сайта (redirect Google, cookie, письма)
	AuthSecret  string // AUTH_SECRET — ключ HMAC для кодов из писем

	MailProvider string // MAIL_PROVIDER: resend | smtp | log (пусто — выбирается по ключам)
	MailFrom     string // MAIL_FROM: «Охват <hello@домен>»
	ResendAPIKey string // RESEND_API_KEY
	SMTPHost     string // SMTP_HOST
	SMTPPort     string // SMTP_PORT (465 — TLS, 587 — STARTTLS)
	SMTPUser     string // SMTP_USER
	SMTPPassword string // SMTP_PASSWORD
	SMTPFrom     string // SMTP_FROM (пусто — MAIL_FROM)

	TelegramBotToken   string        // TELEGRAM_BOT_TOKEN
	TelegramBotName    string        // TELEGRAM_BOT_NAME — имя бота без @ (ссылка t.me/<имя>)
	TelegramSecret     string        // TELEGRAM_WEBHOOK_SECRET — секрет вебхука (заголовок X-Telegram-Bot-Api-Secret-Token)
	TelegramMode       string        // TELEGRAM_MODE: webhook | polling | off; пусто — webhook при https, иначе только приём эмуляции
	TelegramAPIURL     string        // TELEGRAM_API_URL — адрес Bot API (по умолчанию https://api.telegram.org; в e2e — подставной)
	TelegramLoginTTL   time.Duration // TELEGRAM_LOGIN_TTL_MIN, 5
	TelegramLoginPerIP int           // TELEGRAM_LOGIN_PER_IP_HOUR, 30
	TelegramSendRPS    float64       // TELEGRAM_SEND_RPS — сообщений бота в секунду, 25
	GoogleClientID     string        // GOOGLE_CLIENT_ID
	GoogleClientSecret string        // GOOGLE_CLIENT_SECRET

	CodeTTL      time.Duration // AUTH_CODE_TTL_MIN, 10
	CodeAttempts int           // AUTH_CODE_MAX_ATTEMPTS, 5
	CodeResend   time.Duration // AUTH_CODE_RESEND_SEC, 60
	CodePerEmail int           // AUTH_CODE_PER_EMAIL_HOUR, 5
	CodePerIP    int           // AUTH_CODE_PER_IP_HOUR, 20
	SessionTTL   time.Duration // AUTH_SESSION_DAYS, 30
	RateRPS      float64       // RATE_LIMIT_RPS — запросов входа в секунду с адреса, 1
	RateBurst    int           // RATE_LIMIT_BURST — запас подряд, 10
	APIRPS       float64       // RATE_LIMIT_API_RPS — запросов ко всему /api в секунду с адреса, 10
	APIBurst     int           // RATE_LIMIT_API_BURST, 60
	UserRPS      float64       // RATE_LIMIT_USER_RPS — запросов к /api/me* в секунду на пользователя, 5
	UserBurst    int           // RATE_LIMIT_USER_BURST, 30
	WebhookRPS   float64       // TELEGRAM_WEBHOOK_RPS — обновлений бота в секунду на весь вебхук, 50
	WebhookBurst int           // TELEGRAM_WEBHOOK_BURST, 200
	RedisURL     string        // REDIS_URL — общие лимиты для нескольких серверов; пусто — лимиты в памяти процесса
	MailWorkers  int           // MAIL_WORKERS, 2

	TasksMax      int // TASKS_MAX_PER_USER — заданий на пользователя, 50
	SubmitsPerDay int // MODERATION_SUBMITS_PER_DAY — отправок на проверку в сутки, 10

	MediaStore      string  // MEDIA_STORE: postgres | s3 (postgres)
	StorageEndpoint string  // STORAGE_ENDPOINT — https://… (R2, Яндекс Object Storage, MinIO)
	StorageBucket   string  // STORAGE_BUCKET
	StorageRegion   string  // STORAGE_REGION — auto (R2), ru-central1 (Яндекс)
	StorageKey      string  // STORAGE_ACCESS_KEY
	StorageSecret   string  // STORAGE_SECRET_KEY
	MediaMaxMB      int     // MEDIA_MAX_MB — один файл, 10
	MediaQuotaMB    int     // MEDIA_QUOTA_MB — на пользователя, 300
	MediaMaxFiles   int     // MEDIA_MAX_FILES — на пользователя, 300
	MediaWorkers    int     // MEDIA_WORKERS — обработка фото параллельно; 0 — половина ядер
	UploadRPS       float64 // RATE_LIMIT_UPLOAD_RPS — загрузок фото в секунду на пользователя, 0.5
	UploadBurst     int     // RATE_LIMIT_UPLOAD_BURST, 20

	Warnings []string // некритичные замечания для журнала при запуске
}

// Где хранить фото (MEDIA_STORE).
const (
	MediaPostgres = "postgres"
	MediaS3       = "s3"
)

// Production — боевой режим.
func (c Config) Production() bool { return c.AppEnv == EnvProduction }

// SecureCookies — cookie только по https (адрес сайта на https).
func (c Config) SecureCookies() bool { return strings.HasPrefix(c.PublicURL, "https://") }

// Origin — схема и хост сайта (для проверки Origin у изменяющих запросов).
func (c Config) Origin() string {
	u, err := url.Parse(c.PublicURL)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// GoogleRedirectURL — адрес возврата из Google.
func (c Config) GoogleRedirectURL() string { return c.PublicURL + "/api/auth/google/callback" }

// GoogleEnabled — ключи Google заданы.
func (c Config) GoogleEnabled() bool { return c.GoogleClientID != "" && c.GoogleClientSecret != "" }

// BotEnabled — бот принимает сообщения (вход через бота, команды, уведомления).
func (c Config) BotEnabled() bool {
	return c.TelegramBotToken != "" && c.TelegramBotName != "" && c.TelegramSecret != "" && c.TelegramMode != TelegramOff
}

// TelegramWebhookURL — адрес вебхука для setWebhook ("" — вебхук при старте не ставим).
func (c Config) TelegramWebhookURL() string {
	if !c.BotEnabled() || c.TelegramMode != TelegramWebhook {
		return ""
	}
	return c.PublicURL + "/api/telegram/webhook"
}

// Load читает настройки из окружения.
func Load() (Config, error) { return load(os.Getenv) }

func load(get func(string) string) (Config, error) {
	c := Config{
		AppEnv:             strings.ToLower(strings.TrimSpace(get("APP_ENV"))),
		DatabaseURL:        get("DATABASE_URL"),
		HTTPAddr:           get("HTTP_ADDR"),
		PublicURL:          strings.TrimRight(strings.TrimSpace(get("PUBLIC_URL")), "/"),
		AuthSecret:         get("AUTH_SECRET"),
		MailProvider:       strings.ToLower(strings.TrimSpace(get("MAIL_PROVIDER"))),
		MailFrom:           strings.TrimSpace(get("MAIL_FROM")),
		ResendAPIKey:       strings.TrimSpace(get("RESEND_API_KEY")),
		SMTPHost:           strings.TrimSpace(get("SMTP_HOST")),
		SMTPPort:           strings.TrimSpace(get("SMTP_PORT")),
		SMTPUser:           strings.TrimSpace(get("SMTP_USER")),
		SMTPPassword:       get("SMTP_PASSWORD"),
		SMTPFrom:           strings.TrimSpace(get("SMTP_FROM")),
		TelegramBotToken:   strings.TrimSpace(get("TELEGRAM_BOT_TOKEN")),
		TelegramBotName:    strings.TrimPrefix(strings.TrimSpace(get("TELEGRAM_BOT_NAME")), "@"),
		TelegramSecret:     strings.TrimSpace(get("TELEGRAM_WEBHOOK_SECRET")),
		TelegramMode:       strings.ToLower(strings.TrimSpace(get("TELEGRAM_MODE"))),
		TelegramAPIURL:     strings.TrimRight(strings.TrimSpace(get("TELEGRAM_API_URL")), "/"),
		GoogleClientID:     strings.TrimSpace(get("GOOGLE_CLIENT_ID")),
		GoogleClientSecret: strings.TrimSpace(get("GOOGLE_CLIENT_SECRET")),
	}
	var errs []error
	num := func(key string, def int) int {
		v := strings.TrimSpace(get(key))
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("%s: нужно целое число больше 0", key))
			return def
		}
		return n
	}
	if c.AppEnv == "" {
		c.AppEnv = EnvDevelopment
	}
	if c.AppEnv != EnvProduction && c.AppEnv != EnvDevelopment {
		errs = append(errs, errors.New("APP_ENV: production или development"))
	}
	if c.HTTPAddr == "" {
		c.HTTPAddr = ":8080"
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("не задан DATABASE_URL"))
	}
	if c.PublicURL == "" {
		if c.Production() {
			errs = append(errs, errors.New("не задан PUBLIC_URL (адрес сайта, https://…)"))
		}
		c.PublicURL = "http://localhost:8000"
	}
	if u, err := url.Parse(c.PublicURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") {
		errs = append(errs, errors.New("PUBLIC_URL: адрес вида https://example.ru без пути"))
	} else if c.Production() && u.Scheme != "https" {
		errs = append(errs, errors.New("PUBLIC_URL: в боевом режиме — только https"))
	}
	if c.AuthSecret == "" {
		c.Warnings = append(c.Warnings, "AUTH_SECRET не задан — коды из писем не переживут перезапуск сервера")
	} else if len(c.AuthSecret) < 32 {
		errs = append(errs, errors.New("AUTH_SECRET: не короче 32 символов"))
	}
	if c.MailFrom == "" {
		c.MailFrom = "Охват <onboarding@resend.dev>"
	}
	if c.SMTPFrom == "" {
		c.SMTPFrom = c.MailFrom
	}
	if c.SMTPPort == "" {
		c.SMTPPort = "465"
	}
	if c.MailProvider == "" {
		switch {
		case c.ResendAPIKey != "":
			c.MailProvider = MailResend
		case c.SMTPHost != "":
			c.MailProvider = MailSMTP
		default:
			c.MailProvider = MailLog
		}
	}
	switch c.MailProvider {
	case MailResend, MailSMTP, MailLog:
	default:
		errs = append(errs, errors.New("MAIL_PROVIDER: resend, smtp или log"))
	}
	if c.TelegramBotToken != "" && !strings.Contains(c.TelegramBotToken, ":") {
		errs = append(errs, errors.New("TELEGRAM_BOT_TOKEN: вид «123456:ABC…» от @BotFather"))
	}
	if c.TelegramSecret != "" && !validSecret(c.TelegramSecret) {
		errs = append(errs, errors.New("TELEGRAM_WEBHOOK_SECRET: 1–256 символов A-Z, a-z, 0-9, _ и -"))
	}
	if c.TelegramBotName != "" && !validBotName(c.TelegramBotName) {
		errs = append(errs, errors.New("TELEGRAM_BOT_NAME: имя бота латиницей, оканчивается на bot"))
	}
	if c.TelegramAPIURL != "" {
		if u, err := url.Parse(c.TelegramAPIURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			errs = append(errs, errors.New("TELEGRAM_API_URL: адрес вида https://api.telegram.org"))
		}
	}
	switch c.TelegramMode {
	case "":
		c.TelegramMode = TelegramWebhook
		if !strings.HasPrefix(c.PublicURL, "https://") {
			c.TelegramMode = "" // http (локально): Telegram не шлёт вебхуки; приём только эмуляции (e2e) — или polling
		}
	case TelegramWebhook:
		if !strings.HasPrefix(c.PublicURL, "https://") {
			errs = append(errs, errors.New("TELEGRAM_MODE=webhook: нужен PUBLIC_URL на https"))
		}
	case TelegramPolling, TelegramOff:
	default:
		errs = append(errs, errors.New("TELEGRAM_MODE: webhook, polling или off"))
	}
	if c.TelegramBotToken != "" && c.TelegramMode != TelegramOff {
		switch {
		case c.TelegramBotName == "":
			c.Warnings = append(c.Warnings, "TELEGRAM_BOT_NAME не задан — вход через бота выключен")
		case c.TelegramSecret == "":
			c.Warnings = append(c.Warnings, "TELEGRAM_WEBHOOK_SECRET не задан — бот не принимает сообщения, вход через бота выключен")
		}
	}
	if (c.GoogleClientID == "") != (c.GoogleClientSecret == "") {
		c.Warnings = append(c.Warnings, "Google: нужны оба ключа — GOOGLE_CLIENT_ID и GOOGLE_CLIENT_SECRET; вход через Google выключен")
	}
	c.CodeTTL = time.Duration(num("AUTH_CODE_TTL_MIN", 10)) * time.Minute
	c.CodeAttempts = num("AUTH_CODE_MAX_ATTEMPTS", 5)
	c.CodeResend = time.Duration(num("AUTH_CODE_RESEND_SEC", 60)) * time.Second
	c.CodePerEmail = num("AUTH_CODE_PER_EMAIL_HOUR", 5)
	c.CodePerIP = num("AUTH_CODE_PER_IP_HOUR", 20)
	c.SessionTTL = time.Duration(num("AUTH_SESSION_DAYS", 30)) * 24 * time.Hour
	c.RateBurst = num("RATE_LIMIT_BURST", 10)
	c.MailWorkers = num("MAIL_WORKERS", 2)
	c.TelegramLoginTTL = time.Duration(num("TELEGRAM_LOGIN_TTL_MIN", 5)) * time.Minute
	c.TelegramLoginPerIP = num("TELEGRAM_LOGIN_PER_IP_HOUR", 30)
	c.TelegramSendRPS = 25
	if v := strings.TrimSpace(get("TELEGRAM_SEND_RPS")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 || f > 30 {
			errs = append(errs, errors.New("TELEGRAM_SEND_RPS: число от 1 до 30 (лимит Telegram)"))
		} else {
			c.TelegramSendRPS = f
		}
	}
	rate := func(key string, def float64) float64 {
		v := strings.TrimSpace(get(key))
		if v == "" {
			return def
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			errs = append(errs, fmt.Errorf("%s: число больше 0, например 1 или 0.5", key))
			return def
		}
		return f
	}
	c.RateRPS = rate("RATE_LIMIT_RPS", 1)
	c.APIRPS = rate("RATE_LIMIT_API_RPS", 10)
	c.APIBurst = num("RATE_LIMIT_API_BURST", 60)
	c.UserRPS = rate("RATE_LIMIT_USER_RPS", 5)
	c.UserBurst = num("RATE_LIMIT_USER_BURST", 30)
	c.WebhookRPS = rate("TELEGRAM_WEBHOOK_RPS", 50)
	c.WebhookBurst = num("TELEGRAM_WEBHOOK_BURST", 200)
	c.TasksMax = num("TASKS_MAX_PER_USER", 50)
	c.SubmitsPerDay = num("MODERATION_SUBMITS_PER_DAY", 10)
	c.MediaMaxMB = num("MEDIA_MAX_MB", 10)
	c.MediaQuotaMB = num("MEDIA_QUOTA_MB", 300)
	c.MediaMaxFiles = num("MEDIA_MAX_FILES", 300)
	c.MediaWorkers = num("MEDIA_WORKERS", 0)
	c.UploadRPS = rate("RATE_LIMIT_UPLOAD_RPS", 0.5)
	c.UploadBurst = num("RATE_LIMIT_UPLOAD_BURST", 20)
	c.MediaStore = strings.ToLower(strings.TrimSpace(get("MEDIA_STORE")))
	c.StorageEndpoint = strings.TrimRight(strings.TrimSpace(get("STORAGE_ENDPOINT")), "/")
	c.StorageBucket = strings.TrimSpace(get("STORAGE_BUCKET"))
	c.StorageRegion = strings.TrimSpace(get("STORAGE_REGION"))
	c.StorageKey = strings.TrimSpace(get("STORAGE_ACCESS_KEY"))
	c.StorageSecret = strings.TrimSpace(get("STORAGE_SECRET_KEY"))
	switch c.MediaStore {
	case "", MediaPostgres:
		c.MediaStore = MediaPostgres
	case MediaS3:
		if !strings.HasPrefix(c.StorageEndpoint, "https://") && !strings.HasPrefix(c.StorageEndpoint, "http://") ||
			c.StorageBucket == "" || c.StorageKey == "" || c.StorageSecret == "" {
			errs = append(errs, errors.New("MEDIA_STORE=s3: нужны STORAGE_ENDPOINT (https://…), STORAGE_BUCKET, STORAGE_ACCESS_KEY, STORAGE_SECRET_KEY"))
		}
	default:
		errs = append(errs, errors.New("MEDIA_STORE: postgres или s3"))
	}
	c.RedisURL = strings.TrimSpace(get("REDIS_URL"))
	if c.RedisURL != "" && !strings.HasPrefix(c.RedisURL, "redis://") && !strings.HasPrefix(c.RedisURL, "rediss://") {
		errs = append(errs, errors.New("REDIS_URL: адрес вида redis://… или rediss://…"))
	}
	return c, errors.Join(errs...)
}

// MailReady — какой способ отправки писем реально доступен; "" — вход по почте выключен (с причиной).
// В боевом режиме журнал вместо писем запрещён: коды не должны попадать в логи.
func (c Config) MailReady() (provider, reason string) {
	switch c.MailProvider {
	case MailResend:
		if c.ResendAPIKey == "" {
			return "", "MAIL_PROVIDER=resend, но RESEND_API_KEY не задан"
		}
	case MailSMTP:
		if c.SMTPHost == "" || c.SMTPUser == "" || c.SMTPPassword == "" {
			return "", "MAIL_PROVIDER=smtp, но не заданы SMTP_HOST / SMTP_USER / SMTP_PASSWORD"
		}
	case MailLog:
		if c.Production() {
			return "", "в боевом режиме нужен RESEND_API_KEY или SMTP — коды не пишутся в журнал"
		}
	}
	return c.MailProvider, ""
}

func validSecret(s string) bool {
	if len(s) > 256 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func validBotName(s string) bool {
	if len(s) < 5 || len(s) > 32 || !strings.HasSuffix(strings.ToLower(s), "bot") {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

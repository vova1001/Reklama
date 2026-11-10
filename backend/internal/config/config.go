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

	TelegramBotToken   string // TELEGRAM_BOT_TOKEN
	TelegramBotName    string // TELEGRAM_BOT_NAME — имя бота без @ (подпись у кнопки)
	GoogleClientID     string // GOOGLE_CLIENT_ID
	GoogleClientSecret string // GOOGLE_CLIENT_SECRET

	CodeTTL      time.Duration // AUTH_CODE_TTL_MIN, 10
	CodeAttempts int           // AUTH_CODE_MAX_ATTEMPTS, 5
	CodeResend   time.Duration // AUTH_CODE_RESEND_SEC, 60
	CodePerEmail int           // AUTH_CODE_PER_EMAIL_HOUR, 5
	CodePerIP    int           // AUTH_CODE_PER_IP_HOUR, 20
	SessionTTL   time.Duration // AUTH_SESSION_DAYS, 30
	RateRPS      float64       // RATE_LIMIT_RPS — запросов входа в секунду с адреса, 1
	RateBurst    int           // RATE_LIMIT_BURST — запас подряд, 10
	MailWorkers  int           // MAIL_WORKERS, 2

	Warnings []string // некритичные замечания для журнала при запуске
}

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
	c.RateRPS = 1
	if v := strings.TrimSpace(get("RATE_LIMIT_RPS")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			errs = append(errs, errors.New("RATE_LIMIT_RPS: число больше 0, например 1 или 0.5"))
		} else {
			c.RateRPS = f
		}
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

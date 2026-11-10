package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoad(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		err   string
		check func(t *testing.T, c Config)
	}{
		{name: "разработка: умолчания", env: map[string]string{"DATABASE_URL": "postgres://x"}, check: func(t *testing.T, c Config) {
			if c.Production() || c.PublicURL != "http://localhost:8000" || c.SecureCookies() || c.MailProvider != MailLog ||
				c.CodeTTL != 10*time.Minute || c.CodeAttempts != 5 || c.CodeResend != time.Minute || c.RateRPS != 1 || c.RateBurst != 10 {
				t.Errorf("%+v", c)
			}
			if p, _ := c.MailReady(); p != MailLog {
				t.Error("в разработке коды — в журнал")
			}
			if len(c.Warnings) == 0 {
				t.Error("предупреждение про AUTH_SECRET")
			}
		}},
		{name: "бой: адрес и ключи", env: map[string]string{"APP_ENV": "production", "DATABASE_URL": "x", "PUBLIC_URL": "https://ohvat.ru/",
			"AUTH_SECRET": strings.Repeat("s", 32), "RESEND_API_KEY": "re_x", "GOOGLE_CLIENT_ID": "id", "GOOGLE_CLIENT_SECRET": "sec",
			"TELEGRAM_BOT_TOKEN": "1:x", "TELEGRAM_BOT_NAME": "@reklama_auth_bot", "TELEGRAM_WEBHOOK_SECRET": "s3cr-et_X", "RATE_LIMIT_RPS": "0.5"},
			check: func(t *testing.T, c Config) {
				if c.PublicURL != "https://ohvat.ru" || !c.SecureCookies() || c.Origin() != "https://ohvat.ru" ||
					c.GoogleRedirectURL() != "https://ohvat.ru/api/auth/google/callback" || !c.GoogleEnabled() ||
					c.MailProvider != MailResend || c.TelegramBotName != "reklama_auth_bot" || c.RateRPS != 0.5 ||
					!c.BotEnabled() || c.TelegramMode != TelegramWebhook || c.TelegramWebhookURL() != "https://ohvat.ru/api/telegram/webhook" ||
					c.TelegramLoginTTL != 5*time.Minute || c.TelegramLoginPerIP != 30 || c.TelegramSendRPS != 25 {
					t.Errorf("%+v", c)
				}
			}},
		{name: "бой без ключей почты — вход по почте выключен, коды в журнал не пишутся",
			env: map[string]string{"APP_ENV": "production", "DATABASE_URL": "x", "PUBLIC_URL": "https://a.ru"},
			check: func(t *testing.T, c Config) {
				if p, why := c.MailReady(); p != "" || why == "" {
					t.Errorf("%q %q", p, why)
				}
			}},
		{name: "smtp", env: map[string]string{"DATABASE_URL": "x", "MAIL_PROVIDER": "smtp", "SMTP_HOST": "smtp.yandex.ru", "SMTP_USER": "u", "SMTP_PASSWORD": "p"},
			check: func(t *testing.T, c Config) {
				if p, _ := c.MailReady(); p != MailSMTP || c.SMTPPort != "465" || c.SMTPFrom != c.MailFrom {
					t.Errorf("%+v", c)
				}
			}},
		{name: "smtp без пароля", env: map[string]string{"DATABASE_URL": "x", "MAIL_PROVIDER": "smtp", "SMTP_HOST": "h"},
			check: func(t *testing.T, c Config) {
				if p, _ := c.MailReady(); p != "" {
					t.Error(p)
				}
			}},
		{name: "resend без ключа", env: map[string]string{"DATABASE_URL": "x", "MAIL_PROVIDER": "resend"},
			check: func(t *testing.T, c Config) {
				if p, _ := c.MailReady(); p != "" {
					t.Error(p)
				}
			}},
		{name: "SMTP по ключам", env: map[string]string{"DATABASE_URL": "x", "SMTP_HOST": "h"}, check: func(t *testing.T, c Config) {
			if c.MailProvider != MailSMTP {
				t.Error(c.MailProvider)
			}
		}},
		{name: "половина ключей Google", env: map[string]string{"DATABASE_URL": "x", "GOOGLE_CLIENT_ID": "id"}, check: func(t *testing.T, c Config) {
			if c.GoogleEnabled() || len(c.Warnings) < 2 {
				t.Errorf("%+v", c)
			}
		}},
		{name: "нет базы", env: map[string]string{}, err: "DATABASE_URL"},
		{name: "бой без адреса", env: map[string]string{"APP_ENV": "production", "DATABASE_URL": "x"}, err: "PUBLIC_URL"},
		{name: "бой на http", env: map[string]string{"APP_ENV": "production", "DATABASE_URL": "x", "PUBLIC_URL": "http://a.ru"}, err: "https"},
		{name: "адрес с путём", env: map[string]string{"DATABASE_URL": "x", "PUBLIC_URL": "https://a.ru/app"}, err: "без пути"},
		{name: "плохой режим", env: map[string]string{"DATABASE_URL": "x", "APP_ENV": "prod"}, err: "APP_ENV"},
		{name: "короткий секрет", env: map[string]string{"DATABASE_URL": "x", "AUTH_SECRET": "short"}, err: "AUTH_SECRET"},
		{name: "плохая почта", env: map[string]string{"DATABASE_URL": "x", "MAIL_PROVIDER": "pigeon"}, err: "MAIL_PROVIDER"},
		{name: "плохой токен", env: map[string]string{"DATABASE_URL": "x", "TELEGRAM_BOT_TOKEN": "nocolon"}, err: "TELEGRAM_BOT_TOKEN"},
		{name: "бот локально: без вебхука, приём эмуляции", env: map[string]string{"DATABASE_URL": "x", "TELEGRAM_BOT_TOKEN": "1:x",
			"TELEGRAM_BOT_NAME": "test_bot", "TELEGRAM_WEBHOOK_SECRET": "s", "TELEGRAM_API_URL": "http://faketg:8081/", "TELEGRAM_LOGIN_TTL_MIN": "2"},
			check: func(t *testing.T, c Config) {
				if !c.BotEnabled() || c.TelegramMode != "" || c.TelegramWebhookURL() != "" || c.TelegramAPIURL != "http://faketg:8081" || c.TelegramLoginTTL != 2*time.Minute {
					t.Errorf("%+v", c)
				}
			}},
		{name: "бот без секрета — выключен с предупреждением", env: map[string]string{"DATABASE_URL": "x", "TELEGRAM_BOT_TOKEN": "1:x", "TELEGRAM_BOT_NAME": "test_bot", "AUTH_SECRET": strings.Repeat("s", 32)},
			check: func(t *testing.T, c Config) {
				if c.BotEnabled() || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "TELEGRAM_WEBHOOK_SECRET") {
					t.Errorf("%+v", c)
				}
			}},
		{name: "бот выключен", env: map[string]string{"DATABASE_URL": "x", "TELEGRAM_BOT_TOKEN": "1:x", "TELEGRAM_BOT_NAME": "test_bot", "TELEGRAM_WEBHOOK_SECRET": "s", "TELEGRAM_MODE": "off"},
			check: func(t *testing.T, c Config) {
				if c.BotEnabled() {
					t.Error("off")
				}
			}},
		{name: "плохой секрет вебхука", env: map[string]string{"DATABASE_URL": "x", "TELEGRAM_WEBHOOK_SECRET": "a b"}, err: "TELEGRAM_WEBHOOK_SECRET"},
		{name: "плохое имя бота", env: map[string]string{"DATABASE_URL": "x", "TELEGRAM_BOT_NAME": "t.me/x"}, err: "TELEGRAM_BOT_NAME"},
		{name: "вебхук без https", env: map[string]string{"DATABASE_URL": "x", "TELEGRAM_MODE": "webhook"}, err: "https"},
		{name: "плохой режим бота", env: map[string]string{"DATABASE_URL": "x", "TELEGRAM_MODE": "push"}, err: "TELEGRAM_MODE"},
		{name: "плохой адрес Bot API", env: map[string]string{"DATABASE_URL": "x", "TELEGRAM_API_URL": "ftp//x"}, err: "TELEGRAM_API_URL"},
		{name: "rps бота выше лимита", env: map[string]string{"DATABASE_URL": "x", "TELEGRAM_SEND_RPS": "100"}, err: "TELEGRAM_SEND_RPS"},
		{name: "плохой лимит", env: map[string]string{"DATABASE_URL": "x", "AUTH_CODE_PER_IP_HOUR": "-1"}, err: "AUTH_CODE_PER_IP_HOUR"},
		{name: "плохой rps", env: map[string]string{"DATABASE_URL": "x", "RATE_LIMIT_RPS": "abc"}, err: "RATE_LIMIT_RPS"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := load(env(c.env))
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("ждали ошибку про %s, получили %v", c.err, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			c.check(t, cfg)
		})
	}
}

func TestLimitsAndRedis(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://x"}
	cfg, err := load(env(base))
	if err != nil || cfg.APIRPS != 10 || cfg.APIBurst != 60 || cfg.UserRPS != 5 || cfg.UserBurst != 30 ||
		cfg.WebhookRPS != 50 || cfg.WebhookBurst != 200 || cfg.RateRPS != 1 || cfg.RedisURL != "" {
		t.Fatalf("по умолчанию: %v %+v", err, cfg)
	}
	set := func(k, v string) map[string]string {
		m := map[string]string{}
		for a, b := range base {
			m[a] = b
		}
		m[k] = v
		return m
	}
	if cfg, err := load(env(set("RATE_LIMIT_API_RPS", "2.5"))); err != nil || cfg.APIRPS != 2.5 {
		t.Errorf("API RPS: %v %v", err, cfg.APIRPS)
	}
	if cfg, err := load(env(set("REDIS_URL", " rediss://u:p@redis:6380/0 "))); err != nil || cfg.RedisURL != "rediss://u:p@redis:6380/0" {
		t.Errorf("REDIS_URL: %v %q", err, cfg.RedisURL)
	}
	for k, v := range map[string]string{"RATE_LIMIT_USER_RPS": "0", "TELEGRAM_WEBHOOK_RPS": "abc", "REDIS_URL": "http://redis"} {
		if _, err := load(env(set(k, v))); err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%s=%s: %v", k, v, err)
		}
	}
}

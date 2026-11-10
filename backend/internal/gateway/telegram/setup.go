package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
)

// SetupOptions — настройка бота при старте.
type SetupOptions struct {
	BotName       string // ожидаемое имя (TELEGRAM_BOT_NAME) — сверяется с getMe
	WebhookURL    string // "" — вебхук не ставим
	WebhookSecret string
	Polling       bool // приём через getUpdates: вебхук снимается
}

// Commands — меню бота.
var Commands = []Command{
	{Command: "start", Description: "Начало и включить уведомления"},
	{Command: "profile", Description: "Мой аккаунт"},
	{Command: "help", Description: "Помощь"},
	{Command: "stop", Description: "Выключить уведомления"},
}

// ErrBotName — токен принадлежит другому боту.
var ErrBotName = errors.New("TELEGRAM_BOT_NAME не совпадает с ботом токена")

// Setup — getMe (токен верный, имя то), меню команд, вебхук (или снять вебхук для getUpdates).
// Возвращает состояние вебхука для журнала.
func (c *Client) Setup(ctx context.Context, o SetupOptions) (WebhookInfo, error) {
	me, err := c.GetMe(ctx)
	if err != nil {
		return WebhookInfo{}, err
	}
	if o.BotName != "" && !strings.EqualFold(me.Username, o.BotName) {
		return WebhookInfo{}, ErrBotName
	}
	if err := c.SetMyCommands(ctx, Commands); err != nil {
		return WebhookInfo{}, err
	}
	switch {
	case o.WebhookURL != "":
		if err := c.SetWebhook(ctx, WebhookParams{URL: o.WebhookURL, SecretToken: o.WebhookSecret, AllowedUpdates: AllowedUpdates, MaxConnections: 20}); err != nil {
			return WebhookInfo{}, err
		}
	case o.Polling:
		if err := c.DeleteWebhook(ctx); err != nil {
			return WebhookInfo{}, err
		}
	}
	return c.GetWebhookInfo(ctx)
}

// SetupWithRetry повторяет Setup с растущей паузой (5 с → 5 мин), пока не получится или не отменят ctx.
// Неверное имя бота или токен (401/404) — не повторяется.
func (c *Client) SetupWithRetry(ctx context.Context, o SetupOptions, log *slog.Logger) error {
	wait := 5 * time.Second
	for {
		info, err := c.Setup(ctx, o)
		if err == nil {
			log.Info("бот: настроен", "webhook", info.URL, "pending", info.PendingUpdateCount,
				"last_error", info.LastErrorMessage, "polling", o.Polling)
			return nil
		}
		var ae *APIError
		if errors.Is(err, ErrBotName) || (errors.As(err, &ae) && (ae.Code == 401 || ae.Code == 404)) {
			log.Error("бот: не настроен — проверьте TELEGRAM_BOT_TOKEN и TELEGRAM_BOT_NAME", "err", err)
			return nil // сайт работает и без бота
		}
		log.Warn("бот: настройка не удалась, повтор", "in", wait.String(), "err", err)
		if !sleep(ctx, wait) {
			return nil
		}
		if wait *= 2; wait > 5*time.Minute {
			wait = 5 * time.Minute
		}
	}
}

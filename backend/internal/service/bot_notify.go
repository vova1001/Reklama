package service

import (
	"context"
	"errors"
	"log/slog"

	"ohvat/backend/internal/domain"
)

// BotNotifier — уведомления через бота Telegram (реализация Notifier): пользователь сайта → его чат с ботом,
// если Telegram привязан, бот не остановлен и уведомления не выключены (/stop).
type BotNotifier struct {
	chats BotChatRepository
	out   BotMessenger
	texts botTexts
	log   *slog.Logger
}

// NewBotNotifier создаёт уведомления через бота.
func NewBotNotifier(chats BotChatRepository, out BotMessenger, siteURL string, log *slog.Logger) *BotNotifier {
	if log == nil {
		log = slog.Default()
	}
	return &BotNotifier{chats: chats, out: out, texts: newBotTexts(siteURL), log: log}
}

// ErrBadNotification — у уведомления не заполнены нужные поля или неизвестный тип.
var ErrBadNotification = errors.New("уведомление без нужных полей")

// Notify ставит уведомление в очередь бота. Некуда писать — nil.
func (n *BotNotifier) Notify(ctx context.Context, userID int64, x Notification) error {
	chat, err := n.chats.NotifyChat(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	m, ok := n.texts.notification(chat.ChatID, userID, x)
	if !ok {
		return ErrBadNotification
	}
	if err := n.out.Send(ctx, m); err != nil {
		return err
	}
	n.log.InfoContext(ctx, "уведомление поставлено в очередь", "user", userID, "kind", x.Kind)
	return nil
}

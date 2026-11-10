package service

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"ohvat/backend/internal/domain"
)

// ===== бот Telegram: команды, подтверждение входа, уведомления =====
// Транспорт (transport/telegram) разбирает обновления Telegram в BotUpdate; отправка — через BotMessenger
// (очередь в gateway/telegram с лимитами Telegram). Бот отвечает только в личных чатах.

// BotUpdate — входящее событие бота без деталей протокола Telegram.
type BotUpdate struct {
	ID           int64
	ChatID       int64
	Private      bool
	From         domain.TelegramProfile
	Text         string // текст сообщения (команда)
	CallbackID   string // нажатие inline-кнопки
	CallbackData string
	MessageID    int64 // сообщение с кнопкой (для правки)
	Blocked      bool  // пользователь остановил бота
	Unblocked    bool  // пользователь снова запустил бота
}

// BotButton — кнопка под сообщением: ссылка (URL) или действие (Data, до 64 байт).
type BotButton struct {
	Text string
	URL  string
	Data string
}

// BotMessage — сообщение бота (HTML-разметка Telegram). EditMessageID ≠ 0 — правка сообщения.
type BotMessage struct {
	ChatID        int64
	EditMessageID int64
	Text          string
	Buttons       [][]BotButton
}

// BotMessenger — отправка сообщений бота. Реализация ставит в очередь и не ждёт доставки.
type BotMessenger interface {
	Send(ctx context.Context, m BotMessage) error
	AnswerCallback(ctx context.Context, callbackID, text string) error
}

// BotChatRepository — личные чаты бота.
type BotChatRepository interface {
	SaveChat(ctx context.Context, tgUserID, chatID int64, username string) (domain.TelegramChat, error)
	SetNotify(ctx context.Context, tgUserID int64, on bool) error
	BlockChat(ctx context.Context, chatID int64) error
	Chat(ctx context.Context, tgUserID int64) (domain.TelegramChat, error)
	NotifyChat(ctx context.Context, userID int64) (domain.TelegramChat, error)
}

// BotUsers — пользователи сайта для команды /profile.
type BotUsers interface {
	UserByIdentity(ctx context.Context, provider, subject string) (domain.User, error)
}

// BotAuth — вход через бота и завершение сеансов (сервис Auth).
type BotAuth interface {
	BotLoginPending(ctx context.Context, token string) (domain.TelegramLogin, error)
	ConfirmBotLogin(ctx context.Context, token string, p domain.TelegramProfile) (domain.TelegramLogin, error)
	CancelBotLogin(ctx context.Context, token string) error
	LogoutEverywhere(ctx context.Context, userID int64) (int64, error)
}

// BotDeps — зависимости бота.
type BotDeps struct {
	Auth      BotAuth
	Users     BotUsers
	Chats     BotChatRepository
	Messenger BotMessenger
	Clock     Clock
	Log       *slog.Logger
}

// BotConfig — настройки бота.
type BotConfig struct {
	SiteURL string // PUBLIC_URL: кнопки-ссылки только для https (Telegram не принимает http://localhost)
}

// Bot — обработка сообщений и нажатий.
type Bot struct {
	auth  BotAuth
	users BotUsers
	chats BotChatRepository
	out   BotMessenger
	clock Clock
	log   *slog.Logger
	texts botTexts
}

// NewBot создаёт бота.
func NewBot(d BotDeps, cfg BotConfig) *Bot {
	if d.Clock == nil {
		d.Clock = SystemClock{}
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Bot{auth: d.Auth, users: d.Users, chats: d.Chats, out: d.Messenger, clock: d.Clock, log: d.Log, texts: newBotTexts(cfg.SiteURL)}
}

// Действия inline-кнопок (callback_data ≤ 64 байт).
const (
	cbConfirm   = "ok:"  // + токен входа
	cbCancel    = "no:"  // + токен входа
	cbLogoutAll = "out:" // + id пользователя сайта
)

// Handle обрабатывает событие. Ошибка — только сбой инфраструктуры (база): транспорт попросит Telegram повторить.
func (b *Bot) Handle(ctx context.Context, u BotUpdate) error {
	switch {
	case u.Blocked:
		return b.chats.BlockChat(ctx, u.ChatID)
	case u.Unblocked:
		return nil // чат сохранится при первом сообщении
	case !u.Private || u.From.IsBot || u.From.ID <= 0:
		return nil
	case u.CallbackID != "":
		return b.callback(ctx, u)
	case u.Text != "":
		return b.message(ctx, u)
	}
	return nil
}

func (b *Bot) message(ctx context.Context, u BotUpdate) error {
	chat, err := b.chats.SaveChat(ctx, u.From.ID, u.ChatID, u.From.Username)
	if err != nil {
		return err
	}
	cmd, arg := parseCommand(u.Text)
	switch cmd {
	case "/start":
		if tok, ok := ParseBotStart(arg); ok {
			return b.startLogin(ctx, u, tok)
		}
		if strings.HasPrefix(arg, BotStartPrefix) {
			b.send(ctx, b.texts.loginExpired(u.ChatID, 0)) // ссылка обрезана или испорчена
			return nil
		}
		reEnabled := false
		if !chat.Notify {
			if err := b.chats.SetNotify(ctx, u.From.ID, true); err != nil {
				return err
			}
			reEnabled = true
		}
		b.send(ctx, b.texts.welcome(u.ChatID, u.From, reEnabled))
	case "/help":
		b.send(ctx, b.texts.help(u.ChatID))
	case "/profile":
		return b.profile(ctx, u, chat)
	case "/stop":
		if err := b.chats.SetNotify(ctx, u.From.ID, false); err != nil {
			return err
		}
		b.send(ctx, b.texts.stopped(u.ChatID))
	default:
		b.send(ctx, b.texts.unknown(u.ChatID))
	}
	return nil
}

func (b *Bot) startLogin(ctx context.Context, u BotUpdate, tok string) error {
	l, err := b.auth.BotLoginPending(ctx, tok)
	switch {
	case errors.Is(err, ErrBotLoginExpired):
		b.send(ctx, b.texts.loginExpired(u.ChatID, 0))
		return nil
	case err != nil:
		return err
	}
	b.send(ctx, b.texts.loginConfirm(u.ChatID, tok, l.ExpiresAt))
	return nil
}

func (b *Bot) callback(ctx context.Context, u BotUpdate) error {
	d := u.CallbackData
	switch {
	case strings.HasPrefix(d, cbConfirm):
		tok, ok := ParseBotStart(BotStartPrefix + strings.TrimPrefix(d, cbConfirm))
		if !ok {
			break
		}
		if _, err := b.auth.ConfirmBotLogin(ctx, tok, u.From); err != nil {
			if !errors.Is(err, ErrBotLoginExpired) && !errors.Is(err, ErrTelegramInvalid) {
				return err
			}
			b.answer(ctx, u.CallbackID, "Ссылка устарела")
			b.send(ctx, b.texts.loginExpired(u.ChatID, u.MessageID))
			return nil
		}
		// чат нужен для уведомлений: человек мог нажать кнопку, не написав боту после перезапуска базы
		if _, err := b.chats.SaveChat(ctx, u.From.ID, u.ChatID, u.From.Username); err != nil {
			b.log.WarnContext(ctx, "бот: чат не сохранён", "err", err)
		}
		b.answer(ctx, u.CallbackID, "Вход подтверждён")
		b.send(ctx, b.texts.loginConfirmed(u.ChatID, u.MessageID))
		return nil
	case strings.HasPrefix(d, cbCancel):
		tok, ok := ParseBotStart(BotStartPrefix + strings.TrimPrefix(d, cbCancel))
		if !ok {
			break
		}
		if err := b.auth.CancelBotLogin(ctx, tok); err != nil && !errors.Is(err, ErrBotLoginExpired) {
			return err
		}
		b.answer(ctx, u.CallbackID, "Вход отменён")
		b.send(ctx, b.texts.loginCancelled(u.ChatID, u.MessageID))
		return nil
	case strings.HasPrefix(d, cbLogoutAll):
		return b.logoutAll(ctx, u, strings.TrimPrefix(d, cbLogoutAll))
	}
	b.answer(ctx, u.CallbackID, "")
	return nil
}

// logoutAll — «Это не я — выйти везде» из уведомления о входе. Сработает, только если этот Telegram
// привязан к тому же пользователю, что в кнопке.
func (b *Bot) logoutAll(ctx context.Context, u BotUpdate, rawID string) error {
	want, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		b.answer(ctx, u.CallbackID, "")
		return nil
	}
	usr, err := b.users.UserByIdentity(ctx, domain.ProviderTelegram, strconv.FormatInt(u.From.ID, 10))
	if errors.Is(err, domain.ErrNotFound) || (err == nil && usr.ID != want) {
		b.answer(ctx, u.CallbackID, "Этот Telegram не привязан к аккаунту")
		return nil
	}
	if err != nil {
		return err
	}
	n, err := b.auth.LogoutEverywhere(ctx, usr.ID)
	if err != nil {
		return err
	}
	b.log.InfoContext(ctx, "бот: все сеансы завершены", "user", usr.ID, "sessions", n)
	b.answer(ctx, u.CallbackID, "Все сеансы завершены")
	b.send(ctx, b.texts.loggedOutAll(u.ChatID, u.MessageID))
	return nil
}

func (b *Bot) profile(ctx context.Context, u BotUpdate, chat domain.TelegramChat) error {
	usr, err := b.users.UserByIdentity(ctx, domain.ProviderTelegram, strconv.FormatInt(u.From.ID, 10))
	if errors.Is(err, domain.ErrNotFound) {
		b.send(ctx, b.texts.noProfile(u.ChatID))
		return nil
	}
	if err != nil {
		return err
	}
	b.send(ctx, b.texts.profile(u.ChatID, usr, chat.Notify))
	return nil
}

// ChatBlocked — Telegram ответил «бот остановлен пользователем» (403): больше не пишем в этот чат.
func (b *Bot) ChatBlocked(chatID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.chats.BlockChat(ctx, chatID); err != nil {
		b.log.Warn("бот: отметка «остановлен» не сохранена", "err", err)
	}
}

func (b *Bot) send(ctx context.Context, m BotMessage) {
	if err := b.out.Send(ctx, m); err != nil {
		b.log.ErrorContext(ctx, "бот: сообщение не поставлено в очередь", "err", err)
	}
}

func (b *Bot) answer(ctx context.Context, id, text string) {
	if err := b.out.AnswerCallback(ctx, id, text); err != nil {
		b.log.ErrorContext(ctx, "бот: ответ на нажатие не поставлен в очередь", "err", err)
	}
}

// parseCommand — «/start@bot payload» → ("/start", "payload"). Не команда — ("", текст).
func parseCommand(text string) (string, string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", text
	}
	cmd, arg, _ := strings.Cut(text, " ")
	cmd, _, _ = strings.Cut(cmd, "@")
	return strings.ToLower(cmd), strings.TrimSpace(arg)
}

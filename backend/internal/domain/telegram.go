package domain

import "time"

// TelegramProfile — кто пишет боту (данные от Telegram).
type TelegramProfile struct {
	ID        int64
	FirstName string
	LastName  string
	Username  string
	IsBot     bool
}

// Name — имя и фамилия одной строкой.
func (p TelegramProfile) Name() string {
	switch {
	case p.FirstName == "":
		return p.LastName
	case p.LastName == "":
		return p.FirstName
	}
	return p.FirstName + " " + p.LastName
}

// TelegramLogin — вход через бота: ссылка с одноразовым токеном → подтверждение в боте → браузер забирает вход.
// Хранятся только SHA-256 токена из ссылки и секрета браузера.
type TelegramLogin struct {
	ID          int64
	BrowserHash []byte
	TgUserID    int64 // 0 — ещё не подтверждён
	TgName      string
	TgUsername  string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	ConfirmedAt *time.Time
	ConsumedAt  *time.Time
	Cancelled   bool
}

// NewTelegramLogin — новый вход через бота.
type NewTelegramLogin struct {
	TokenHash   []byte
	BrowserHash []byte
	IP          string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// TelegramChat — личный чат с ботом.
type TelegramChat struct {
	TgUserID int64
	ChatID   int64
	Username string
	Notify   bool
	Blocked  bool
}

package telegram

import (
	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// Обновление Telegram — только поля, которые нужны боту (https://core.telegram.org/bots/api#update).
type update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *message       `json:"message"`
	CallbackQuery *callbackQuery `json:"callback_query"`
	MyChatMember  *chatMember    `json:"my_chat_member"`
}

type user struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

type chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Username string `json:"username"`
}

type message struct {
	MessageID int64  `json:"message_id"`
	From      *user  `json:"from"`
	Chat      chat   `json:"chat"`
	Text      string `json:"text"`
}

type callbackQuery struct {
	ID      string   `json:"id"`
	From    user     `json:"from"`
	Message *message `json:"message"`
	Data    string   `json:"data"`
}

type chatMember struct {
	Chat          chat `json:"chat"`
	From          user `json:"from"`
	NewChatMember struct {
		Status string `json:"status"`
	} `json:"new_chat_member"`
}

const maxText = 4096 // длиннее сообщений в Telegram не бывает

// toBot — обновление в событие бота; false — событие боту не нужно.
func (u update) toBot() (service.BotUpdate, bool) {
	out := service.BotUpdate{ID: u.UpdateID}
	switch {
	case u.Message != nil && u.Message.From != nil:
		m := u.Message
		if len(m.Text) == 0 || len(m.Text) > maxText {
			return out, false
		}
		out.ChatID, out.Private, out.From, out.Text = m.Chat.ID, m.Chat.Type == "private", prof(*m.From), m.Text
	case u.CallbackQuery != nil && u.CallbackQuery.Message != nil:
		c := u.CallbackQuery
		if c.ID == "" || len(c.Data) > 64 {
			return out, false
		}
		out.ChatID, out.Private, out.From = c.Message.Chat.ID, c.Message.Chat.Type == "private", prof(c.From)
		out.CallbackID, out.CallbackData, out.MessageID = c.ID, c.Data, c.Message.MessageID
	case u.MyChatMember != nil && u.MyChatMember.Chat.Type == "private":
		m := u.MyChatMember
		out.ChatID, out.Private, out.From = m.Chat.ID, true, prof(m.From)
		switch m.NewChatMember.Status {
		case "kicked":
			out.Blocked = true
		case "member":
			out.Unblocked = true
		default:
			return out, false
		}
	case u.MyChatMember != nil && isChannel(u.MyChatMember.Chat.Type):
		// бота добавили в канал или группу (или убрали): подтверждение соцсети; ответ — в личный чат добавившего
		m := u.MyChatMember
		if m.From.ID <= 0 || m.From.IsBot {
			return out, false
		}
		out.ChatID, out.From = m.Chat.ID, prof(m.From)
		out.Channel = &service.ChannelEvent{ChatID: m.Chat.ID, Title: clip(m.Chat.Title, 255), Username: clip(m.Chat.Username, 64),
			Type: m.Chat.Type, Status: m.NewChatMember.Status, From: out.From}
	default:
		return out, false
	}
	return out, out.ChatID != 0
}

func isChannel(t string) bool { return t == "channel" || t == "group" || t == "supergroup" }

func prof(u user) domain.TelegramProfile {
	return domain.TelegramProfile{ID: u.ID, FirstName: clip(u.FirstName, 64), LastName: clip(u.LastName, 64), Username: clip(u.Username, 64), IsBot: u.IsBot}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

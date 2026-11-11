// Package telegram — Telegram Bot API: клиент (HTTPS, таймауты, разбор ошибок без утечки токена),
// очередь отправки с лимитами Telegram (outbox.go) и настройка бота при старте (setup.go).
// Реализует service.BotMessenger.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL — адрес Bot API.
const DefaultBaseURL = "https://api.telegram.org"

// Client — вызовы Bot API. Токен живёт только в адресе запроса и не попадает в ошибки и журнал.
type Client struct {
	base    string
	token   string
	hc      *http.Client
	timeout time.Duration
}

// Options — настройки клиента.
type Options struct {
	BaseURL string        // DefaultBaseURL; в тестах — адрес подставного сервера
	Timeout time.Duration // дедлайн одного вызова (кроме getUpdates), 10 с
	HTTP    *http.Client  // nil — свой клиент без общего таймаута (дедлайн — через контекст)
}

// New создаёт клиент Bot API.
func New(token string, opt Options) *Client {
	if opt.BaseURL == "" {
		opt.BaseURL = DefaultBaseURL
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 10 * time.Second
	}
	if opt.HTTP == nil {
		opt.HTTP = &http.Client{Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		}}
	}
	return &Client{base: strings.TrimRight(opt.BaseURL, "/"), token: token, hc: opt.HTTP, timeout: opt.Timeout}
}

// APIError — Telegram ответил ok=false.
type APIError struct {
	Method      string
	Code        int // error_code: 400, 403, 429…
	Description string
	RetryAfter  time.Duration // при 429
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Method, e.Code, e.Description)
}

// Temporary — стоит повторить (лимит, сбой Telegram).
func (e *APIError) Temporary() bool { return e.Code == http.StatusTooManyRequests || e.Code >= 500 }

// NetError — сбой сети или таймаут (без адреса запроса: в нём токен).
type NetError struct {
	Method string
	Err    error
}

func (e *NetError) Error() string { return "telegram " + e.Method + ": " + e.Err.Error() }
func (e *NetError) Unwrap() error { return e.Err }

type envelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

const maxResponse = 4 << 20

// Call вызывает метод Bot API с JSON-параметрами; out — куда разобрать result (может быть nil).
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	return c.call(ctx, method, params, out, c.timeout)
}

func (c *Client) call(ctx context.Context, method string, params, out any, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("telegram %s: параметры: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/bot"+c.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return &NetError{Method: method, Err: errors.New("неверный адрес Bot API")}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return &NetError{Method: method, Err: c.clean(err)}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return &NetError{Method: method, Err: c.clean(err)}
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return &APIError{Method: method, Code: resp.StatusCode, Description: "ответ не JSON"}
	}
	if !env.OK {
		code := env.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &APIError{Method: method, Code: code, Description: c.redact(env.Description),
			RetryAfter: time.Duration(env.Parameters.RetryAfter) * time.Second}
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("telegram %s: результат: %w", method, err)
		}
	}
	return nil
}

// clean убирает адрес запроса (в нём токен) из ошибки net/http.
func (c *Client) clean(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return errors.New(c.redact(err.Error()))
}

func (c *Client) redact(s string) string {
	if c.token == "" {
		return s
	}
	return strings.ReplaceAll(s, c.token, "***")
}

// ===== методы =====

// User — бот или пользователь (getMe).
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	IsBot    bool   `json:"is_bot"`
}

// GetMe — кто этот бот.
func (c *Client) GetMe(ctx context.Context) (User, error) {
	var u User
	err := c.Call(ctx, "getMe", struct{}{}, &u)
	return u, err
}

// WebhookInfo — состояние вебхука (getWebhookInfo).
type WebhookInfo struct {
	URL                  string   `json:"url"`
	PendingUpdateCount   int      `json:"pending_update_count"`
	LastErrorDate        int64    `json:"last_error_date"`
	LastErrorMessage     string   `json:"last_error_message"`
	MaxConnections       int      `json:"max_connections"`
	AllowedUpdates       []string `json:"allowed_updates"`
	HasCustomCertificate bool     `json:"has_custom_certificate"`
}

// GetWebhookInfo — состояние вебхука.
func (c *Client) GetWebhookInfo(ctx context.Context) (WebhookInfo, error) {
	var w WebhookInfo
	err := c.Call(ctx, "getWebhookInfo", struct{}{}, &w)
	return w, err
}

// WebhookParams — параметры setWebhook.
type WebhookParams struct {
	URL            string   `json:"url"`
	SecretToken    string   `json:"secret_token,omitempty"`
	AllowedUpdates []string `json:"allowed_updates"`
	MaxConnections int      `json:"max_connections,omitempty"`
	DropPending    bool     `json:"drop_pending_updates,omitempty"`
}

// SetWebhook — куда Telegram присылает обновления.
func (c *Client) SetWebhook(ctx context.Context, p WebhookParams) error {
	return c.Call(ctx, "setWebhook", p, nil)
}

// DeleteWebhook — выключить вебхук (нужно для getUpdates).
func (c *Client) DeleteWebhook(ctx context.Context) error {
	return c.Call(ctx, "deleteWebhook", map[string]bool{"drop_pending_updates": false}, nil)
}

// Command — команда в меню бота.
type Command struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// SetMyCommands — меню команд бота.
func (c *Client) SetMyCommands(ctx context.Context, cmds []Command) error {
	return c.Call(ctx, "setMyCommands", map[string]any{"commands": cmds}, nil)
}

// GetUpdates — обновления длинным опросом (если вебхук не используется). Возвращает сырые обновления.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]json.RawMessage, error) {
	var out []json.RawMessage
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": timeoutSec, "allowed_updates": AllowedUpdates,
	}, &out, time.Duration(timeoutSec)*time.Second+c.timeout)
	return out, err
}

// AllowedUpdates — какие обновления нужны боту.
var AllowedUpdates = []string{"message", "callback_query", "my_chat_member"} // my_chat_member — и личные чаты, и каналы/группы, куда добавили бота

// InlineButton — кнопка под сообщением.
type InlineButton struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

// Markup — inline-клавиатура.
type Markup struct {
	InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
}

type sendParams struct {
	ChatID                int64   `json:"chat_id"`
	MessageID             int64   `json:"message_id,omitempty"`
	Text                  string  `json:"text"`
	ParseMode             string  `json:"parse_mode"`
	DisableWebPagePreview bool    `json:"disable_web_page_preview"`
	ReplyMarkup           *Markup `json:"reply_markup,omitempty"`
}

// SendMessage — сообщение в чат (HTML).
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, m *Markup) error {
	return c.Call(ctx, "sendMessage", sendParams{ChatID: chatID, Text: text, ParseMode: "HTML", DisableWebPagePreview: true, ReplyMarkup: m}, nil)
}

// EditMessageText — заменить текст и кнопки сообщения.
func (c *Client) EditMessageText(ctx context.Context, chatID, messageID int64, text string, m *Markup) error {
	if m == nil {
		m = &Markup{InlineKeyboard: [][]InlineButton{}} // убрать кнопки
	}
	return c.Call(ctx, "editMessageText", sendParams{ChatID: chatID, MessageID: messageID, Text: text, ParseMode: "HTML", DisableWebPagePreview: true, ReplyMarkup: m}, nil)
}

// AnswerCallbackQuery — ответ на нажатие кнопки (короткая подсказка).
func (c *Client) AnswerCallbackQuery(ctx context.Context, id, text string) error {
	return c.Call(ctx, "answerCallbackQuery", map[string]string{"callback_query_id": id, "text": text}, nil)
}

// ===== каналы и группы (подтверждение соцсетей) =====

// Chat — канал или группа (getChat).
type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"` // channel | group | supergroup | private
	Title    string `json:"title"`
	Username string `json:"username"`
}

// ChatMember — участник чата (getChatAdministrators).
type ChatMember struct {
	Status string `json:"status"` // creator | administrator | …
	User   User   `json:"user"`
}

// GetChat — сведения о чате.
func (c *Client) GetChat(ctx context.Context, chatID int64) (Chat, error) {
	var ch Chat
	err := c.Call(ctx, "getChat", map[string]int64{"chat_id": chatID}, &ch)
	return ch, err
}

// GetChatAdministrators — админы чата (бот должен быть в чате).
func (c *Client) GetChatAdministrators(ctx context.Context, chatID int64) ([]ChatMember, error) {
	var out []ChatMember
	err := c.Call(ctx, "getChatAdministrators", map[string]int64{"chat_id": chatID}, &out)
	return out, err
}

// GetChatMemberCount — подписчиков канала или участников группы.
func (c *Client) GetChatMemberCount(ctx context.Context, chatID int64) (int64, error) {
	var n int64
	err := c.Call(ctx, "getChatMemberCount", map[string]int64{"chat_id": chatID}, &n)
	return n, err
}

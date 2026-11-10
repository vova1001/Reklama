package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// SendError — сервис отправки ответил ошибкой. Permanent — повтор не поможет (4xx кроме 429, SMTP 5xx).
type SendError struct {
	Via        string
	Status     int
	Detail     string
	RetryAfter time.Duration // из заголовка Retry-After (429)
}

func (e *SendError) Error() string { return fmt.Sprintf("%s: %d: %s", e.Via, e.Status, e.Detail) }

// Permanent — ошибку не стоит повторять.
func (e *SendError) Permanent() bool {
	if e.Via == "smtp" {
		return e.Status >= 500
	}
	return e.Status >= 400 && e.Status < 500 && e.Status != http.StatusTooManyRequests
}

// retryAfter — Retry-After в секундах или дата HTTP; не разобрано — 0.
func retryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// ===== Resend (HTTPS API) =====

// Resend — письма через HTTP API Resend (https://resend.com/docs/api-reference/emails/send-email).
type Resend struct {
	APIKey  string
	From    string
	BaseURL string       // для тестов; по умолчанию https://api.resend.com
	Client  *http.Client // по умолчанию — с таймаутом 10 с
}

// Name — для журналов.
func (Resend) Name() string { return "resend" }

// Send отправляет письмо.
func (r Resend) Send(ctx context.Context, m Message) error {
	base := r.BaseURL
	if base == "" {
		base = "https://api.resend.com"
	}
	payload, err := json.Marshal(map[string]any{"from": r.From, "to": []string{m.To}, "subject": m.Subject, "html": m.HTML, "text": m.Text})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/emails", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")
	c := r.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return &SendError{Via: "resend", Status: res.StatusCode, Detail: strings.TrimSpace(string(b)),
			RetryAfter: retryAfter(res.Header.Get("Retry-After"))}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	return nil
}

// ===== SMTP (временно — ящик Яндекса или Mail.ru) =====

// SMTP — письма через SMTP-сервер: порт 465 — сразу TLS, иначе STARTTLS (обязательно).
type SMTP struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
}

// Name — для журналов.
func (SMTP) Name() string { return "smtp" }

// Send отправляет письмо; весь обмен укладывается в дедлайн ctx.
func (s SMTP) Send(ctx context.Context, m Message) error {
	addr := net.JoinHostPort(s.Host, s.Port)
	d := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	tlsCfg := &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	if s.Port == "465" {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp: подключение: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp: %w", err)
	}
	defer c.Close()
	if s.Port != "465" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp: сервер не поддерживает STARTTLS")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp: STARTTLS: %w", err)
		}
	}
	if s.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
			return fmt.Errorf("smtp: вход: %w", err)
		}
	}
	if err := c.Mail(envelope(s.From)); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(m.To); err != nil {
		var te *textproto.Error
		if errors.As(err, &te) && te.Code >= 500 {
			return &SendError{Via: "smtp", Status: te.Code, Detail: "RCPT TO: " + te.Msg} // адрес отвергнут — повтор не поможет
		}
		return fmt.Errorf("smtp: RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}
	if _, err := w.Write(mimeMessage(s.From, m)); err != nil {
		return fmt.Errorf("smtp: запись: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: отправка: %w", err)
	}
	return c.Quit()
}

// envelope — адрес из «Имя <addr>».
func envelope(from string) string {
	if i, j := strings.LastIndexByte(from, '<'), strings.LastIndexByte(from, '>'); i >= 0 && j > i {
		return from[i+1 : j]
	}
	return strings.TrimSpace(from)
}

// mimeMessage — письмо multipart/alternative: текст и HTML в UTF-8.
func mimeMessage(from string, m Message) []byte {
	var rb [12]byte
	_, _ = rand.Read(rb[:])
	boundary := "ohvat-" + hex.EncodeToString(rb[:])
	fromHdr := from
	if i := strings.LastIndexByte(from, '<'); i > 0 {
		fromHdr = mime.QEncoding.Encode("utf-8", strings.TrimSpace(from[:i])) + " " + from[i:]
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\n",
		fromHdr, m.To, mime.QEncoding.Encode("utf-8", m.Subject), time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", boundary, m.Text)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", boundary, m.HTML)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes()
}

// ===== журнал (только режим разработки) =====

// Log — вместо письма код пишется в журнал сервера. Включается только при APP_ENV=development.
type Log struct{ Logger *slog.Logger }

// Name — для журналов.
func (Log) Name() string { return "log" }

// Send пишет письмо в журнал.
func (l Log) Send(_ context.Context, m Message) error {
	lg := l.Logger
	if lg == nil {
		lg = slog.Default()
	}
	lg.Info("почта (режим разработки): письмо не отправляется", "to", m.To, "subject", m.Subject, "text", m.Text)
	return nil
}

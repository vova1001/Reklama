package service

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// ===== уведомления пользователю (реализация — бот Telegram, см. bot_notify.go) =====

// NotificationKind — тип уведомления.
type NotificationKind string

// Типы уведомлений. Сейчас отправляется «вход с нового устройства»; задания, отклики и выплаты — готовы
// к подключению, когда появятся эти модули.
const (
	NotifyNewLogin    NotificationKind = "login_new_device"
	NotifyNewTask     NotificationKind = "task_new"
	NotifyNewResponse NotificationKind = "response_new"
	NotifyPayout      NotificationKind = "payout"
)

// Статусы выплаты.
const (
	PayoutSent   = "sent"   // отправлена
	PayoutDone   = "done"   // зачислена
	PayoutFailed = "failed" // не прошла
)

// Notification — что сообщить пользователю. Заполняются поля своего типа.
type Notification struct {
	Kind NotificationKind
	At   time.Time

	// вход
	Method string // email | telegram | google
	Device string // «Chrome, Windows»
	IP     string // уже замаскированный адрес

	// задание и отклик
	Title string // название задания
	Topic string // тема задания
	From  string // кто откликнулся
	Path  string // адрес на сайте: /tasks/12

	// выплата
	AmountKopecks int64
	Status        string
}

// Notifier — доставка уведомлений пользователю сайта. Нет канала (не привязан Telegram, уведомления выключены) —
// не ошибка: nil. Реализация не ждёт доставки — ставит в очередь.
type Notifier interface {
	Notify(ctx context.Context, userID int64, n Notification) error
}

// ===== кто делает запрос (транспорт кладёт в контекст) =====

// ClientInfo — сведения о клиенте запроса: адрес, браузер, id устройства из httpOnly-cookie.
type ClientInfo struct {
	IP        string
	UserAgent string
	DeviceID  string
}

type clientKey struct{}

// WithClient — контекст запроса со сведениями о клиенте (их читает вход: устройство и уведомление о входе).
func WithClient(ctx context.Context, ci ClientInfo) context.Context {
	return context.WithValue(ctx, clientKey{}, ci)
}

func clientFrom(ctx context.Context) ClientInfo {
	ci, _ := ctx.Value(clientKey{}).(ClientInfo)
	return ci
}

// DescribeUA — браузер и система из User-Agent коротко: «Chrome, Windows». Неизвестно — «неизвестное устройство».
func DescribeUA(ua string) string {
	browsers := []struct{ key, name string }{
		{"YaBrowser/", "Яндекс Браузер"}, {"Edg/", "Edge"}, {"OPR/", "Opera"}, {"Firefox/", "Firefox"},
		{"SamsungBrowser/", "Samsung Internet"}, {"CriOS/", "Chrome"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"},
	}
	systems := []struct{ key, name string }{
		{"Android", "Android"}, {"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Windows", "Windows"},
		{"Mac OS X", "macOS"}, {"CrOS", "ChromeOS"}, {"Linux", "Linux"},
	}
	var b, s string
	for _, x := range browsers {
		if strings.Contains(ua, x.key) {
			b = x.name
			break
		}
	}
	for _, x := range systems {
		if strings.Contains(ua, x.key) {
			s = x.name
			break
		}
	}
	switch {
	case b != "" && s != "":
		return b + ", " + s
	case b != "":
		return b
	case s != "":
		return s
	}
	return "неизвестное устройство"
}

// MaskIP — адрес без последней части: 203.0.113.x, 2001:db8:85a3::x.
func MaskIP(ip string) string {
	p := net.ParseIP(ip)
	switch {
	case p == nil:
		return ""
	case p.To4() != nil:
		v4 := p.To4()
		return fmt.Sprintf("%d.%d.%d.x", v4[0], v4[1], v4[2])
	}
	b := p.To16()
	return fmt.Sprintf("%x:%x:%x::x", uint16(b[0])<<8|uint16(b[1]), uint16(b[2])<<8|uint16(b[3]), uint16(b[4])<<8|uint16(b[5]))
}

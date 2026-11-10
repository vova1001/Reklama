package service

import (
	"html"
	"strconv"
	"strings"
	"time"

	"ohvat/backend/internal/domain"
)

// msk — московское время для сообщений (без tzdata в образе: смещение фиксированное, перехода на летнее нет).
var msk = time.FixedZone("МСК", 3*60*60)

// botTexts — тексты бота (HTML-разметка Telegram: всё пользовательское экранируется).
type botTexts struct {
	site  string // адрес сайта; "" — кнопки-ссылки не показываем (не https)
	brand string
}

func newBotTexts(siteURL string) botTexts {
	t := botTexts{brand: "Охват"}
	if strings.HasPrefix(siteURL, "https://") {
		t.site = strings.TrimRight(siteURL, "/")
	}
	return t
}

func (t botTexts) link(text, path string) []BotButton {
	if t.site == "" {
		return nil
	}
	return []BotButton{{Text: text, URL: t.site + path}}
}

func rows(r ...[]BotButton) [][]BotButton {
	out := make([][]BotButton, 0, len(r))
	for _, x := range r {
		if len(x) > 0 {
			out = append(out, x)
		}
	}
	return out
}

func (t botTexts) welcome(chat int64, p domain.TelegramProfile, reEnabled bool) BotMessage {
	hello := "Здравствуйте!"
	if n := strings.TrimSpace(p.FirstName); n != "" {
		hello = "Здравствуйте, " + html.EscapeString(n) + "!"
	}
	var b strings.Builder
	b.WriteString(hello + "\n\nЭто бот <b>" + t.brand + "</b> — площадки, где бизнес находит авторов для рекламы.\n\n")
	b.WriteString("Через бота можно войти на сайт без пароля и получать уведомления: о входе в аккаунт, новых заданиях, откликах и выплатах.")
	if reEnabled {
		b.WriteString("\n\nУведомления снова включены.")
	}
	b.WriteString("\n\n/profile — мой аккаунт\n/help — помощь\n/stop — выключить уведомления")
	return BotMessage{ChatID: chat, Text: b.String(), Buttons: rows(append(t.link("Открыть сайт", "/"), t.link("Войти", "/login")...))}
}

func (t botTexts) help(chat int64) BotMessage {
	return BotMessage{ChatID: chat, Text: "<b>Как войти на сайт</b>\nНа странице входа нажмите «Войти через Telegram» — откроется этот бот. " +
		"Нажмите «Подтвердить вход», и сайт откроется сам.\n\n<b>Команды</b>\n/start — начало и включить уведомления\n" +
		"/profile — мой аккаунт\n/stop — выключить уведомления\n/help — эта справка", Buttons: rows(t.link("Войти на сайт", "/login"))}
}

func (t botTexts) unknown(chat int64) BotMessage {
	return BotMessage{ChatID: chat, Text: "Не знаю такой команды. Список команд — /help."}
}

func (t botTexts) stopped(chat int64) BotMessage {
	return BotMessage{ChatID: chat, Text: "Уведомления выключены. Включить снова — /start."}
}

func (t botTexts) loginConfirm(chat int64, token string, until time.Time) BotMessage {
	return BotMessage{ChatID: chat,
		Text: "<b>Вход на сайт " + t.brand + "</b>\n\nПодтвердите, что это вы входите на сайт. Кнопка действует до " +
			until.In(msk).Format("15:04") + " (МСК).\n\nЕсли вы не нажимали «Войти через Telegram» — нажмите «Это не я».",
		Buttons: [][]BotButton{{{Text: "Подтвердить вход", Data: cbConfirm + token}}, {{Text: "Это не я", Data: cbCancel + token}}}}
}

func (t botTexts) loginConfirmed(chat, msg int64) BotMessage {
	return BotMessage{ChatID: chat, EditMessageID: msg, Text: "<b>Вход подтверждён</b>\n\nВернитесь на сайт — приложение откроется само."}
}

func (t botTexts) loginCancelled(chat, msg int64) BotMessage {
	return BotMessage{ChatID: chat, EditMessageID: msg, Text: "Вход отменён — доступ к аккаунту никто не получил."}
}

func (t botTexts) loginExpired(chat, msg int64) BotMessage {
	return BotMessage{ChatID: chat, EditMessageID: msg,
		Text:    "Ссылка для входа устарела или уже использована. Вернитесь на сайт и нажмите «Войти через Telegram» ещё раз.",
		Buttons: rows(t.link("Войти на сайт", "/login"))}
}

func (t botTexts) loggedOutAll(chat, msg int64) BotMessage {
	return BotMessage{ChatID: chat, EditMessageID: msg,
		Text: "<b>Все сеансы завершены</b>\n\nНа всех устройствах нужно войти заново.", Buttons: rows(t.link("Войти на сайт", "/login"))}
}

func (t botTexts) noProfile(chat int64) BotMessage {
	return BotMessage{ChatID: chat, Text: "Вы ещё не входили на сайт через Telegram. Откройте страницу входа и нажмите «Войти через Telegram».",
		Buttons: rows(t.link("Войти на сайт", "/login"))}
}

var modeTitle = map[string]string{domain.ModeBusiness: "бизнес", domain.ModeCreator: "автор"}

func (t botTexts) profile(chat int64, u domain.User, notify bool) BotMessage {
	name := strings.TrimSpace(u.Name)
	if name == "" {
		name = "Без имени"
	}
	mode := modeTitle[u.ActiveMode]
	if mode == "" {
		mode = modeTitle[domain.ModeBusiness]
	}
	var b strings.Builder
	b.WriteString("<b>" + html.EscapeString(name) + "</b>\nРежим: " + mode)
	if u.City != "" {
		b.WriteString("\nГород: " + html.EscapeString(u.City))
	}
	if u.Email != "" {
		b.WriteString("\nПочта: " + html.EscapeString(MaskEmail(u.Email)))
	}
	if notify {
		b.WriteString("\nУведомления: включены (выключить — /stop)")
	} else {
		b.WriteString("\nУведомления: выключены (включить — /start)")
	}
	return BotMessage{ChatID: chat, Text: b.String(), Buttons: rows(t.link("Открыть приложение", "/app"))}
}

// ===== уведомления =====

var methodTitle = map[string]string{domain.ProviderEmail: "код на почту", domain.ProviderGoogle: "Google", domain.ProviderTelegram: "Telegram"}

func (t botTexts) notification(chat, userID int64, n Notification) (BotMessage, bool) {
	at := n.At
	if at.IsZero() {
		at = time.Now()
	}
	when := at.In(msk).Format("02.01.2006 15:04") + " МСК"
	esc := html.EscapeString
	switch n.Kind {
	case NotifyNewLogin:
		var b strings.Builder
		b.WriteString("<b>Вход в аккаунт с нового устройства</b>\n\n")
		if m := methodTitle[n.Method]; m != "" {
			b.WriteString("Способ: " + m + "\n")
		}
		if n.Device != "" {
			b.WriteString("Устройство: " + esc(n.Device) + "\n")
		}
		if n.IP != "" {
			b.WriteString("Адрес: " + esc(n.IP) + "\n")
		}
		b.WriteString("Время: " + when + "\n\nЕсли это были не вы — завершите все сеансы.")
		return BotMessage{ChatID: chat, Text: b.String(), Buttons: rows(
			[]BotButton{{Text: "Это не я — выйти везде", Data: cbLogoutAll + strconv.FormatInt(userID, 10)}},
			t.link("Открыть приложение", "/app"))}, true
	case NotifyNewTask:
		head := "<b>Новое задание</b>"
		if n.Topic != "" {
			head = "<b>Новое задание по теме «" + esc(n.Topic) + "»</b>"
		}
		return BotMessage{ChatID: chat, Text: head + "\n\n" + esc(n.Title), Buttons: rows(t.link("Открыть задание", pathOr(n.Path, "/app")))}, n.Title != ""
	case NotifyNewResponse:
		text := "<b>Новый отклик</b> на задание «" + esc(n.Title) + "»"
		if n.From != "" {
			text += "\nОт: " + esc(n.From)
		}
		return BotMessage{ChatID: chat, Text: text, Buttons: rows(t.link("Посмотреть отклик", pathOr(n.Path, "/app")))}, n.Title != ""
	case NotifyPayout:
		status := map[string]string{PayoutSent: "отправлена", PayoutDone: "зачислена", PayoutFailed: "не прошла — проверьте реквизиты в кабинете"}[n.Status]
		if status == "" || n.AmountKopecks <= 0 {
			return BotMessage{}, false
		}
		return BotMessage{ChatID: chat, Text: "<b>Выплата " + FormatRub(n.AmountKopecks) + "</b> " + status + ".",
			Buttons: rows(t.link("Открыть приложение", pathOr(n.Path, "/app")))}, true
	}
	return BotMessage{}, false
}

func pathOr(p, def string) string {
	if strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") {
		return p
	}
	return def
}

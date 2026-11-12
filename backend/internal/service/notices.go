package service

import (
	"context"
	"errors"
	"log/slog"

	"ohvat/backend/internal/domain"
)

// ===== уведомление владельцу о решении модератора: почта + Telegram =====

// DecisionMail — письмо о решении (шаблон — в gateway/mail).
type DecisionMail struct {
	Kind   domain.ReviewKind
	Title  string
	Action domain.ReviewAction
	Reason string
	Notes  []FieldNote // замечания по полям — по порядку полей
	URL    string      // полный адрес на сайте
}

// FieldNote — замечание к полю с понятным названием поля.
type FieldNote struct {
	Field string
	Note  string
}

// DecisionMailer — отправка письма о решении (очередь писем).
type DecisionMailer interface {
	SendDecision(ctx context.Context, to string, m DecisionMail) error
}

// UserLookup — пользователь по id (почта для письма).
type UserLookup interface {
	UserByID(ctx context.Context, id int64) (domain.User, error)
}

// FieldTitles — названия полей для замечаний «на доработку» (письмо, бот, админка).
var FieldTitles = map[string]string{
	"general": "Общее", "title": "Название", "categories": "Сфера", "city": "Город", "formats": "Формат",
	"minViews": "Просмотры", "reward": "Оплата", "publishBy": "Срок", "blocks": "Описание", "cover": "Обложка",
	"photos": "Фото", "name": "Имя", "topics": "Темы", "platforms": "Соцсети", "avatar": "Фото профиля", "works": "Работы",
	"spheres": "Сферы", "logo": "Логотип", "slug": "Адрес страницы",
}

var fieldOrder = []string{"general", "title", "name", "slug", "categories", "topics", "spheres", "city", "formats", "platforms", "minViews",
	"reward", "publishBy", "logo", "blocks", "cover", "avatar", "photos", "works"}

// OrderedNotes — замечания по порядку полей формы.
func OrderedNotes(n map[string]string) []FieldNote {
	out := make([]FieldNote, 0, len(n))
	for _, f := range fieldOrder {
		if v, ok := n[f]; ok {
			out = append(out, FieldNote{Field: FieldTitles[f], Note: v})
		}
	}
	return out
}

// OwnerNotices — уведомления владельцу о решении: письмо (если есть подтверждённая почта) и сообщение бота
// (если Telegram привязан). Каналы независимы: сбой одного не мешает другому.
type OwnerNotices struct {
	Users   UserLookup
	Mail    DecisionMailer // nil — почта выключена
	Bot     Notifier       // nil — бот выключен
	SiteURL string
	Log     *slog.Logger
}

var _ DecisionNotifier = (*OwnerNotices)(nil)

// NotifyDecision ставит письмо и сообщение в очереди.
func (o *OwnerNotices) NotifyDecision(ctx context.Context, ownerID int64, n DecisionNotice) error {
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	var errs []error
	if o.Mail != nil && o.Users != nil {
		u, err := o.Users.UserByID(ctx, ownerID)
		switch {
		case err != nil:
			errs = append(errs, err)
		case u.Email != "":
			m := DecisionMail{Kind: n.Kind, Title: n.Title, Action: n.Action, Reason: n.Reason, Notes: OrderedNotes(n.Notes), URL: o.SiteURL + n.Path}
			if err := o.Mail.SendDecision(ctx, u.Email, m); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if o.Bot != nil {
		x := Notification{Kind: NotifyModeration, Title: n.Title, Status: string(n.Action), Path: n.Path, Reason: n.Reason,
			Notes: OrderedNotes(n.Notes), Subject: string(n.Kind)}
		if err := o.Bot.Notify(ctx, ownerID, x); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		log.InfoContext(ctx, "решение: уведомления поставлены в очередь", "user", ownerID, "action", n.Action)
	}
	return errors.Join(errs...)
}

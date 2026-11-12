package service

import (
	"context"
	"errors"
	"strings"

	"ohvat/backend/internal/domain"
)

// ===== ошибки админки (транспорт переводит их в ответы) =====

var (
	// ErrNotStaff — не сотрудник (или заблокирован): админки «нет» — 404.
	ErrNotStaff = errors.New("нет доступа к админке")
	// ErrForbidden — у роли нет права на действие.
	ErrForbidden = errors.New("недостаточно прав")
	// ErrTwoFactorSetup — 2FA не настроена: доступна только страница настройки.
	ErrTwoFactorSetup = errors.New("нужно настроить 2FA")
	// ErrTwoFactorRequired — 2FA не подтверждена в этой сессии или подтверждена больше 12 часов назад.
	ErrTwoFactorRequired = errors.New("нужно подтвердить 2FA")
	// ErrTwoFactorInvalid — неверный код 2FA.
	ErrTwoFactorInvalid = errors.New("неверный код 2FA")
	// ErrLeaseHeld — элемент в работе у другого модератора.
	ErrLeaseHeld = errors.New("в работе у другого модератора")
	// ErrLeaseLost — аренда истекла: элемент надо взять заново.
	ErrLeaseLost = errors.New("аренда истекла")
	// ErrUndoExpired — отменить решение уже нельзя (прошло 10 с или применено).
	ErrUndoExpired = errors.New("решение уже применено")
	// ErrLastAdmin — нельзя оставить сайт без администратора.
	ErrLastAdmin = errors.New("последний администратор")
	// ErrSelfAction — действие над собой (заблокировать себя, сменить себе роль) запрещено.
	ErrSelfAction = errors.New("нельзя над собой")
	// ErrBlocked — аккаунт заблокирован: вход закрыт.
	ErrBlocked = errors.New("аккаунт заблокирован")
)

// authorize — сотрудник с правом p; иначе ErrNotStaff (не сотрудник, заблокирован) или ErrForbidden.
// Вызывается в каждом методе сервисов админки — транспорт проверяет только вход и 2FA.
func authorize(u domain.User, p domain.Permission) error {
	if u.ID == 0 || !u.Staff() || u.Blocked() {
		return ErrNotStaff
	}
	if !u.StaffRole.Can(p) {
		return ErrForbidden
	}
	return nil
}

// Authorize — то же для транспорта (страницы админки, отдача фото на проверке).
func Authorize(u domain.User, p domain.Permission) error { return authorize(u, p) }

// auditFor — запись журнала от имени сотрудника: кто (id, почта или имя, роль), откуда (адрес, браузер).
func auditFor(ctx context.Context, u domain.User, action, subject, subjectID string) domain.AuditEntry {
	ci := clientFrom(ctx)
	label := u.Email
	if label == "" {
		label = u.Name
	}
	return domain.AuditEntry{
		ActorID: u.ID, ActorLabel: clip(label, 200), ActorRole: u.StaffRole, Action: action, Subject: subject,
		SubjectID: subjectID, IP: ci.IP, UserAgent: clip(ci.UserAgent, 300),
	}
}

// ParseAdminEmails — ADMIN_EMAILS: почты через запятую (пробелы и регистр не важны, пустые — пропускаются).
func ParseAdminEmails(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if e := strings.ToLower(strings.TrimSpace(p)); e != "" && strings.Contains(e, "@") {
			out = append(out, e)
		}
	}
	return out
}

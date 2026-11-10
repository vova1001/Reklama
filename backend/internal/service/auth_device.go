package service

import (
	"context"

	"ohvat/backend/internal/domain"
)

// DeviceRepository — устройства, с которых входил пользователь (хеш id из cookie).
type DeviceRepository interface {
	// TouchDevice отмечает устройство; true — увидели впервые.
	TouchDevice(ctx context.Context, userID int64, deviceHash []byte) (bool, error)
}

// afterLogin — запомнить устройство и, если вход с нового устройства, сообщить пользователю.
// Не мешает входу: ошибки только в журнал. Первый вход нового пользователя и вход через Telegram
// (человек и так в чате с ботом) уведомления не дают.
func (s *Auth) afterLogin(ctx context.Context, u domain.User, id domain.Identity, created bool) {
	ci := clientFrom(ctx)
	newDevice := true
	if s.devices != nil && ci.DeviceID != "" {
		first, err := s.devices.TouchDevice(ctx, u.ID, hashToken(ci.DeviceID))
		if err != nil {
			s.log.WarnContext(ctx, "устройство не отмечено", "user", u.ID, "err", err)
			return
		}
		newDevice = first
	}
	if created || !newDevice || s.notifier == nil || id.Provider == domain.ProviderTelegram {
		return
	}
	n := Notification{Kind: NotifyNewLogin, At: s.clock.Now(), Method: id.Provider, Device: DescribeUA(ci.UserAgent), IP: MaskIP(ci.IP)}
	if err := s.notifier.Notify(ctx, u.ID, n); err != nil {
		s.log.WarnContext(ctx, "уведомление о входе не отправлено", "user", u.ID, "err", err)
	}
}

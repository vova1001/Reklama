// Package qrcode — QR-код ссылки otpauth:// для настройки 2FA (реализует service.QRCoder). Картинка PNG строится
// на сервере: секрет не уходит сторонним сервисам.
package qrcode

import (
	"fmt"

	"rsc.io/qr"
)

// PNG — QR-код с уровнем коррекции M (масштаб 6 px на модуль, с полем).
type PNG struct{}

// PNG кодирует текст в картинку PNG.
func (PNG) PNG(text string) ([]byte, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, fmt.Errorf("QR-код: %w", err)
	}
	c.Scale = 6
	return c.PNG(), nil
}

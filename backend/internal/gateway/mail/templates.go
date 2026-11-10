package mail

import (
	"fmt"
	"html"
)

// LoginCodeMessage — письмо с кодом входа в цветах сайта: крупный код, срок, что делать, если это не вы.
func LoginCodeMessage(to, code string) Message {
	subject := fmt.Sprintf("Код для входа в Охват: %s", code)
	text := fmt.Sprintf("Ваш код для входа в Охват: %s\n\nКод действует 10 минут. Если вы не входили — просто удалите письмо.", code)
	body := fmt.Sprintf(`<!doctype html><html lang="ru"><body style="margin:0;padding:32px 16px;background:#e9e7e1;font-family:Inter,Arial,sans-serif;color:#1b1b1a">
<div style="max-width:480px;margin:0 auto;background:#ffffff;border-radius:28px;padding:36px 32px">
<p style="margin:0 0 24px;font-size:24px;font-weight:700;letter-spacing:-0.04em">Охват</p>
<p style="margin:0 0 12px;font-size:18px;line-height:1.45">Ваш код для входа:</p>
<p style="margin:0 0 24px;font-size:44px;font-weight:700;letter-spacing:0.18em;color:#0f7553">%s</p>
<p style="margin:0;font-size:16px;line-height:1.45;color:#605f5a">Код действует 10 минут. Если вы не входили в Охват — просто удалите письмо.</p>
</div></body></html>`, html.EscapeString(code))
	return Message{To: to, Subject: subject, Text: text, HTML: body}
}

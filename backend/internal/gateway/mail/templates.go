package mail

import (
	"fmt"
	"html"
	"strings"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
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

// DecisionMessage — письмо о решении модератора: что решено, причина или замечания по полям, ссылка.
func DecisionMessage(to string, m service.DecisionMail) Message {
	head := service.ModerationHead(m.Kind, m.Action)
	if head == "" {
		head = "Решение по проверке"
	}
	var txt, rows strings.Builder
	txt.WriteString(head + "\n")
	if m.Title != "" {
		txt.WriteString("«" + m.Title + "»\n")
	}
	esc := html.EscapeString
	switch m.Action {
	case domain.ActionReject:
		if m.Reason != "" {
			txt.WriteString("\nПричина: " + m.Reason + "\n")
			rows.WriteString(`<p style="margin:0 0 16px;font-size:16px;line-height:1.5"><b>Причина:</b> ` + esc(m.Reason) + `</p>`)
		}
	case domain.ActionRework:
		txt.WriteString("\nЧто поправить:\n")
		rows.WriteString(`<p style="margin:0 0 8px;font-size:16px;font-weight:600">Что поправить:</p><ul style="margin:0 0 16px;padding-left:20px;font-size:16px;line-height:1.5">`)
		for _, n := range m.Notes {
			txt.WriteString("• " + n.Field + ": " + n.Note + "\n")
			rows.WriteString(`<li><b>` + esc(n.Field) + `:</b> ` + esc(n.Note) + `</li>`)
		}
		rows.WriteString(`</ul>`)
	}
	btn := "Открыть"
	if m.Action != domain.ActionApprove {
		btn = "Исправить"
	}
	txt.WriteString("\n" + btn + ": " + m.URL + "\n")
	title := ""
	if m.Title != "" {
		title = `<p style="margin:0 0 16px;font-size:17px;color:#636361">«` + esc(m.Title) + `»</p>`
	}
	body := fmt.Sprintf(`<!doctype html><html lang="ru"><body style="margin:0;padding:32px 16px;background:#f4f4f3;font-family:Inter,Arial,sans-serif;color:#161616">
<div style="max-width:520px;margin:0 auto;background:#ffffff;border-radius:20px;padding:32px 28px">
<p style="margin:0 0 20px;font-size:22px;font-weight:700;letter-spacing:-0.03em">Охват</p>
<p style="margin:0 0 8px;font-size:20px;font-weight:650">%s</p>%s%s
<p style="margin:8px 0 0"><a href="%s" style="display:inline-block;padding:12px 22px;border-radius:999px;background:#0f7553;color:#ffffff;text-decoration:none;font-weight:600">%s</a></p>
</div></body></html>`, esc(head), title, rows.String(), esc(m.URL), esc(btn))
	return Message{To: to, Subject: head + " — Охват", Text: txt.String(), HTML: body}
}

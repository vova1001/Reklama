package domain

import "time"

// FormDraft — состояние формы редактора (задание, анкета) как есть: поля, блоки, порядок, выбранное, фото-ссылки.
// Нужно, чтобы введённое не терялось при обновлении страницы, даже если оно ещё не проходит проверку.
type FormDraft struct {
	Form    string // task-new | task-<id> | card
	Data    []byte // JSON-объект от формы
	SavedAt time.Time
}

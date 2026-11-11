package domain

// Format — формат публикации: из списка (ID — slug, Builtin) или свой (ID = Title — название, как ввёл бизнес
// после нормализации).
type Format struct {
	ID      string
	Title   string
	Builtin bool
	Own     bool // свой, уже использованный бизнесом в его заданиях
}

// BuiltinFormats — форматы списка (пилюли формы), порядок — как в форме.
var BuiltinFormats = []Format{
	{ID: "reels", Title: "Reels", Builtin: true},
	{ID: "shorts", Title: "Shorts", Builtin: true},
	{ID: "youtube", Title: "YouTube", Builtin: true},
	{ID: "vk_clips", Title: "VK Клипы", Builtin: true},
	{ID: "tiktok", Title: "TikTok", Builtin: true},
	{ID: "telegram", Title: "Telegram", Builtin: true},
}

// FormatHints — известные форматы вне пилюль: подсказки при вводе своего (выбранная становится своим форматом
// с этим написанием).
var FormatHints = []string{
	"Stories", "Пост", "Карусель", "Прямой эфир", "Обзор", "Распаковка", "Интеграция в видео", "Отзыв",
	"Подкаст", "Дзен", "Rutube", "VK Видео", "Likee", "Threads",
}

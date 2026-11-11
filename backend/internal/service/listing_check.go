package service

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// Проверки содержимого заданий и анкет. Два уровня: «форма» — для любого сохранения (длины, числа, списки, адреса;
// черновик может быть неполным) и «полнота» — при отправке на проверку (обязательные поля заполнены).

// Пределы — одни для заданий и анкет.
const (
	maxBlocks      = 8     // динамических блоков
	maxBlockItems  = 15    // пунктов (абзацев, ссылок) в блоке
	maxBlockTitle  = 60    // символов в заголовке блока
	maxParagraph   = 1000  // символов в абзаце
	maxListItem    = 200   // символов в пункте списка
	maxLink        = 300   // символов в ссылке
	maxBlocksText  = 6000  // символов во всех блоках вместе
	maxPlatforms   = 6     // площадок в анкете
	maxCount       = 1e9   // просмотров, лайков, подписчиков
	maxRewardKop   = 1e8   // оплата — до 1 000 000 ₽
	minRewardKop   = 10000 // от 100 ₽
	maxDeadlineDay = 180   // срок публикации — до полугода
)

// Formats — форматы публикации задания (порядок — как в форме).
var Formats = []string{"reels", "shorts", "youtube", "vk_clips", "tiktok", "telegram"}

// msk — дата «сегодня» для сроков считается по Москве (UTC+3, без перехода на летнее время).

// checkBlocks — динамические блоки: вид, заголовок, пункты. Пустые пункты и пустые блоки отбрасываются;
// ошибка блока — в поле "blocks.<номер во входном списке>", общая — в "blocks".
func checkBlocks(ve *ValidationError, in []domain.Block) []domain.Block {
	if len(in) > maxBlocks {
		ve.add("blocks", fmt.Sprintf("Не больше %d блоков", maxBlocks))
		return nil
	}
	out := make([]domain.Block, 0, len(in))
	total := 0
	for i, b := range in {
		key := fmt.Sprintf("blocks.%d", i)
		nb := domain.Block{Kind: b.Kind, Title: squash(b.Title), Items: make([]string, 0, len(b.Items))}
		limit := 0
		switch b.Kind {
		case domain.BlockText:
			limit = maxParagraph
		case domain.BlockList:
			limit = maxListItem
		case domain.BlockLinks:
			limit = maxLink
		default:
			ve.add(key, "Неизвестный вид блока")
			continue
		}
		if utf8.RuneCountInString(nb.Title) > maxBlockTitle {
			ve.add(key, fmt.Sprintf("Заголовок блока — до %d символов", maxBlockTitle))
		}
		for _, it := range b.Items {
			it = squash(it)
			if it == "" {
				continue
			}
			if b.Kind == domain.BlockLinks {
				u, ok := normURL(it)
				if !ok {
					ve.add(key, "Ссылка — в виде https://…")
					continue
				}
				it = u
			}
			if utf8.RuneCountInString(it) > limit {
				ve.add(key, fmt.Sprintf("Пункт — до %d символов", limit))
			}
			total += utf8.RuneCountInString(it)
			nb.Items = append(nb.Items, it)
		}
		if len(nb.Items) > maxBlockItems {
			ve.add(key, fmt.Sprintf("Не больше %d пунктов в блоке", maxBlockItems))
		}
		if nb.Title == "" && len(nb.Items) == 0 {
			continue // пустой блок — не храним
		}
		out = append(out, nb)
	}
	if total > maxBlocksText {
		ve.add("blocks", fmt.Sprintf("Слишком много текста — сократите до %d знаков", maxBlocksText))
	}
	return out
}

// blocksFilled — есть хоть один блок с пунктами (заголовок не обязателен: без него блок показывается без заголовка).
func blocksFilled(bs []domain.Block) bool {
	for _, b := range bs {
		if len(b.Items) > 0 {
			return true
		}
	}
	return false
}

// checkFormats — форматы из списка, без повторов.
func checkFormats(ve *ValidationError, in []string) []string {
	out := uniq(in)
	for _, f := range out {
		if !contains(Formats, f) {
			ve.add("formats", "Выберите форматы из списка")
			break
		}
	}
	return out
}

// checkCount — число от 0 до maxCount.
func checkCount(ve *ValidationError, field string, n int64) {
	if n < 0 || n > maxCount {
		ve.add(field, "Число от 0 до 1 000 000 000")
	}
}

// maxLen — строка не длиннее n символов (для черновика; минимум проверяет полнота).
func maxLen(ve *ValidationError, field, v string, n int, msg string) {
	if utf8.RuneCountInString(v) > n {
		ve.add(field, msg)
	}
}

// parseDate — ГГГГ-ММ-ДД или пусто.
func parseDate(ve *ValidationError, field, v string) *time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	d, err := time.Parse(time.DateOnly, v)
	if err != nil {
		ve.add(field, "Дата в формате ДД.ММ.ГГГГ")
		return nil
	}
	return &d
}

// today — дата «сегодня» по Москве (00:00 UTC той же календарной даты — так хранится срок).
func today(now time.Time) time.Time {
	y, m, d := now.In(msk).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// normURL — ссылка https://… (http повышается, без схемы — дописывается); без логина в адресе, хост с точкой.
func normURL(raw string) (string, bool) {
	c := strings.TrimSpace(raw)
	if c == "" || len(c) > maxLink*4 {
		return "", false
	}
	if !strings.Contains(c, "://") {
		c = "https://" + c
	}
	u, err := url.Parse(c)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || !strings.Contains(u.Hostname(), ".") ||
		strings.ContainsAny(u.Host, " <>\"") {
		return "", false
	}
	u.Scheme = "https"
	u.Host = strings.ToLower(u.Host)
	return u.String(), true
}

// platformKinds — вид площадки по домену (и его поддоменам); иначе — "other".
var platformKinds = map[string]string{
	"t.me": "telegram", "telegram.me": "telegram",
	"youtube.com": "youtube", "youtu.be": "youtube",
	"vk.com": "vk", "vk.ru": "vk", "vkvideo.ru": "vk",
	"tiktok.com":    "tiktok",
	"instagram.com": "instagram",
	"dzen.ru":       "dzen",
	"rutube.ru":     "rutube",
}

func platformKind(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return "other"
	}
	h := strings.TrimPrefix(u.Hostname(), "www.")
	for {
		if k, ok := platformKinds[h]; ok {
			return k
		}
		i := strings.IndexByte(h, '.')
		if i < 0 {
			return "other"
		}
		h = h[i+1:]
	}
}

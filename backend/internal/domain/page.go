package domain

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ===== публичные страницы: витрина бизнеса /b/<slug> и профиль автора /c/<slug> =====

// PageKind — вид страницы: бизнес или автор (у пользователя — по одной каждого вида).
type PageKind string

// Виды страниц.
const (
	PageBusiness PageKind = "business"
	PageCreator  PageKind = "creator"
)

// ValidPageKind — вид из списка.
func ValidPageKind(k PageKind) bool { return k == PageBusiness || k == PageCreator }

// SubjectPage — страница в журнале смен статуса и у прикреплённых файлов.
const SubjectPage Subject = "page"

// Виды блоков страницы.
const (
	PBAbout    = "about"    // «О компании» / «О себе» — текст
	PBPhotos   = "photos"   // галерея фото
	PBVideo    = "video"    // видео: файл или YouTube / VK
	PBContacts = "contacts" // адрес, часы, сайт, соцсети
	PBFacts    = "facts"    // пункты
	PBText     = "text"     // заголовок + текст
	PBChannels = "channels" // подтверждённые соцсети автора (сами, из social_accounts)
	PBTopics   = "topics"   // «Что могу рекламировать» — темы
	PBWorks    = "works"    // примеры работ: фото, видео, ссылки
	PBReviews  = "reviews"  // отзывы (пока без сделок — заглушка)
	PBCard     = "card"     // анкета автора (сама, опубликованная)
)

// PageBlockTypes — какие блоки можно добавить на страницу вида k (порядок — как в списке «Добавить блок»).
func PageBlockTypes(k PageKind) []string {
	if k == PageCreator {
		return []string{PBAbout, PBChannels, PBTopics, PBWorks, PBReviews, PBCard, PBText, PBPhotos, PBVideo, PBFacts}
	}
	return []string{PBAbout, PBPhotos, PBVideo, PBContacts, PBFacts, PBText}
}

// PageBlockSingle — блок этого вида бывает на странице один.
func PageBlockSingle(t string) bool {
	switch t {
	case PBAbout, PBContacts, PBChannels, PBTopics, PBWorks, PBReviews, PBCard:
		return true
	}
	return false
}

// PageBlockAuto — блок без своего содержимого: наполняется сам (соцсети, отзывы, анкета).
func PageBlockAuto(t string) bool { return t == PBChannels || t == PBReviews || t == PBCard }

// Источники видео.
const (
	VideoFile    = "file"    // загруженный файл (MP4 / WebM), без перекодирования
	VideoYouTube = "youtube" // встраивание youtube-nocookie.com
	VideoVK      = "vk"      // встраивание vk.com/video_ext.php
)

// PageVideo — видео в блоке: файл (Media + Poster) или встраивание (ID, а у VK — OID и Hash). Адрес встраивания
// строит сервер из проверенных частей (EmbedURL) — ссылка пользователя в iframe не попадает.
type PageVideo struct {
	Source string
	Media  string // media.id файла (VideoFile)
	Poster string // media.id кадра-заставки (по желанию)
	ID     string // YouTube: id ролика; VK: id ролика
	OID    string // VK: владелец (-123 — сообщество)
	Hash   string // VK: hash доступа (по желанию)
}

// PageContacts — «Адрес и контакты».
type PageContacts struct {
	City    string
	Address string
	Hours   string
	Site    string   // https://…
	Socials []string // https://… (до 6)
	Phone   string
}

// PageBlock — блок страницы; поля — по виду блока (лишние не хранятся).
type PageBlock struct {
	Type  string
	Title string   // заголовок (text — обязателен; у остальных — свой вместо стандартного)
	Text  string   // about, text
	Items []string // facts — пункты; topics — slug тем
	// ItemTitles — названия тем блока topics по порядку (только для показа: публичная страница, проверка)
	ItemTitles []string
	Photos     []string      // photos, works — media.id фото
	Videos     []PageVideo   // video — одно; works — до 3
	Links      []string      // works — ссылки на работы
	Contacts   *PageContacts // contacts
}

// Page — публичная страница (витрина бизнеса или профиль автора).
type Page struct {
	ID           int64
	UserID       int64
	Kind         PageKind
	Slug         string
	Status       Status
	RejectReason string
	Title        string // название бизнеса или имя автора
	City         string
	Spheres      []string // slug сфер (бизнес) или тем (автор), до 5
	Logo         string   // media.id логотипа / аватара (1:1)
	Cover        string   // media.id обложки (3:1)
	Blocks       []PageBlock
	SphereTitles []string // названия сфер (на публичной странице и в проверке)
	CreatedAt    time.Time
	UpdatedAt    time.Time
	SubmittedAt  *time.Time
	PublishedAt  *time.Time
	ReviewNotes  map[string]string
	// Правка опубликованного (как у задания): новая версия ждёт, опубликованная видна всем.
	Revision            *Page
	RevisionStatus      Status
	RevisionReason      string
	RevisionSubmittedAt *time.Time
}

// PageMedia — все файлы содержимого страницы (логотип, обложка, фото и видео блоков, заставки) без повторов.
func PageMedia(p Page) []string {
	var out []string
	seen := map[string]bool{}
	add := func(ids ...string) {
		for _, id := range ids {
			if id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	add(p.Logo, p.Cover)
	for _, b := range p.Blocks {
		add(b.Photos...)
		for _, v := range b.Videos {
			add(v.Media, v.Poster)
		}
	}
	return out
}

// ===== адрес страницы =====

// ErrSlugLimit — адрес страницы меняли слишком много раз (прежние адреса хранятся и ведут на новый).
var ErrSlugLimit = errors.New("адресов страницы слишком много")

// Пределы адреса.
const (
	SlugMin = 3
	SlugMax = 40
)

var reSlugPage = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// reservedSlugs — адреса, которые нельзя занять (служебные и вводящие в заблуждение).
var reservedSlugs = map[string]bool{
	"admin": true, "app": true, "api": true, "new": true, "edit": true, "login": true, "logout": true, "settings": true,
	"ohvat": true, "support": true, "help": true, "media": true, "static": true, "moderator": true, "official": true,
	"null": true, "undefined": true, "page": true, "profile": true,
}

// ValidPageSlug — латиница в нижнем регистре, цифры, одиночные дефисы между ними, 3–40 символов, не служебный.
func ValidPageSlug(s string) bool {
	return len(s) >= SlugMin && len(s) <= SlugMax && reSlugPage.MatchString(s) && !reservedSlugs[s]
}

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i", 'й': "y",
	'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f",
	'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

// SlugFrom — адрес из названия или имени: транслитерация, нижний регистр, всё прочее — дефис, до SlugMax.
// Может вернуть недопустимый (короткий, служебный) — тогда вызывающий берёт запасной.
func SlugFrom(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case translit[r] != "" || r == 'ъ' || r == 'ь':
			b.WriteString(translit[r])
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > SlugMax {
		s = strings.TrimRight(s[:SlugMax], "-")
	}
	return s
}

// ===== встраивание видео (только белый список) =====

var (
	reYouTubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	reVKVideo   = regexp.MustCompile(`(?:video|clip)(-?[0-9]{1,19})_([0-9]{1,19})`)
	reVKOwner   = regexp.MustCompile(`^-?[0-9]{1,19}$`)
	reVKNum     = regexp.MustCompile(`^[0-9]{1,19}$`)
	reVKHash    = regexp.MustCompile(`^[0-9a-f]{1,32}$`)
)

// ParseVideoLink — ссылка на ролик YouTube или VK → источник и части для встраивания. Только https/http и только
// хосты из списка; всё прочее — ok=false.
func ParseVideoLink(raw string) (PageVideo, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 500 {
		return PageVideo{}, false
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" {
		return PageVideo{}, false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	host = strings.TrimPrefix(host, "m.")
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch host {
	case "youtube.com", "youtube-nocookie.com", "music.youtube.com":
		id := ""
		switch {
		case u.Path == "/watch":
			id = u.Query().Get("v")
		case len(parts) == 2 && (parts[0] == "embed" || parts[0] == "shorts" || parts[0] == "live" || parts[0] == "v"):
			id = parts[1]
		}
		if reYouTubeID.MatchString(id) {
			return PageVideo{Source: VideoYouTube, ID: id}, true
		}
	case "youtu.be":
		if len(parts) == 1 && reYouTubeID.MatchString(parts[0]) {
			return PageVideo{Source: VideoYouTube, ID: parts[0]}, true
		}
	case "vk.com", "vk.ru", "vkvideo.ru":
		q := u.Query()
		if u.Path == "/video_ext.php" {
			oid, id, hash := q.Get("oid"), q.Get("id"), q.Get("hash")
			if reVKOwner.MatchString(oid) && reVKNum.MatchString(id) && (hash == "" || reVKHash.MatchString(hash)) {
				return PageVideo{Source: VideoVK, OID: oid, ID: id, Hash: hash}, true
			}
			return PageVideo{}, false
		}
		for _, cand := range []string{u.Path, q.Get("z")} {
			if m := reVKVideo.FindStringSubmatch(cand); m != nil {
				return PageVideo{Source: VideoVK, OID: m[1], ID: m[2]}, true
			}
		}
	}
	return PageVideo{}, false
}

// EmbedURL — адрес для iframe из проверенных частей; "" — не встраивается (файл или неверные части).
func (v PageVideo) EmbedURL() string {
	switch v.Source {
	case VideoYouTube:
		if reYouTubeID.MatchString(v.ID) {
			return "https://www.youtube-nocookie.com/embed/" + v.ID
		}
	case VideoVK:
		if reVKOwner.MatchString(v.OID) && reVKNum.MatchString(v.ID) && (v.Hash == "" || reVKHash.MatchString(v.Hash)) {
			s := "https://vk.com/video_ext.php?oid=" + v.OID + "&id=" + v.ID
			if v.Hash != "" {
				s += "&hash=" + v.Hash
			}
			return s
		}
	}
	return ""
}

// WatchURL — ссылка на ролик на самой площадке (для «Открыть на YouTube / VK»); "" — файл.
func (v PageVideo) WatchURL() string {
	switch v.Source {
	case VideoYouTube:
		if reYouTubeID.MatchString(v.ID) {
			return "https://www.youtube.com/watch?v=" + v.ID
		}
	case VideoVK:
		if reVKOwner.MatchString(v.OID) && reVKNum.MatchString(v.ID) {
			return "https://vk.com/video" + v.OID + "_" + v.ID
		}
	}
	return ""
}

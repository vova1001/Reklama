package domain

import (
	"strconv"
	"time"
)

// Назначение фото: от него зависят пропорции и где его можно прикрепить.
const (
	PurposeAvatar     = "avatar"      // аватар профиля, 1:1
	PurposeTaskCover  = "task_cover"  // обложка задания, 16:9
	PurposeTaskPhoto  = "task_photo"  // пример «что показать»
	PurposeCardAvatar = "card_avatar" // аватар анкеты автора, 1:1
	PurposeCardWork   = "card_work"   // пример работы автора
	// Страницы бизнеса и автора (задача 14).
	PurposePageLogo    = "page_logo"    // логотип / аватар страницы, 1:1
	PurposePageCover   = "page_cover"   // обложка страницы, 3:1
	PurposePagePhoto   = "page_photo"   // фото в блоках страницы
	PurposeVideoPoster = "video_poster" // кадр-заставка загруженного видео
	PurposePageVideo   = "page_video"   // видео (файл MP4 / WebM как есть; загружается отдельно от фото)
)

// MediaSizes — ширины (по длинной стороне), в которых хранится каждое фото (WebP); меньше исходного не растягиваем.
var MediaSizes = []int{320, 800, 1600}

// ValidMediaSize — размер из списка.
func ValidMediaSize(n int) bool {
	for _, s := range MediaSizes {
		if s == n {
			return true
		}
	}
	return false
}

// Aspect — пропорции назначения (ширина/высота); 0 — свободные.
func Aspect(purpose string) float64 {
	switch purpose {
	case PurposeAvatar, PurposeCardAvatar, PurposePageLogo:
		return 1
	case PurposeTaskCover:
		return 16.0 / 9.0
	case PurposePageCover:
		return 3
	}
	return 0
}

// ValidPurpose — назначение фото из списка (видео загружается отдельно).
func ValidPurpose(p string) bool {
	switch p {
	case PurposeAvatar, PurposeTaskCover, PurposeTaskPhoto, PurposeCardAvatar, PurposeCardWork,
		PurposePageLogo, PurposePageCover, PurposePagePhoto, PurposeVideoPoster:
		return true
	}
	return false
}

// Media — загруженное фото (метаданные; файлы — в хранилище).
type Media struct {
	ID        string
	OwnerID   int64
	Purpose   string
	Width     int
	Height    int
	Bytes     int64
	Subject   string // "", "user", "task", "creator_card"
	SubjectID int64
	CreatedAt time.Time
}

// MediaAccess — кто может смотреть фото: Public — всем (аватар профиля, опубликованное), иначе — только владелец.
type MediaAccess struct {
	ID      string
	OwnerID int64
	Public  bool
}

// MediaKey — ключ файла фото в хранилище.
func MediaKey(id string, size int) string {
	return "media/" + id + "/" + strconv.Itoa(size) + ".webp"
}

// VideoKey — ключ файла видео в хранилище (один файл, как загружен).
func VideoKey(id string) string { return "media/" + id + "/video" }

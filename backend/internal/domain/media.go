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
	case PurposeAvatar, PurposeCardAvatar:
		return 1
	case PurposeTaskCover:
		return 16.0 / 9.0
	}
	return 0
}

// ValidPurpose — назначение из списка.
func ValidPurpose(p string) bool {
	switch p {
	case PurposeAvatar, PurposeTaskCover, PurposeTaskPhoto, PurposeCardAvatar, PurposeCardWork:
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

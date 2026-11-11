package domain

import "time"

// SocialPlatform — площадка, на которой автор подтверждает аккаунт.
type SocialPlatform string

// Площадки (порядок — как в анкете).
const (
	PlatformTelegram  SocialPlatform = "telegram"
	PlatformYouTube   SocialPlatform = "youtube"
	PlatformVK        SocialPlatform = "vk"
	PlatformTikTok    SocialPlatform = "tiktok"
	PlatformInstagram SocialPlatform = "instagram"
	PlatformThreads   SocialPlatform = "threads"
)

// SocialPlatforms — все площадки по порядку показа.
var SocialPlatforms = []SocialPlatform{PlatformTelegram, PlatformYouTube, PlatformVK, PlatformTikTok, PlatformInstagram, PlatformThreads}

// Valid — площадка из списка.
func (p SocialPlatform) Valid() bool {
	for _, x := range SocialPlatforms {
		if p == x {
			return true
		}
	}
	return false
}

// SocialStatus — состояние подтверждения.
type SocialStatus string

// Статусы подтверждения.
const (
	SocialUnverified SocialStatus = "unverified" // не подключено
	SocialPending    SocialStatus = "pending"    // ждёт проверки (начато подключение, проверка не завершена)
	SocialVerified   SocialStatus = "verified"   // подтверждено, цифры от площадки
	SocialFailed     SocialStatus = "failed"     // доступ потерян — нужна повторная проверка
)

// SocialMethod — как подтверждено.
type SocialMethod string

// Способы подтверждения.
const (
	MethodBot   SocialMethod = "bot"   // Telegram: бот — админ канала
	MethodOAuth SocialMethod = "oauth" // официальный API площадки с разрешения автора
)

// Виды аккаунта.
const (
	SocialProfile = "profile" // личный профиль (TikTok, Instagram, Threads, VK)
	SocialChannel = "channel" // канал (Telegram, YouTube)
	SocialGroup   = "group"   // сообщество VK
)

// SocialStats — цифры от площадки. nil — площадка не отдала.
type SocialStats struct {
	Followers       *int64
	FollowersHidden bool // автор скрыл число подписчиков (YouTube)
	Views           *int64
	Videos          *int64
	Likes           *int64
}

// SocialAccount — подтверждённый (или потерявший доступ) аккаунт автора.
type SocialAccount struct {
	ID            int64
	UserID        int64
	Platform      SocialPlatform
	ExternalID    string
	Kind          string
	Handle        string // @имя без @
	Title         string
	URL           string // https://… или ""
	Status        SocialStatus
	Method        SocialMethod
	CredentialID  int64 // 0 — без токенов (Telegram)
	Stats         SocialStats
	FailReason    string
	Failures      int
	VerifiedAt    *time.Time
	RefreshedAt   *time.Time
	NextRefreshAt *time.Time
	CreatedAt     time.Time
}

// SocialCredential — доступ OAuth (токены — только зашифрованно).
type SocialCredential struct {
	ID               int64
	UserID           int64
	Platform         SocialPlatform
	Subject          string
	Secret           []byte // шифротекст
	AccessExpiresAt  *time.Time
	RefreshExpiresAt *time.Time
}

// SocialState — начатое подключение (state OAuth или токен ссылки на бота).
type SocialState struct {
	UserID    int64
	Platform  SocialPlatform
	Verifier  []byte // шифротекст PKCE verifier (и device_id VK)
	CreatedAt time.Time
	ExpiresAt time.Time
}

// TelegramChannel — канал или группа, куда бота добавили (по my_chat_member).
type TelegramChannel struct {
	ChatID    int64
	Title     string
	Username  string
	Type      string // channel | group | supergroup
	AddedBy   int64
	BotStatus string
}

// Kind — вид аккаунта: канал или группа.
func (c TelegramChannel) Kind() string {
	if c.Type == "channel" {
		return SocialChannel
	}
	return SocialGroup
}

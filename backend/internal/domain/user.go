package domain

import "time"

// Режимы аккаунта (совпадают с аудиториями тарифов). Один аккаунт — оба режима, активен один.
const (
	ModeBusiness = AudienceBusiness
	ModeCreator  = AudienceCreator
)

// Способы входа.
const (
	ProviderEmail    = "email"
	ProviderTelegram = "telegram"
	ProviderGoogle   = "google"
)

// ValidMode — режим из списка.
func ValidMode(m string) bool { return m == ModeBusiness || m == ModeCreator }

// User — пользователь площадки.
type User struct {
	ID          int64
	Name        string
	Email       string // "" — нет подтверждённой почты
	AvatarURL   string
	City        string
	ActiveMode  string     // ModeBusiness | ModeCreator
	OnboardedAt *time.Time // когда впервые сохранены общие данные
	CreatedAt   time.Time
}

// Onboarded — общие данные профиля сохранялись хотя бы раз.
func (u User) Onboarded() bool { return u.OnboardedAt != nil }

// NewUser — данные для создания пользователя при первом входе.
type NewUser struct {
	Name      string
	Email     string
	AvatarURL string
}

// Identity — способ входа: провайдер и его идентификатор человека.
type Identity struct {
	Provider string
	Subject  string
	Email    string
}

// LoginCode — одноразовый код из письма (хранится только HMAC).
type LoginCode struct {
	ID        int64
	Email     string
	CodeHash  []byte
	Attempts  int
	CreatedAt time.Time
	ExpiresAt time.Time
}

// NewLoginCode — новый код для записи.
type NewLoginCode struct {
	Email     string
	CodeHash  []byte
	IP        string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CodeStats — сколько кодов ушло на почту или с адреса за окно и когда.
type CodeStats struct {
	Sent    int
	FirstAt time.Time // самый ранний в окне
	LastAt  time.Time // самый поздний в окне
}

// Basics — общие данные профиля (для обоих режимов).
type Basics struct {
	Name string
	City string
}

// BusinessProfile — профиль режима «Бизнес».
type BusinessProfile struct {
	Name      string // название компании или бренда
	City      string
	Spheres   []string // slug тем из справочника
	UpdatedAt time.Time
}

// CreatorProfile — профиль режима «Автор».
type CreatorProfile struct {
	Name      string // имя или псевдоним
	City      string
	Spheres   []string // темы, о которых пишет
	Channels  []string // https://… или @имя
	UpdatedAt time.Time
}

// Profiles — профили режимов пользователя; nil — профиль режима ещё не заполнен.
type Profiles struct {
	Business *BusinessProfile
	Creator  *CreatorProfile
}

// Filled — заполнен ли профиль режима.
func (p Profiles) Filled(mode string) bool {
	switch mode {
	case ModeBusiness:
		return p.Business != nil
	case ModeCreator:
		return p.Creator != nil
	}
	return false
}

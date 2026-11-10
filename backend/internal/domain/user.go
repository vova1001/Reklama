package domain

import "time"

// Роли пользователей (совпадают с аудиториями тарифов).
const (
	RoleBusiness = AudienceBusiness
	RoleCreator  = AudienceCreator
)

// Способы входа.
const (
	ProviderEmail    = "email"
	ProviderTelegram = "telegram"
	ProviderGoogle   = "google"
)

// ValidRole — роль из списка (пустая — «ещё не выбрана» — не считается).
func ValidRole(r string) bool { return r == RoleBusiness || r == RoleCreator }

// User — пользователь площадки.
type User struct {
	ID          int64
	Role        string // "" — не выбрана
	Name        string
	Email       string // "" — нет подтверждённой почты
	AvatarURL   string
	City        string
	Spheres     []string // бизнес: темы
	Channels    []string // автор: ссылки на каналы
	OnboardedAt *time.Time
	CreatedAt   time.Time
}

// Onboarded — анкета первого входа заполнена.
func (u User) Onboarded() bool { return u.OnboardedAt != nil }

// NewUser — данные для создания пользователя при первом входе.
type NewUser struct {
	Role      string
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
	Role      string
	Attempts  int
	CreatedAt time.Time
	ExpiresAt time.Time
}

// NewLoginCode — новый код для записи.
type NewLoginCode struct {
	Email     string
	CodeHash  []byte
	Role      string
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

// Profile — анкета первого входа.
type Profile struct {
	Role     string
	Name     string
	City     string
	Spheres  []string
	Channels []string
}

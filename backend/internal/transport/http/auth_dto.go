package httpapi

import "ohvat/backend/internal/domain"

// Тела запросов и ответы входа — строго по api/openapi.yaml.

type emailStartIn struct {
	Email   string `json:"email"`
	Role    string `json:"role"`
	Consent bool   `json:"consent"`
}

type emailVerifyIn struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type profileIn struct {
	Role     string   `json:"role"`
	Name     string   `json:"name"`
	City     string   `json:"city"`
	Spheres  []string `json:"spheres"`
	Channels []string `json:"channels"`
}

type telegramDTO struct {
	BotID   string `json:"botId"`
	BotName string `json:"botName"`
	Bot     bool   `json:"bot"` // вход через бота (ссылка t.me) — основной способ
}

type botStartIn struct {
	Role    string `json:"role"`
	Consent bool   `json:"consent"`
}

type botStartDTO struct {
	URL       string `json:"url"`
	ExpiresIn int    `json:"expiresIn"`
}

type botStatusDTO struct {
	Status  string   `json:"status"` // pending | ok
	User    *userDTO `json:"user,omitempty"`
	Next    string   `json:"next,omitempty"`
	Created bool     `json:"created,omitempty"`
}

type authConfigDTO struct {
	Email     bool         `json:"email"`
	Google    bool         `json:"google"`
	Telegram  *telegramDTO `json:"telegram"`
	CSRFToken string       `json:"csrfToken"`
}

type codeSentDTO struct {
	Email       string `json:"email"`
	ResendAfter int    `json:"resendAfter"`
	ExpiresIn   int    `json:"expiresIn"`
}

type userDTO struct {
	ID        int64    `json:"id"`
	Role      *string  `json:"role"`
	Name      string   `json:"name"`
	Email     *string  `json:"email"`
	AvatarURL string   `json:"avatarUrl"`
	City      string   `json:"city"`
	Spheres   []string `json:"spheres"`
	Channels  []string `json:"channels"`
	Onboarded bool     `json:"onboarded"`
}

type loginDTO struct {
	User    userDTO `json:"user"`
	Next    string  `json:"next"`
	Created bool    `json:"created"`
}

type meDTO struct {
	User      userDTO  `json:"user"`
	Providers []string `json:"providers"`
}

func toUserDTO(u domain.User) userDTO {
	out := userDTO{
		ID: u.ID, Name: u.Name, AvatarURL: u.AvatarURL, City: u.City,
		Spheres: nonNilStr(u.Spheres), Channels: nonNilStr(u.Channels), Onboarded: u.Onboarded(),
	}
	if u.Role != "" {
		r := u.Role
		out.Role = &r
	}
	if u.Email != "" {
		e := u.Email
		out.Email = &e
	}
	return out
}

func nonNilStr(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

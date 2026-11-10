package httpapi

import "ohvat/backend/internal/domain"

// Тела запросов и ответы входа — строго по api/openapi.yaml.

type emailStartIn struct {
	Email   string `json:"email"`
	Consent bool   `json:"consent"`
}

type emailVerifyIn struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type telegramDTO struct {
	BotID   string `json:"botId"`
	BotName string `json:"botName"`
	Bot     bool   `json:"bot"` // вход через бота (ссылка t.me) — основной способ
}

type botStartIn struct {
	Consent bool `json:"consent"`
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
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Email     *string `json:"email"`
	AvatarURL string  `json:"avatarUrl"`
	City      string  `json:"city"`
	Mode      string  `json:"mode"`
	Onboarded bool    `json:"onboarded"`
}

type loginDTO struct {
	User    userDTO `json:"user"`
	Next    string  `json:"next"`
	Created bool    `json:"created"`
}

func toUserDTO(u domain.User) userDTO {
	out := userDTO{ID: u.ID, Name: u.Name, AvatarURL: u.AvatarURL, City: u.City, Mode: u.ActiveMode, Onboarded: u.Onboarded()}
	if out.Mode == "" {
		out.Mode = domain.ModeBusiness
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

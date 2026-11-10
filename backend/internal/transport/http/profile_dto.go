package httpapi

import (
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// Тела запросов и ответы приложения (режим, профили) — строго по api/openapi.yaml.

type modeIn struct {
	Mode string `json:"mode"`
}

type basicsIn struct {
	Name string `json:"name"`
	City string `json:"city"`
}

type businessIn struct {
	Name    string   `json:"name"`
	City    string   `json:"city"`
	Spheres []string `json:"spheres"`
}

type creatorIn struct {
	Name     string   `json:"name"`
	City     string   `json:"city"`
	Spheres  []string `json:"spheres"`
	Channels []string `json:"channels"`
}

type stepDTO struct {
	Key  string `json:"key"`
	Done bool   `json:"done"`
}

type profilesFilledDTO struct {
	Business bool `json:"business"`
	Creator  bool `json:"creator"`
}

type meDTO struct {
	User       userDTO           `json:"user"`
	Providers  []string          `json:"providers"`
	Profiles   profilesFilledDTO `json:"profiles"`
	Checklist  []stepDTO         `json:"checklist"`
	Completion int               `json:"completion"`
}

type businessDTO struct {
	Name      string    `json:"name"`
	City      string    `json:"city"`
	Spheres   []string  `json:"spheres"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type creatorDTO struct {
	Name      string    `json:"name"`
	City      string    `json:"city"`
	Spheres   []string  `json:"spheres"`
	Channels  []string  `json:"channels"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type businessOut struct {
	Profile *businessDTO `json:"profile"`
}

type creatorOut struct {
	Profile *creatorDTO `json:"profile"`
}

type logoutAllDTO struct {
	Sessions int64 `json:"sessions"`
}

func toMeDTO(o service.Overview) meDTO {
	steps := make([]stepDTO, 0, len(o.Checklist))
	for _, st := range o.Checklist {
		steps = append(steps, stepDTO{Key: st.Key, Done: st.Done})
	}
	return meDTO{
		User: toUserDTO(o.User), Providers: nonNilStr(o.Providers),
		Profiles:  profilesFilledDTO{Business: o.Profiles.Business != nil, Creator: o.Profiles.Creator != nil},
		Checklist: steps, Completion: o.Completion,
	}
}

func toBusinessDTO(p *domain.BusinessProfile) *businessDTO {
	if p == nil {
		return nil
	}
	return &businessDTO{Name: p.Name, City: p.City, Spheres: nonNilStr(p.Spheres), UpdatedAt: p.UpdatedAt.UTC()}
}

func toCreatorDTO(p *domain.CreatorProfile) *creatorDTO {
	if p == nil {
		return nil
	}
	return &creatorDTO{Name: p.Name, City: p.City, Spheres: nonNilStr(p.Spheres), Channels: nonNilStr(p.Channels), UpdatedAt: p.UpdatedAt.UTC()}
}

package service

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

var (
	reSlug   = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	reHandle = regexp.MustCompile(`^@[A-Za-z0-9_.]{3,40}$`)
)

// SaveProfile — анкета первого входа (и её правка). Роль после заполнения анкеты не меняется.
// Бизнес: сферы — 1–5 тем из справочника. Автор: каналы — 1–5 ссылок или @имён.
func (s *Auth) SaveProfile(ctx context.Context, u domain.User, p domain.Profile) (domain.User, error) {
	ve := &ValidationError{}
	p.Role = strings.TrimSpace(p.Role)
	if p.Role == "" {
		p.Role = u.Role
	}
	switch {
	case !domain.ValidRole(p.Role):
		ve.add("role", "Выберите: бизнес или автор")
	case u.Onboarded() && u.Role != "" && p.Role != u.Role:
		return domain.User{}, ErrRoleLocked
	}
	p.Name = strings.Join(strings.Fields(p.Name), " ")
	p.City = strings.Join(strings.Fields(p.City), " ")
	checkLen(ve, "name", p.Name, 2, 80, "Как вас называть — от 2 до 80 символов")
	checkLen(ve, "city", p.City, 2, 80, "Город — от 2 до 80 символов")

	switch p.Role {
	case domain.RoleBusiness:
		p.Channels = nil
		p.Spheres = uniq(p.Spheres)
		switch {
		case len(p.Spheres) == 0:
			ve.add("spheres", "Выберите хотя бы одну сферу")
		case len(p.Spheres) > 5:
			ve.add("spheres", "Не больше 5 сфер")
		default:
			for _, sl := range p.Spheres {
				if !reSlug.MatchString(sl) {
					ve.add("spheres", "Выберите сферы из списка")
				}
			}
		}
	case domain.RoleCreator:
		p.Spheres = nil
		ch := make([]string, 0, len(p.Channels))
		for _, c := range uniq(p.Channels) {
			n, ok := normChannel(c)
			if !ok {
				ve.add("channels", "Ссылка на канал — https://… или @имя")
				continue
			}
			ch = append(ch, n)
		}
		p.Channels = ch
		if len(p.Channels) == 0 && ve.Fields["channels"] == "" {
			ve.add("channels", "Добавьте хотя бы одну ссылку на канал")
		}
		if len(p.Channels) > 5 {
			ve.add("channels", "Не больше 5 ссылок")
		}
	}
	if err := ve.orNil(); err != nil {
		return domain.User{}, err
	}
	if p.Role == domain.RoleBusiness {
		n, err := s.users.CountCategories(ctx, p.Spheres)
		if err != nil {
			return domain.User{}, err
		}
		if n != len(p.Spheres) {
			return domain.User{}, &ValidationError{Fields: map[string]string{"spheres": "Выберите сферы из списка"}}
		}
	}
	return s.users.UpdateProfile(ctx, u.ID, p)
}

func checkLen(ve *ValidationError, field, v string, minN, maxN int, msg string) {
	if n := utf8.RuneCountInString(v); n < minN || n > maxN {
		ve.add(field, msg)
	}
}

// normChannel — ссылка https://… (http — повышается до https) или @имя.
func normChannel(raw string) (string, bool) {
	c := strings.TrimSpace(raw)
	if c == "" || len(c) > 200 {
		return "", false
	}
	if reHandle.MatchString(c) {
		return c, true
	}
	if !strings.Contains(c, "://") {
		c = "https://" + c
	}
	u, err := url.Parse(c)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.Contains(u.Host, ".") || u.User != nil {
		return "", false
	}
	u.Scheme = "https"
	return u.String(), true
}

func uniq(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

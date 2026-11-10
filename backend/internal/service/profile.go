package service

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// ===== зависимости (только интерфейсы) =====

// ProfileRepository — режим аккаунта, общие данные и профили режимов.
type ProfileRepository interface {
	SetMode(ctx context.Context, userID int64, mode string) (domain.User, error)
	UpdateBasics(ctx context.Context, userID int64, b domain.Basics) (domain.User, error)
	// BusinessProfile / CreatorProfile — профиль режима; не заполнен — domain.ErrNotFound.
	BusinessProfile(ctx context.Context, userID int64) (domain.BusinessProfile, error)
	CreatorProfile(ctx context.Context, userID int64) (domain.CreatorProfile, error)
	SaveBusinessProfile(ctx context.Context, userID int64, p domain.BusinessProfile) (domain.BusinessProfile, error)
	SaveCreatorProfile(ctx context.Context, userID int64, p domain.CreatorProfile) (domain.CreatorProfile, error)
	CountCategories(ctx context.Context, slugs []string) (int, error)
}

// ProviderLister — способы входа пользователя.
type ProviderLister interface {
	Providers(ctx context.Context, userID int64) ([]string, error)
}

// ===== сервис =====

// Profiles — один аккаунт, два режима: переключение режима, общие данные, профили «Бизнес» и «Автор».
// Профиль режима не обязателен: он заполняется, когда впервые нужен; сервис только подсказывает (Checklist).
type Profiles struct {
	repo ProfileRepository
	ids  ProviderLister
}

// NewProfiles создаёт сервис профилей.
func NewProfiles(repo ProfileRepository, ids ProviderLister) *Profiles {
	return &Profiles{repo: repo, ids: ids}
}

// Шаги заполнения профиля (ключи — для подсказок на странице).
const (
	StepBasics   = "basics"   // как вас называть и город
	StepProfile  = "profile"  // профиль активного режима
	StepTelegram = "telegram" // Telegram привязан — уведомления в боте
)

// Step — шаг заполнения профиля.
type Step struct {
	Key  string
	Done bool
}

// Overview — всё о вошедшем: пользователь, способы входа, профили режимов и прогресс заполнения.
type Overview struct {
	User       domain.User
	Providers  []string
	Profiles   domain.Profiles
	Checklist  []Step
	Completion int // 0–100, для активного режима
}

// Overview — данные для приложения после входа.
func (s *Profiles) Overview(ctx context.Context, u domain.User) (Overview, error) {
	prov, err := s.ids.Providers(ctx, u.ID)
	if err != nil {
		return Overview{}, err
	}
	ps, err := s.profiles(ctx, u.ID)
	if err != nil {
		return Overview{}, err
	}
	o := Overview{User: u, Providers: prov, Profiles: ps}
	o.Checklist = []Step{
		{Key: StepBasics, Done: strings.TrimSpace(u.Name) != "" && strings.TrimSpace(u.City) != ""},
		{Key: StepProfile, Done: ps.Filled(u.ActiveMode)},
		{Key: StepTelegram, Done: contains(prov, domain.ProviderTelegram)},
	}
	done := 0
	for _, st := range o.Checklist {
		if st.Done {
			done++
		}
	}
	o.Completion = done * 100 / len(o.Checklist)
	return o, nil
}

func (s *Profiles) profiles(ctx context.Context, userID int64) (domain.Profiles, error) {
	var out domain.Profiles
	b, err := s.repo.BusinessProfile(ctx, userID)
	switch {
	case err == nil:
		out.Business = &b
	case !errors.Is(err, domain.ErrNotFound):
		return domain.Profiles{}, err
	}
	c, err := s.repo.CreatorProfile(ctx, userID)
	switch {
	case err == nil:
		out.Creator = &c
	case !errors.Is(err, domain.ErrNotFound):
		return domain.Profiles{}, err
	}
	return out, nil
}

// SwitchMode — сделать режим активным. Профиль режима заполнять не обязательно.
func (s *Profiles) SwitchMode(ctx context.Context, u domain.User, mode string) (Overview, error) {
	mode = strings.TrimSpace(mode)
	if !domain.ValidMode(mode) {
		return Overview{}, &ValidationError{Fields: map[string]string{"mode": "Режим — «Бизнес» или «Автор»"}}
	}
	if mode != u.ActiveMode {
		nu, err := s.repo.SetMode(ctx, u.ID, mode)
		if err != nil {
			return Overview{}, err
		}
		u = nu
	}
	return s.Overview(ctx, u)
}

// SaveBasics — общие данные: как вас называть и город.
func (s *Profiles) SaveBasics(ctx context.Context, u domain.User, b domain.Basics) (domain.User, error) {
	ve := &ValidationError{}
	b.Name = squash(b.Name)
	b.City = squash(b.City)
	checkLen(ve, "name", b.Name, 2, 80, "Как вас называть — от 2 до 80 символов")
	checkLen(ve, "city", b.City, 2, 80, "Город — от 2 до 80 символов")
	if err := ve.orNil(); err != nil {
		return domain.User{}, err
	}
	return s.repo.UpdateBasics(ctx, u.ID, b)
}

// Business — профиль бизнеса; nil — ещё не заполнен.
func (s *Profiles) Business(ctx context.Context, u domain.User) (*domain.BusinessProfile, error) {
	p, err := s.repo.BusinessProfile(ctx, u.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Creator — профиль автора; nil — ещё не заполнен.
func (s *Profiles) Creator(ctx context.Context, u domain.User) (*domain.CreatorProfile, error) {
	p, err := s.repo.CreatorProfile(ctx, u.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SaveBusiness — профиль бизнеса: название, город, 1–5 сфер из справочника.
func (s *Profiles) SaveBusiness(ctx context.Context, u domain.User, p domain.BusinessProfile) (domain.BusinessProfile, error) {
	ve := &ValidationError{}
	p.Name = squash(p.Name)
	p.City = squash(p.City)
	checkLen(ve, "name", p.Name, 2, 80, "Название — от 2 до 80 символов")
	checkLen(ve, "city", p.City, 2, 80, "Город — от 2 до 80 символов")
	p.Spheres = checkSpheres(ve, p.Spheres)
	if err := ve.orNil(); err != nil {
		return domain.BusinessProfile{}, err
	}
	if err := s.checkCatalog(ctx, p.Spheres); err != nil {
		return domain.BusinessProfile{}, err
	}
	return s.repo.SaveBusinessProfile(ctx, u.ID, p)
}

// SaveCreator — профиль автора: имя, город, 1–5 тем из справочника, 1–5 каналов (https://… или @имя).
func (s *Profiles) SaveCreator(ctx context.Context, u domain.User, p domain.CreatorProfile) (domain.CreatorProfile, error) {
	ve := &ValidationError{}
	p.Name = squash(p.Name)
	p.City = squash(p.City)
	checkLen(ve, "name", p.Name, 2, 80, "Имя или псевдоним — от 2 до 80 символов")
	checkLen(ve, "city", p.City, 2, 80, "Город — от 2 до 80 символов")
	p.Spheres = checkSpheres(ve, p.Spheres)
	p.Channels = checkChannels(ve, p.Channels)
	if err := ve.orNil(); err != nil {
		return domain.CreatorProfile{}, err
	}
	if err := s.checkCatalog(ctx, p.Spheres); err != nil {
		return domain.CreatorProfile{}, err
	}
	return s.repo.SaveCreatorProfile(ctx, u.ID, p)
}

// checkCatalog — все темы есть в справочнике и активны.
func (s *Profiles) checkCatalog(ctx context.Context, slugs []string) error {
	n, err := s.repo.CountCategories(ctx, slugs)
	if err != nil {
		return err
	}
	if n != len(slugs) {
		return &ValidationError{Fields: map[string]string{"spheres": "Выберите сферы из списка"}}
	}
	return nil
}

// ===== проверки полей =====

var (
	reSlug   = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	reHandle = regexp.MustCompile(`^@[A-Za-z0-9_.]{3,40}$`)
)

const maxItems = 5

func checkSpheres(ve *ValidationError, in []string) []string {
	out := uniq(in)
	switch {
	case len(out) == 0:
		ve.add("spheres", "Выберите хотя бы одну сферу")
	case len(out) > maxItems:
		ve.add("spheres", "Не больше 5 сфер")
	default:
		for _, sl := range out {
			if !reSlug.MatchString(sl) {
				ve.add("spheres", "Выберите сферы из списка")
			}
		}
	}
	return out
}

func checkChannels(ve *ValidationError, in []string) []string {
	raw := uniq(in)
	if len(raw) > maxItems {
		ve.add("channels", "Не больше 5 ссылок")
		return raw
	}
	out := make([]string, 0, len(raw))
	for _, c := range raw {
		n, ok := normChannel(c)
		if !ok {
			ve.add("channels", "Ссылка на канал — https://… или @имя")
			continue
		}
		if !contains(out, n) {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		ve.add("channels", "Добавьте хотя бы одну ссылку на канал")
	}
	return out
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

// squash — пробелы по краям убраны, внутри — по одному.
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

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

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

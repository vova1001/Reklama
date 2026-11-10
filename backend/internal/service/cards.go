package service

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// CardRepository — анкеты авторов (одна на пользователя).
type CardRepository interface {
	// Card — анкета; нет — domain.ErrNotFound.
	Card(ctx context.Context, userID int64) (domain.CreatorCard, error)
	// SaveCard — создать или обновить (только черновик или отклонённую, иначе domain.ErrConflict).
	SaveCard(ctx context.Context, c domain.CreatorCard) (domain.CreatorCard, error)
	// SetCardStatus — смена статуса из ch.From (иначе domain.ErrConflict) и запись в журнал — атомарно.
	SetCardStatus(ctx context.Context, ch domain.StatusChange) (domain.CreatorCard, error)
}

// PlatformInput — площадка из формы: ссылка и подписчики (вид определяется по адресу).
type PlatformInput struct {
	URL       string
	Followers int64
}

// CardInput — анкета из формы. Черновик может быть неполным; полноту проверяет Submit.
type CardInput struct {
	Name      string
	City      string
	Topics    []string
	Platforms []PlatformInput
	Blocks    []domain.Block
	Avatar    string   // media.id аватара
	Works     []string // media.id примеров работ, по порядку
}

// Cards — анкета автора: черновик → на проверку → решение модератора; бизнесу видны только одобренные.
type Cards struct {
	repo CardRepository
	ListingDeps
}

// NewCards создаёт сервис анкет.
func NewCards(repo CardRepository, d ListingDeps) *Cards {
	d.defaults()
	return &Cards{repo: repo, ListingDeps: d}
}

// Get — своя анкета; nil — ещё не создана.
func (s *Cards) Get(ctx context.Context, u domain.User) (*domain.CreatorCard, error) {
	c, err := s.repo.Card(ctx, u.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Save — сохранить черновик (или правку отклонённой анкеты; статус не меняется).
func (s *Cards) Save(ctx context.Context, u domain.User, in CardInput) (domain.CreatorCard, error) {
	cur, err := s.repo.Card(ctx, u.ID)
	switch {
	case err == nil:
		if !cur.Status.Editable() {
			return domain.CreatorCard{}, ErrWrongStatus
		}
	case !errors.Is(err, domain.ErrNotFound):
		return domain.CreatorCard{}, err
	}
	exists := err == nil
	c, err := s.shape(ctx, u.ID, exists, in)
	if err != nil {
		return domain.CreatorCard{}, err
	}
	c.UserID = u.ID
	out, err := s.repo.SaveCard(ctx, c)
	return out, conflictToStatus(err)
}

// Submit — отправить на проверку: анкета заполнена полностью, лимит отправок за сутки не исчерпан.
func (s *Cards) Submit(ctx context.Context, u domain.User) (domain.CreatorCard, error) {
	cur, err := s.repo.Card(ctx, u.ID)
	if err != nil {
		return domain.CreatorCard{}, err
	}
	if !domain.CanTransition(cur.Status, domain.StatusPending, domain.ActorOwner) {
		return domain.CreatorCard{}, ErrWrongStatus
	}
	if err := cardComplete(cur); err != nil {
		return domain.CreatorCard{}, err
	}
	if err := checkSubmitQuota(ctx, s.Submits, s.Clock, u.ID, s.Limits.SubmitsPerDay); err != nil {
		return domain.CreatorCard{}, err
	}
	out, err := s.repo.SetCardStatus(ctx, domain.StatusChange{SubjectID: u.ID, From: cur.Status, To: domain.StatusPending, ActorID: u.ID})
	return out, conflictToStatus(err)
}

// Withdraw — вернуть анкету с проверки в черновик.
func (s *Cards) Withdraw(ctx context.Context, u domain.User) (domain.CreatorCard, error) {
	cur, err := s.repo.Card(ctx, u.ID)
	if err != nil {
		return domain.CreatorCard{}, err
	}
	if !domain.CanTransition(cur.Status, domain.StatusDraft, domain.ActorOwner) {
		return domain.CreatorCard{}, ErrWrongStatus
	}
	out, err := s.repo.SetCardStatus(ctx, domain.StatusChange{SubjectID: u.ID, From: cur.Status, To: domain.StatusDraft, ActorID: u.ID})
	return out, conflictToStatus(err)
}

// shape — проверка формы: длины, темы (общие или свои, своих — до 3), площадки (ссылка, подписчики), блоки, фото.
func (s *Cards) shape(ctx context.Context, userID int64, exists bool, in CardInput) (domain.CreatorCard, error) {
	ve := &ValidationError{}
	c := domain.CreatorCard{Name: squash(in.Name), City: squash(in.City)}
	maxLen(ve, "name", c.Name, 80, "Имя — до 80 символов")
	maxLen(ve, "city", c.City, 80, "Город — до 80 символов")
	c.Topics = uniq(in.Topics)
	if len(c.Topics) > maxItems {
		ve.add("topics", "Не больше 5 тем")
	}
	for _, t := range c.Topics {
		if !reSlug.MatchString(t) {
			ve.add("topics", "Выберите темы из списка")
			break
		}
	}
	c.Platforms = checkPlatforms(ve, in.Platforms)
	c.Blocks = checkBlocks(ve, in.Blocks)
	var sid int64
	if exists {
		sid = userID
	}
	avatar, err := checkMedia(ctx, s.Media, ve, userID, nonEmpty(in.Avatar), mediaSpec{"avatar", domain.PurposeCardAvatar, 1, domain.SubjectCard, sid})
	if err != nil {
		return domain.CreatorCard{}, err
	}
	if len(avatar) == 1 {
		c.Avatar = avatar[0]
	}
	if c.Works, err = checkMedia(ctx, s.Media, ve, userID, in.Works, mediaSpec{"works", domain.PurposeCardWork, maxCardWorks, domain.SubjectCard, sid}); err != nil {
		return domain.CreatorCard{}, err
	}
	if err := ve.orNil(); err != nil {
		return domain.CreatorCard{}, err
	}
	if len(c.Topics) > 0 {
		n, custom, err := s.Categories.Usable(ctx, userID, c.Topics)
		if err != nil {
			return domain.CreatorCard{}, err
		}
		if n != len(c.Topics) {
			return domain.CreatorCard{}, &ValidationError{Fields: map[string]string{"topics": "Выберите темы из списка"}}
		}
		if custom > maxCustomPerItem {
			return domain.CreatorCard{}, &ValidationError{Fields: map[string]string{"topics": fmt.Sprintf("Своих тем — не больше %d", maxCustomPerItem)}}
		}
	}
	return c, nil
}

// checkPlatforms — до 6 площадок: ссылка https://…, подписчики — число; вид — по домену; повторы убираются.
// Пустая строка (ни ссылки, ни подписчиков) отбрасывается; ошибка — в "platforms.<номер во входном списке>".
func checkPlatforms(ve *ValidationError, in []PlatformInput) []domain.Platform {
	if len(in) > maxPlatforms {
		ve.add("platforms", fmt.Sprintf("Не больше %d площадок", maxPlatforms))
		return nil
	}
	out := make([]domain.Platform, 0, len(in))
	seen := map[string]bool{}
	for i, p := range in {
		key := fmt.Sprintf("platforms.%d", i)
		raw := squash(p.URL)
		if raw == "" && p.Followers == 0 {
			continue
		}
		link, ok := normURL(raw)
		if !ok || utf8.RuneCountInString(link) > maxLink {
			ve.add(key, "Ссылка на площадку — https://…")
			continue
		}
		if p.Followers < 0 || p.Followers > maxCount {
			ve.add(key, "Подписчики — число от 0 до 1 000 000 000")
			continue
		}
		if seen[link] {
			continue
		}
		seen[link] = true
		out = append(out, domain.Platform{Kind: platformKind(link), URL: link, Followers: p.Followers})
	}
	return out
}

// cardComplete — к отправке: имя, город, 1–5 тем, площадки с подписчиками, рассказ о себе или примеры.
func cardComplete(c domain.CreatorCard) error {
	ve := &ValidationError{}
	if n := utf8.RuneCountInString(c.Name); n < 2 {
		ve.add("name", "Имя — от 2 символов")
	}
	if utf8.RuneCountInString(c.City) < 2 {
		ve.add("city", "Укажите город")
	}
	if len(c.Topics) == 0 {
		ve.add("topics", "Выберите хотя бы одну тему")
	}
	if len(c.Platforms) == 0 {
		ve.add("platforms", "Добавьте хотя бы одну площадку")
	}
	for i, p := range c.Platforms {
		if p.Followers <= 0 {
			ve.add(fmt.Sprintf("platforms.%d", i), "Укажите число подписчиков")
		}
	}
	blocksComplete(ve, c.Blocks, "Расскажите о себе или добавьте примеры работ")
	return ve.orNil()
}

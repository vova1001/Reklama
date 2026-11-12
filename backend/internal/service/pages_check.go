package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// Проверка страницы в два уровня (как у заданий): «форма» — при каждом сохранении (виды блоков, длины, ссылки,
// свои файлы нужного назначения) и «полнота» — при отправке на проверку. Ошибка блока — "blocks.<номер>".

// Пределы страниц.
const (
	maxPageBlocks     = 20   // блоков на странице
	maxPageTitle      = 80   // название / имя, заголовок блока
	maxPageText       = 3000 // текст блока «О компании», «Заголовок + текст»
	maxWorksText      = 1000 // описание в «Примерах работ»
	maxPageFacts      = 12   // пунктов в «Фактах»
	maxFactLen        = 200
	maxBlockPhotos    = 12 // фото в одном блоке
	maxPagePhotos     = 60 // фото на странице всего
	maxWorksVideos    = 3  // видео в «Примерах работ»
	maxPageVideos     = 5  // видео на странице всего
	maxWorksLinks     = 10
	maxPageSocials    = 6
	maxPageTopics     = 10
	maxPageTextTotal  = 20000 // знаков во всех блоках вместе
	maxAddressLen     = 200
	maxHoursLen       = 120
	maxPhoneLen       = 30
	minPageTitleRunes = 2
)

var rePhone = regexp.MustCompile(`^\+?[0-9 ()-]{5,30}$`)

// shape — проверка формы и нормализация. pageID — 0 для новой (файлы должны быть не прикреплены).
func (s *Pages) shape(ctx context.Context, userID int64, kind domain.PageKind, pageID int64, in PageInput) (domain.Page, error) {
	ve := &ValidationError{}
	p := domain.Page{Title: squash(in.Title), City: squash(in.City)}
	what := "Название"
	if kind == domain.PageCreator {
		what = "Имя"
	}
	maxLen(ve, "title", p.Title, maxPageTitle, fmt.Sprintf("%s — до %d символов", what, maxPageTitle))
	maxLen(ve, "city", p.City, 80, "Город — до 80 символов")
	many, pick := "сфер", "Выберите сферы из списка"
	if kind == domain.PageCreator {
		many, pick = "тем", "Выберите темы из списка"
	}
	p.Spheres = checkSlugs(ve, "spheres", in.Spheres, many, pick)
	p.Logo = firstOf(nonEmpty(strings.TrimSpace(in.Logo)))
	p.Cover = firstOf(nonEmpty(strings.TrimSpace(in.Cover)))
	p.Blocks = checkPageBlocks(ve, kind, in.Blocks)

	// файлы: свои, нужного назначения, не прикреплены к другому — одним запросом на всю страницу
	if err := s.checkPageMedia(ctx, ve, userID, pageID, p); err != nil {
		return domain.Page{}, err
	}
	if err := ve.orNil(); err != nil {
		return domain.Page{}, err
	}
	if err := checkUsable(ctx, s.Categories, userID, "spheres", p.Spheres, pick, "Своих — не больше %d"); err != nil {
		return domain.Page{}, err
	}
	for i, b := range p.Blocks {
		if b.Type == domain.PBTopics {
			if err := checkUsable(ctx, s.Categories, userID, fmt.Sprintf("blocks.%d", i), b.Items, "Выберите темы из списка", "Своих тем — не больше %d"); err != nil {
				return domain.Page{}, err
			}
		}
	}
	return p, nil
}

func firstOf(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// checkPageBlocks — виды блоков страницы этого вида, одиночные — по одному, содержимое — по виду блока.
// Пустые блоки хранятся (страница собирается по частям), на публичной странице не показываются.
func checkPageBlocks(ve *ValidationError, kind domain.PageKind, in []PageBlockInput) []domain.PageBlock {
	if len(in) > maxPageBlocks {
		ve.add("blocks", fmt.Sprintf("Не больше %d блоков", maxPageBlocks))
		return nil
	}
	allowed := domain.PageBlockTypes(kind)
	seen := map[string]bool{}
	out := make([]domain.PageBlock, 0, len(in))
	total, photos, videos := 0, 0, 0
	for i, b := range in {
		key := fmt.Sprintf("blocks.%d", i)
		if !contains(allowed, b.Type) {
			ve.add(key, "Такой блок здесь недоступен")
			continue
		}
		if domain.PageBlockSingle(b.Type) && seen[b.Type] {
			ve.add(key, "Такой блок уже есть")
			continue
		}
		seen[b.Type] = true
		nb := domain.PageBlock{Type: b.Type, Title: squash(b.Title), Items: []string{}, Photos: []string{}, Videos: []domain.PageVideo{}, Links: []string{}}
		maxLen(ve, key, nb.Title, maxPageTitle, fmt.Sprintf("Заголовок — до %d символов", maxPageTitle))
		switch b.Type {
		case domain.PBAbout, domain.PBText:
			nb.Text = cleanText(b.Text)
			maxLen(ve, key, nb.Text, maxPageText, fmt.Sprintf("Текст — до %d символов", maxPageText))
		case domain.PBFacts:
			for _, it := range b.Items {
				if it = squash(it); it != "" {
					maxLen(ve, key, it, maxFactLen, fmt.Sprintf("Пункт — до %d символов", maxFactLen))
					nb.Items = append(nb.Items, it)
				}
			}
			if len(nb.Items) > maxPageFacts {
				ve.add(key, fmt.Sprintf("Не больше %d пунктов", maxPageFacts))
			}
		case domain.PBPhotos:
			nb.Photos = blockPhotos(ve, key, b.Photos)
		case domain.PBVideo:
			nb.Videos = blockVideos(ve, key, b.Videos, 1)
		case domain.PBContacts:
			nb.Contacts = checkContacts(ve, key, b.Contacts)
		case domain.PBTopics:
			nb.Items = uniq(nonEmptyAll(b.Items))
			if len(nb.Items) > maxPageTopics {
				ve.add(key, fmt.Sprintf("Не больше %d тем", maxPageTopics))
			}
			for _, t := range nb.Items {
				if !reSlug.MatchString(t) {
					ve.add(key, "Выберите темы из списка")
					break
				}
			}
		case domain.PBWorks:
			nb.Text = cleanText(b.Text)
			maxLen(ve, key, nb.Text, maxWorksText, fmt.Sprintf("Описание — до %d символов", maxWorksText))
			nb.Photos = blockPhotos(ve, key, b.Photos)
			nb.Videos = blockVideos(ve, key, b.Videos, maxWorksVideos)
			for _, l := range b.Links {
				if l = strings.TrimSpace(l); l == "" {
					continue
				}
				u, ok := normURL(l)
				if !ok {
					ve.add(key, "Ссылка — в виде https://…")
					continue
				}
				nb.Links = append(nb.Links, u)
			}
			nb.Links = uniq(nb.Links)
			if len(nb.Links) > maxWorksLinks {
				ve.add(key, fmt.Sprintf("Не больше %d ссылок", maxWorksLinks))
			}
		}
		total += utf8.RuneCountInString(nb.Title) + utf8.RuneCountInString(nb.Text)
		for _, it := range nb.Items {
			total += utf8.RuneCountInString(it)
		}
		photos += len(nb.Photos)
		videos += len(nb.Videos)
		out = append(out, nb)
	}
	if total > maxPageTextTotal {
		ve.add("blocks", fmt.Sprintf("Слишком много текста — сократите до %d знаков", maxPageTextTotal))
	}
	if photos > maxPagePhotos {
		ve.add("blocks", fmt.Sprintf("Не больше %d фото на странице", maxPagePhotos))
	}
	if videos > maxPageVideos {
		ve.add("blocks", fmt.Sprintf("Не больше %d видео на странице", maxPageVideos))
	}
	return out
}

func blockPhotos(ve *ValidationError, key string, in []string) []string {
	ids := uniq(nonEmptyAll(in))
	if len(ids) > maxBlockPhotos {
		ve.add(key, fmt.Sprintf("Не больше %d фото в блоке", maxBlockPhotos))
	}
	for _, id := range ids {
		if !validMediaID(id) {
			ve.add(key, "Загрузите фото ещё раз")
			break
		}
	}
	return ids
}

// blockVideos — видео файлом (id файла и заставки) или ссылкой YouTube / VK (только белый список; адрес для
// встраивания строится из проверенных частей).
func blockVideos(ve *ValidationError, key string, in []VideoInput, max int) []domain.PageVideo {
	out := []domain.PageVideo{}
	for _, v := range in {
		v.URL, v.Media, v.Poster = strings.TrimSpace(v.URL), strings.TrimSpace(v.Media), strings.TrimSpace(v.Poster)
		switch {
		case v.URL != "":
			pv, ok := domain.ParseVideoLink(v.URL)
			if !ok {
				ve.add(key, "Подойдёт ссылка на ролик YouTube или VK Видео")
				continue
			}
			out = append(out, pv)
		case v.Media != "":
			if !validMediaID(v.Media) || (v.Poster != "" && !validMediaID(v.Poster)) {
				ve.add(key, "Загрузите видео ещё раз")
				continue
			}
			out = append(out, domain.PageVideo{Source: domain.VideoFile, Media: v.Media, Poster: v.Poster})
		}
	}
	if len(out) > max {
		ve.add(key, fmt.Sprintf("Не больше %d видео в блоке", max))
	}
	return out
}

func checkContacts(ve *ValidationError, key string, in *domain.PageContacts) *domain.PageContacts {
	if in == nil {
		return &domain.PageContacts{Socials: []string{}}
	}
	c := &domain.PageContacts{City: squash(in.City), Address: squash(in.Address), Hours: squash(in.Hours), Phone: squash(in.Phone), Socials: []string{}}
	maxLen(ve, key, c.City, 80, "Город — до 80 символов")
	maxLen(ve, key, c.Address, maxAddressLen, fmt.Sprintf("Адрес — до %d символов", maxAddressLen))
	maxLen(ve, key, c.Hours, maxHoursLen, fmt.Sprintf("Часы работы — до %d символов", maxHoursLen))
	if c.Phone != "" && (len(c.Phone) > maxPhoneLen || !rePhone.MatchString(c.Phone)) {
		ve.add(key, "Телефон — цифры, например +7 900 000-00-00")
	}
	if site := strings.TrimSpace(in.Site); site != "" {
		u, ok := normURL(site)
		if !ok {
			ve.add(key, "Сайт — в виде https://…")
		}
		c.Site = u
	}
	for _, l := range in.Socials {
		if l = strings.TrimSpace(l); l == "" {
			continue
		}
		u, ok := normURL(l)
		if !ok {
			ve.add(key, "Соцсеть — ссылкой https://…")
			continue
		}
		c.Socials = append(c.Socials, u)
	}
	c.Socials = uniq(c.Socials)
	if len(c.Socials) > maxPageSocials {
		ve.add(key, fmt.Sprintf("Не больше %d соцсетей", maxPageSocials))
	}
	return c
}

// cleanText — текст с абзацами: без управляющих символов, пробелы в строке схлопнуты, не больше одной пустой строки
// подряд.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		l = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, l)
		l = squash(l)
		if l == "" {
			blank++
			if blank > 1 || len(out) == 0 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// checkPageMedia — логотип, обложка, фото, видео и заставки: свои, нужного назначения, свободные или уже этой
// страницы.
func (s *Pages) checkPageMedia(ctx context.Context, ve *ValidationError, userID, pageID int64, p domain.Page) error {
	ids := domain.PageMedia(p)
	if len(ids) == 0 || s.Media == nil {
		return nil
	}
	ms, err := s.Media.OwnMedia(ctx, userID, ids)
	if err != nil {
		return err
	}
	byID := make(map[string]domain.Media, len(ms))
	for _, m := range ms {
		byID[m.ID] = m
	}
	ok := func(id, purpose string) bool {
		m, found := byID[id]
		free := m.Subject == "" || (m.Subject == string(domain.SubjectPage) && pageID != 0 && m.SubjectID == pageID)
		return found && m.Purpose == purpose && free
	}
	if p.Logo != "" && !ok(p.Logo, domain.PurposePageLogo) {
		ve.add("logo", "Загрузите фото ещё раз")
	}
	if p.Cover != "" && !ok(p.Cover, domain.PurposePageCover) {
		ve.add("cover", "Загрузите фото ещё раз")
	}
	for i, b := range p.Blocks {
		key := fmt.Sprintf("blocks.%d", i)
		for _, id := range b.Photos {
			if !ok(id, domain.PurposePagePhoto) {
				ve.add(key, "Загрузите фото ещё раз")
				break
			}
		}
		for _, v := range b.Videos {
			if v.Source != domain.VideoFile {
				continue
			}
			if !ok(v.Media, domain.PurposePageVideo) || (v.Poster != "" && !ok(v.Poster, domain.PurposeVideoPoster)) {
				ve.add(key, "Загрузите видео ещё раз")
				break
			}
		}
	}
	return nil
}

// pageComplete — к отправке: название (имя), город, хотя бы одна сфера (тема) и хотя бы один блок с содержимым.
func pageComplete(p domain.Page) error {
	ve := &ValidationError{}
	if utf8.RuneCountInString(p.Title) < minPageTitleRunes {
		if p.Kind == domain.PageCreator {
			ve.add("title", "Имя — от 2 символов")
		} else {
			ve.add("title", "Название — от 2 символов")
		}
	}
	if utf8.RuneCountInString(p.City) < 2 {
		ve.add("city", "Укажите город")
	}
	if len(p.Spheres) == 0 {
		if p.Kind == domain.PageCreator {
			ve.add("spheres", "Выберите хотя бы одну тему")
		} else {
			ve.add("spheres", "Выберите хотя бы одну сферу")
		}
	}
	filled := false
	for _, b := range p.Blocks {
		if pageBlockFilled(b) {
			filled = true
			break
		}
	}
	if !filled {
		ve.add("blocks", "Добавьте хотя бы один блок с содержимым")
	}
	return ve.orNil()
}

// pageBlockFilled — у блока есть что показать (автоматические блоки — не в счёт: у них нет своего содержимого).
func pageBlockFilled(b domain.PageBlock) bool {
	if domain.PageBlockAuto(b.Type) {
		return false
	}
	if b.Text != "" || len(b.Items) > 0 || len(b.Photos) > 0 || len(b.Videos) > 0 || len(b.Links) > 0 {
		return true
	}
	if c := b.Contacts; c != nil {
		return c.City != "" || c.Address != "" || c.Hours != "" || c.Site != "" || c.Phone != "" || len(c.Socials) > 0
	}
	return false
}

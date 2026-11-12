package httpapi

import (
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// Страницы бизнеса и авторов — строго по api/openapi.yaml.

type pageContactsDTO struct {
	City    string   `json:"city"`
	Address string   `json:"address"`
	Hours   string   `json:"hours"`
	Site    string   `json:"site"`
	Phone   string   `json:"phone"`
	Socials []string `json:"socials"`
}

type videoIn struct {
	Media  string `json:"media"`
	Poster string `json:"poster"`
	URL    string `json:"url"`
}

type pageBlockIn struct {
	Type     string           `json:"type"`
	Title    string           `json:"title"`
	Text     string           `json:"text"`
	Items    []string         `json:"items"`
	Photos   []string         `json:"photos"`
	Videos   []videoIn        `json:"videos"`
	Links    []string         `json:"links"`
	Contacts *pageContactsDTO `json:"contacts"`
}

type pageIn struct {
	Slug    string        `json:"slug"`
	Title   string        `json:"title"`
	City    string        `json:"city"`
	Spheres []string      `json:"spheres"`
	Logo    string        `json:"logo"`
	Cover   string        `json:"cover"`
	Blocks  []pageBlockIn `json:"blocks"`
}

type slugIn struct {
	Slug string `json:"slug"`
}

// pageVideoDTO — видео: файл (src, poster) или встраивание (embedUrl — адрес для iframe, url — ролик на площадке).
type pageVideoDTO struct {
	Source   string    `json:"source"`
	Media    string    `json:"media,omitempty"`
	Src      string    `json:"src,omitempty"`
	Poster   *photoDTO `json:"poster,omitempty"`
	URL      string    `json:"url,omitempty"`
	EmbedURL string    `json:"embedUrl,omitempty"`
}

type pageBlockDTO struct {
	Type       string           `json:"type"`
	Title      string           `json:"title"`
	Text       string           `json:"text"`
	Items      []string         `json:"items"`
	ItemTitles []string         `json:"itemTitles,omitempty"`
	Photos     []photoDTO       `json:"photos"`
	Videos     []pageVideoDTO   `json:"videos"`
	Links      []string         `json:"links"`
	Contacts   *pageContactsDTO `json:"contacts,omitempty"`
}

type pageRevisionDTO struct {
	Status       string     `json:"status"`
	RejectReason string     `json:"rejectReason"`
	SubmittedAt  *time.Time `json:"submittedAt"`
	Page         pageDTO    `json:"page"`
}

type pageDTO struct {
	ID           int64             `json:"id"`
	UserID       int64             `json:"userId"` // владелец (жалоба на профиль, анкета автора)
	Kind         string            `json:"kind"`
	Slug         string            `json:"slug"`
	Status       string            `json:"status"`
	RejectReason string            `json:"rejectReason"`
	Title        string            `json:"title"`
	City         string            `json:"city"`
	Spheres      []string          `json:"spheres"`
	SphereTitles []string          `json:"sphereTitles"`
	Logo         *photoDTO         `json:"logo"`
	Cover        *photoDTO         `json:"cover"`
	Blocks       []pageBlockDTO    `json:"blocks"`
	UpdatedAt    time.Time         `json:"updatedAt"`
	SubmittedAt  *time.Time        `json:"submittedAt"`
	PublishedAt  *time.Time        `json:"publishedAt"`
	ReviewNotes  map[string]string `json:"reviewNotes,omitempty"`
	Revision     *pageRevisionDTO  `json:"revision,omitempty"`
}

type pageOut struct {
	Page          *pageDTO `json:"page"`
	SuggestedSlug string   `json:"suggestedSlug,omitempty"`
}

type publicPageOut struct {
	Page      *pageDTO       `json:"page,omitempty"`
	MovedTo   string         `json:"movedTo,omitempty"`
	Tasks     []taskDTO      `json:"tasks,omitempty"`
	Platforms *[]platformDTO `json:"platforms,omitempty"`
	Card      *cardDTO       `json:"card,omitempty"`
}

type videoDTO struct {
	ID    string `json:"id"`
	Src   string `json:"src"`
	Bytes int64  `json:"bytes"`
}

// videoURL — адрес файла видео.
func videoURL(id string) string { return "/media/" + id + "/video" }

func (in pageIn) toService() service.PageInput {
	out := service.PageInput{Slug: in.Slug, Title: in.Title, City: in.City, Spheres: in.Spheres, Logo: in.Logo, Cover: in.Cover,
		Blocks: make([]service.PageBlockInput, 0, len(in.Blocks))}
	for _, b := range in.Blocks {
		nb := service.PageBlockInput{Type: b.Type, Title: b.Title, Text: b.Text, Items: b.Items, Photos: b.Photos, Links: b.Links}
		for _, v := range b.Videos {
			nb.Videos = append(nb.Videos, service.VideoInput{Media: v.Media, Poster: v.Poster, URL: v.URL})
		}
		if c := b.Contacts; c != nil {
			nb.Contacts = &domain.PageContacts{City: c.City, Address: c.Address, Hours: c.Hours, Site: c.Site, Phone: c.Phone, Socials: c.Socials}
		}
		out.Blocks = append(out.Blocks, nb)
	}
	return out
}

func toPageVideoDTO(v domain.PageVideo) pageVideoDTO {
	if v.Source == domain.VideoFile {
		return pageVideoDTO{Source: v.Source, Media: v.Media, Src: videoURL(v.Media), Poster: photo(v.Poster)}
	}
	return pageVideoDTO{Source: v.Source, URL: v.WatchURL(), EmbedURL: v.EmbedURL()}
}

func toPageBlocksDTO(bs []domain.PageBlock) []pageBlockDTO {
	out := make([]pageBlockDTO, 0, len(bs))
	for _, b := range bs {
		nb := pageBlockDTO{Type: b.Type, Title: b.Title, Text: b.Text, Items: nonNilStr(b.Items), ItemTitles: b.ItemTitles, Photos: photos(b.Photos),
			Videos: make([]pageVideoDTO, 0, len(b.Videos)), Links: nonNilStr(b.Links)}
		for _, v := range b.Videos {
			nb.Videos = append(nb.Videos, toPageVideoDTO(v))
		}
		if c := b.Contacts; c != nil {
			nb.Contacts = &pageContactsDTO{City: c.City, Address: c.Address, Hours: c.Hours, Site: c.Site, Phone: c.Phone, Socials: nonNilStr(c.Socials)}
		}
		out = append(out, nb)
	}
	return out
}

func toPageDTO(p *domain.Page) *pageDTO {
	if p == nil {
		return nil
	}
	out := &pageDTO{
		ID: p.ID, UserID: p.UserID, Kind: string(p.Kind), Slug: p.Slug, Status: string(p.Status), RejectReason: p.RejectReason, Title: p.Title,
		City: p.City, Spheres: nonNilStr(p.Spheres), SphereTitles: nonNilStr(p.SphereTitles), Logo: photo(p.Logo),
		Cover: photo(p.Cover), Blocks: toPageBlocksDTO(p.Blocks), UpdatedAt: p.UpdatedAt.UTC(),
		SubmittedAt: utcPtr(p.SubmittedAt), PublishedAt: utcPtr(p.PublishedAt),
	}
	if len(p.ReviewNotes) > 0 {
		out.ReviewNotes = p.ReviewNotes
	}
	if p.Revision != nil {
		rev := *p.Revision
		rev.ID, rev.UserID, rev.Kind, rev.Slug, rev.Status, rev.UpdatedAt = p.ID, p.UserID, p.Kind, p.Slug, p.RevisionStatus, p.UpdatedAt
		out.Revision = &pageRevisionDTO{Status: string(p.RevisionStatus), RejectReason: p.RevisionReason,
			SubmittedAt: utcPtr(p.RevisionSubmittedAt), Page: *toPageDTO(&rev)}
	}
	return out
}

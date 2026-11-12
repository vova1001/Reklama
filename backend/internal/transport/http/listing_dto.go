package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// Задания, анкеты, категории и фото — строго по api/openapi.yaml.

type blockDTO struct {
	Kind  string   `json:"kind"`
	Title string   `json:"title"`
	Items []string `json:"items"`
}

type taskIn struct {
	Title         string     `json:"title"`
	City          string     `json:"city"`
	Nationwide    bool       `json:"nationwide"`
	Categories    []string   `json:"categories"`
	Formats       []string   `json:"formats"`
	Access        string     `json:"access"`
	MinViews      int64      `json:"minViews"`
	RewardKopecks int64      `json:"rewardKopecks"`
	PublishBy     string     `json:"publishBy"`
	PublishTime   string     `json:"publishTime"`
	Blocks        []blockDTO `json:"blocks"`
	Cover         string     `json:"cover"`
	Photos        []string   `json:"photos"`
}

type cardIn struct {
	Name      string          `json:"name"`
	City      string          `json:"city"`
	Topics    []string        `json:"topics"`
	Platforms json.RawMessage `json:"platforms,omitempty"` // прежние формы присылали площадки — не принимаются, игнорируются
	Blocks    []blockDTO      `json:"blocks"`
	Avatar    string          `json:"avatar"`
	Works     []string        `json:"works"`
}

type photoDTO struct {
	ID    string `json:"id"`
	Thumb string `json:"thumb"` // 320
	Src   string `json:"src"`   // 800
	Large string `json:"large"` // 1600
}

type taskDTO struct {
	ID             int64      `json:"id"`
	Status         string     `json:"status"`
	RejectReason   string     `json:"rejectReason"`
	Title          string     `json:"title"`
	City           string     `json:"city"`
	Nationwide     bool       `json:"nationwide"`
	Categories     []string   `json:"categories"`
	CategoryTitles []string   `json:"categoryTitles,omitempty"`
	Formats        []string   `json:"formats"`
	Access         string     `json:"access"`
	MinViews       int64      `json:"minViews"`
	RewardKopecks  int64      `json:"rewardKopecks"`
	PublishBy      *string    `json:"publishBy"`
	PublishTime    string     `json:"publishTime"`
	DeadlineAt     *time.Time `json:"deadlineAt"`
	Blocks         []blockDTO `json:"blocks"`
	Cover          *photoDTO  `json:"cover"`
	Photos         []photoDTO `json:"photos"`
	Brand          string     `json:"brand,omitempty"`
	BrandSlug      string     `json:"brandSlug,omitempty"` // адрес страницы бизнеса /b/<slug>
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	SubmittedAt    *time.Time `json:"submittedAt"`
	PublishedAt    *time.Time `json:"publishedAt"`
	// ReviewNotes — замечания модератора по полям (на доработке); Revision — правка опубликованного.
	ReviewNotes map[string]string `json:"reviewNotes,omitempty"`
	Revision    *taskRevisionDTO  `json:"revision,omitempty"`
}

// taskRevisionDTO — правка опубликованного задания: новая версия ждёт проверки, опубликованная видна всем.
type taskRevisionDTO struct {
	Status       string     `json:"status"`
	RejectReason string     `json:"rejectReason"`
	SubmittedAt  *time.Time `json:"submittedAt"`
	Task         taskDTO    `json:"task"`
}

type cardRevisionDTO struct {
	Status       string     `json:"status"`
	RejectReason string     `json:"rejectReason"`
	SubmittedAt  *time.Time `json:"submittedAt"`
	Card         cardDTO    `json:"card"`
}

type tasksOut struct {
	Tasks []taskDTO `json:"tasks"`
}

type feedOut struct {
	Tasks []taskDTO `json:"tasks"`
	Next  *string   `json:"next"`
}

// platformDTO — подтверждённая соцсеть в анкете (цифры от площадки).
type platformDTO struct {
	Kind            string     `json:"kind"`
	Type            string     `json:"type"`
	Handle          string     `json:"handle"`
	Title           string     `json:"title"`
	URL             string     `json:"url"`
	Followers       int64      `json:"followers"`
	FollowersHidden bool       `json:"followersHidden"`
	Verified        bool       `json:"verified"`
	RefreshedAt     *time.Time `json:"refreshedAt"`
}

type cardDTO struct {
	ID           int64             `json:"id"` // id автора — адрес страницы анкеты
	Status       string            `json:"status"`
	RejectReason string            `json:"rejectReason"`
	Name         string            `json:"name"`
	City         string            `json:"city"`
	Topics       []string          `json:"topics"`
	TopicTitles  []string          `json:"topicTitles,omitempty"`
	PageSlug     string            `json:"pageSlug,omitempty"` // адрес профиля автора /c/<slug>
	Platforms    []platformDTO     `json:"platforms"`
	Blocks       []blockDTO        `json:"blocks"`
	Avatar       *photoDTO         `json:"avatar"`
	Works        []photoDTO        `json:"works"`
	UpdatedAt    time.Time         `json:"updatedAt"`
	SubmittedAt  *time.Time        `json:"submittedAt"`
	PublishedAt  *time.Time        `json:"publishedAt"`
	ReviewNotes  map[string]string `json:"reviewNotes,omitempty"`
	Revision     *cardRevisionDTO  `json:"revision,omitempty"`
}

type cardOut struct {
	Card *cardDTO `json:"card"`
}

type cardFeedOut struct {
	Cards []cardDTO `json:"cards"`
	Next  *string   `json:"next"`
}

type categoryItemDTO struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Icon   string `json:"icon"`
	Status string `json:"status"` // approved | pending_review | rejected
	Own    bool   `json:"own"`
}

type categoriesOut struct {
	Common []categoryItemDTO `json:"common"`
	Own    []categoryItemDTO `json:"own"`
}

type similarOut struct {
	Items []categoryItemDTO `json:"items"`
}

type formatItemDTO struct {
	ID      string `json:"id"` // slug из списка или своё название — так и передаётся в formats
	Title   string `json:"title"`
	Builtin bool   `json:"builtin"`
	Own     bool   `json:"own"`
}

type formatsOut struct {
	Items []formatItemDTO `json:"items"`
}

type categoryIn struct {
	Title string `json:"title"`
}

type categoryCreated struct {
	Category categoryItemDTO `json:"category"`
	Created  bool            `json:"created"`
}

type mediaDTO struct {
	photoDTO
	Purpose string `json:"purpose"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

type avatarIn struct {
	MediaID *string `json:"mediaId"`
}

type avatarOut struct {
	AvatarURL string `json:"avatarUrl"`
}

func blocksIn(in []blockDTO) []domain.Block {
	out := make([]domain.Block, 0, len(in))
	for _, b := range in {
		out = append(out, domain.Block{Kind: b.Kind, Title: b.Title, Items: b.Items})
	}
	return out
}

func toBlocksDTO(bs []domain.Block) []blockDTO {
	out := make([]blockDTO, 0, len(bs))
	for _, b := range bs {
		out = append(out, blockDTO{Kind: b.Kind, Title: b.Title, Items: nonNilStr(b.Items)})
	}
	return out
}

func (in taskIn) toService() service.TaskInput {
	return service.TaskInput{
		Title: in.Title, City: in.City, Nationwide: in.Nationwide, Categories: in.Categories, Formats: in.Formats,
		Access: in.Access, MinViews: in.MinViews, RewardKopecks: in.RewardKopecks,
		PublishBy: in.PublishBy, PublishTime: in.PublishTime, Blocks: blocksIn(in.Blocks), Cover: in.Cover, Photos: in.Photos,
	}
}

func (in cardIn) toService() service.CardInput {
	return service.CardInput{Name: in.Name, City: in.City, Topics: in.Topics, Blocks: blocksIn(in.Blocks), Avatar: in.Avatar, Works: in.Works}
}

func photo(id string) *photoDTO {
	if id == "" {
		return nil
	}
	return &photoDTO{ID: id, Thumb: service.MediaURL(id, 320), Src: service.MediaURL(id, 800), Large: service.MediaURL(id, 1600)}
}

func photos(ids []string) []photoDTO {
	out := make([]photoDTO, 0, len(ids))
	for _, id := range ids {
		out = append(out, *photo(id))
	}
	return out
}

func toTaskDTO(t domain.Task) taskDTO {
	var by *string
	if t.PublishBy != nil {
		s := t.PublishBy.Format(time.DateOnly)
		by = &s
	}
	out := taskDTO{
		ID: t.ID, Status: string(t.Status), RejectReason: t.RejectReason, Title: t.Title, City: t.City, Nationwide: t.Nationwide,
		Categories: nonNilStr(t.Categories), CategoryTitles: t.CategoryTitles, Formats: nonNilStr(t.Formats), Access: t.Access,
		MinViews: t.MinViews, RewardKopecks: t.RewardKopecks, PublishBy: by, PublishTime: t.PublishTime, DeadlineAt: utcPtr(t.DeadlineAt), Blocks: toBlocksDTO(t.Blocks),
		Cover: photo(t.Cover), Photos: photos(t.Photos), Brand: t.Brand, BrandSlug: t.BrandSlug,
		CreatedAt: t.CreatedAt.UTC(), UpdatedAt: t.UpdatedAt.UTC(), SubmittedAt: utcPtr(t.SubmittedAt), PublishedAt: utcPtr(t.PublishedAt),
	}
	if len(t.ReviewNotes) > 0 {
		out.ReviewNotes = t.ReviewNotes
	}
	if t.Revision != nil {
		rev := *t.Revision
		rev.ID, rev.OwnerID, rev.Status = t.ID, t.OwnerID, t.RevisionStatus
		rev.CreatedAt, rev.UpdatedAt = t.CreatedAt, t.UpdatedAt
		out.Revision = &taskRevisionDTO{Status: string(t.RevisionStatus), RejectReason: t.RevisionReason,
			SubmittedAt: utcPtr(t.RevisionSubmittedAt), Task: toTaskDTO(rev)}
	}
	return out
}

func toTasksDTO(ts []domain.Task) []taskDTO {
	out := make([]taskDTO, 0, len(ts))
	for _, t := range ts {
		out = append(out, toTaskDTO(t))
	}
	return out
}

func toPlatformsDTO(in []domain.Platform) []platformDTO {
	ps := make([]platformDTO, 0, len(in))
	for _, p := range in {
		ps = append(ps, platformDTO{Kind: p.Kind, Type: p.Type, Handle: p.Handle, Title: p.Title, URL: p.URL,
			Followers: p.Followers, FollowersHidden: p.FollowersHidden, Verified: p.Verified, RefreshedAt: utcPtr(p.RefreshedAt)})
	}
	return ps
}

func toCardDTO(c *domain.CreatorCard) *cardDTO {
	if c == nil {
		return nil
	}
	ps := toPlatformsDTO(c.Platforms)
	out := &cardDTO{
		ID: c.UserID, TopicTitles: c.TopicTitles, PageSlug: c.PageSlug, PublishedAt: utcPtr(c.PublishedAt),
		Status: string(c.Status), RejectReason: c.RejectReason, Name: c.Name, City: c.City, Topics: nonNilStr(c.Topics),
		Platforms: ps, Blocks: toBlocksDTO(c.Blocks), Avatar: photo(c.Avatar), Works: photos(c.Works),
		UpdatedAt: c.UpdatedAt.UTC(), SubmittedAt: utcPtr(c.SubmittedAt),
	}
	if len(c.ReviewNotes) > 0 {
		out.ReviewNotes = c.ReviewNotes
	}
	if c.Revision != nil {
		rev := *c.Revision
		rev.UserID, rev.Status, rev.Platforms, rev.UpdatedAt = c.UserID, c.RevisionStatus, c.Platforms, c.UpdatedAt
		out.Revision = &cardRevisionDTO{Status: string(c.RevisionStatus), RejectReason: c.RevisionReason,
			SubmittedAt: utcPtr(c.RevisionSubmittedAt), Card: *toCardDTO(&rev)}
	}
	return out
}

func toCategoryItems(cs []domain.CategoryInfo) []categoryItemDTO {
	out := make([]categoryItemDTO, 0, len(cs))
	for _, c := range cs {
		out = append(out, categoryItemDTO{Slug: c.Slug, Title: c.Title, Icon: c.Icon, Status: c.Status, Own: c.Own()})
	}
	return out
}

func toMediaDTO(m domain.Media) mediaDTO {
	return mediaDTO{photoDTO: *photo(m.ID), Purpose: m.Purpose, Width: m.Width, Height: m.Height}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

// Курсор ленты — непрозрачная строка: base64url("<unix-нс>:<id>").
func encodeCursor(c *domain.FeedCursor) *string {
	if c == nil {
		return nil
	}
	s := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(c.PublishedAt.UnixNano(), 10) + ":" + strconv.FormatInt(c.ID, 10)))
	return &s
}

func decodeCursor(s string) (*domain.FeedCursor, bool) {
	if s == "" {
		return nil, true
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, false
	}
	ns, id, ok := strings.Cut(string(raw), ":")
	if !ok {
		return nil, false
	}
	n, err1 := strconv.ParseInt(ns, 10, 64)
	i, err2 := strconv.ParseInt(id, 10, 64)
	if err1 != nil || err2 != nil || i <= 0 {
		return nil, false
	}
	return &domain.FeedCursor{PublishedAt: time.Unix(0, n).UTC(), ID: i}, true
}

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// Pages — публичные страницы (витрина бизнеса, профиль автора), их адреса и проверка. SQL — только в
// queries/pages.sql. Смена статуса и запись в журнал — одной транзакцией и только из ожидаемого статуса; файлы
// прикрепляются к странице в той же транзакции, что и сохранение. Колонка media — файлы опубликованного
// содержимого: по ней решается, видно ли файл всем (правка видна только владельцу, пока её не одобрят).
type Pages struct {
	db DB
	q  *store.Queries
}

// NewPages создаёт репозиторий страниц.
func NewPages(db DB) *Pages { return &Pages{db: db, q: store.New(db)} }

// Page — страница пользователя этого вида; нет — ErrNotFound.
func (r *Pages) Page(ctx context.Context, userID int64, kind domain.PageKind) (domain.Page, error) {
	row, err := r.q.GetPage(ctx, store.GetPageParams{UserID: userID, Kind: string(kind)})
	if err != nil {
		return domain.Page{}, dbErr("страница", err)
	}
	return pageOf(row)
}

// PageByID — страница по id (проверка); нет — ErrNotFound.
func (r *Pages) PageByID(ctx context.Context, id int64) (domain.Page, error) {
	row, err := r.q.GetPageByID(ctx, id)
	if err != nil {
		return domain.Page{}, dbErr("страница", err)
	}
	return pageOf(row)
}

// CreatePage — новая страница-черновик с адресом p.Slug; адрес занят (сейчас или раньше) — ErrConflict, страница
// уже есть — ErrConflict.
func (r *Pages) CreatePage(ctx context.Context, p domain.Page) (domain.Page, error) {
	blocks, err := pageBlocksJSON(p.Blocks)
	if err != nil {
		return domain.Page{}, err
	}
	var row store.ProfilePage
	err = inTx(ctx, r.db, r.q, func(q *store.Queries) error {
		var err error
		row, err = q.CreatePage(ctx, store.CreatePageParams{
			UserID: p.UserID, Kind: string(p.Kind), Slug: p.Slug, Title: p.Title, City: p.City, Spheres: nonNil(p.Spheres),
			Logo: text(p.Logo), Cover: text(p.Cover), Blocks: blocks, Media: nonNil(domain.PageMedia(p)),
		})
		if err != nil {
			return err
		}
		if _, err := q.ClaimPageSlug(ctx, store.ClaimPageSlugParams{Kind: string(p.Kind), Slug: p.Slug, PageID: row.ID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrConflict
			}
			return err
		}
		return attach(ctx, q, p.UserID, domain.SubjectPage, row.ID, domain.PageMedia(p))
	})
	if errors.Is(err, domain.ErrConflict) {
		return domain.Page{}, err
	}
	if err != nil {
		return domain.Page{}, dbErr("новая страница", err)
	}
	return pageOf(row)
}

// SavePage — содержимое страницы (только черновик, отклонённая, на доработке; иначе — ErrConflict).
func (r *Pages) SavePage(ctx context.Context, p domain.Page) (domain.Page, error) {
	blocks, err := pageBlocksJSON(p.Blocks)
	if err != nil {
		return domain.Page{}, err
	}
	var row store.ProfilePage
	err = inTx(ctx, r.db, r.q, func(q *store.Queries) error {
		var err error
		row, err = q.UpdatePageContent(ctx, store.UpdatePageContentParams{
			ID: p.ID, Title: p.Title, City: p.City, Spheres: nonNil(p.Spheres), Logo: text(p.Logo), Cover: text(p.Cover),
			Blocks: blocks, Media: nonNil(domain.PageMedia(p)),
		})
		if err != nil {
			return err
		}
		return attach(ctx, q, row.UserID, domain.SubjectPage, row.ID, domain.PageMedia(p))
	})
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, domain.ErrConflict) {
		return domain.Page{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Page{}, dbErr("сохранение страницы", err)
	}
	return pageOf(row)
}

// SetPageSlug — сменить адрес: прежние остаются за страницей (ведут на новый). Занят другим — ErrConflict;
// адресов у страницы уже maxSlugs (а новый — не из прежних) — domain.ErrSlugLimit.
func (r *Pages) SetPageSlug(ctx context.Context, id int64, kind domain.PageKind, slug string, maxSlugs int) (domain.Page, error) {
	var row store.ProfilePage
	err := inTx(ctx, r.db, r.q, func(q *store.Queries) error {
		n, err := q.CountPageSlugs(ctx, id)
		if err != nil {
			return err
		}
		taken, err := q.PageSlugTaken(ctx, store.PageSlugTakenParams{Kind: string(kind), Slug: slug})
		if err != nil {
			return err
		}
		if !taken && int(n) >= maxSlugs {
			return domain.ErrSlugLimit
		}
		if _, err := q.ClaimPageSlug(ctx, store.ClaimPageSlugParams{Kind: string(kind), Slug: slug, PageID: id}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrConflict
			}
			return err
		}
		row, err = q.SetPageSlug(ctx, store.SetPageSlugParams{ID: id, Slug: slug})
		return err
	})
	switch {
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrSlugLimit):
		return domain.Page{}, err
	case err != nil:
		return domain.Page{}, dbErr("адрес страницы", err)
	}
	return pageOf(row)
}

// SlugTaken — адрес занят (нынешний или прежний какой-либо страницы этого вида).
func (r *Pages) SlugTaken(ctx context.Context, kind domain.PageKind, slug string) (bool, error) {
	ok, err := r.q.PageSlugTaken(ctx, store.PageSlugTakenParams{Kind: string(kind), Slug: slug})
	return ok, dbErr("адрес страницы", err)
}

// SetPageStatus — смены статуса по цепочке и записи в журнал — одной транзакцией; статус уже другой —
// ErrConflict, ничего не меняется.
func (r *Pages) SetPageStatus(ctx context.Context, chain ...domain.StatusChange) (domain.Page, error) {
	if len(chain) == 0 {
		return domain.Page{}, errors.New("статус страницы: пустая цепочка")
	}
	var row store.ProfilePage
	err := inTx(ctx, r.db, r.q, func(q *store.Queries) error {
		for _, ch := range chain {
			var err error
			row, err = q.SetPageStatus(ctx, store.SetPageStatusParams{
				ID: ch.SubjectID, FromStatus: string(ch.From), ToStatus: string(ch.To), Reason: ch.Reason, Notes: notesJSON(ch.Notes),
			})
			if err != nil {
				return err
			}
			if err := q.LogStatusChange(ctx, logParams(domain.SubjectPage, ch)); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Page{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Page{}, dbErr("статус страницы", err)
	}
	return pageOf(row)
}

// SavePageRevision — правка опубликованной страницы (опубликованная видна, пока правку не одобрят); файлы правки
// прикрепляются, но не открываются всем.
func (r *Pages) SavePageRevision(ctx context.Context, id int64, rev domain.Page) (domain.Page, error) {
	raw, err := pageContentJSON(rev)
	if err != nil {
		return domain.Page{}, err
	}
	var row store.ProfilePage
	err = inTx(ctx, r.db, r.q, func(q *store.Queries) error {
		var err error
		if row, err = q.SavePageRevision(ctx, store.SavePageRevisionParams{ID: id, Revision: raw}); err != nil {
			return err
		}
		live, err := pageOf(row)
		if err != nil {
			return err
		}
		keep := union(domain.PageMedia(live), domain.PageMedia(rev))
		if err := q.DetachMedia(ctx, store.DetachMediaParams{Subject: text(string(domain.SubjectPage)), SubjectID: pgInt8(id), Keep: keep}); err != nil {
			return err
		}
		return attachOnly(ctx, q, row.UserID, domain.SubjectPage, id, domain.PageMedia(rev))
	})
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, domain.ErrConflict) {
		return domain.Page{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Page{}, dbErr("правка страницы", err)
	}
	return pageOf(row)
}

// SetPageRevision — смена статуса правки и запись в журнал.
func (r *Pages) SetPageRevision(ctx context.Context, ch domain.StatusChange) (domain.Page, error) {
	var row store.ProfilePage
	err := inTx(ctx, r.db, r.q, func(q *store.Queries) error {
		var err error
		row, err = q.SetPageRevisionStatus(ctx, store.SetPageRevisionStatusParams{
			ID: ch.SubjectID, FromStatus: string(ch.From), ToStatus: string(ch.To), Reason: ch.Reason, Notes: notesJSON(ch.Notes),
		})
		if err != nil {
			return err
		}
		return q.LogStatusChange(ctx, logParams(domain.SubjectPage, revisionLog(ch)))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Page{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Page{}, dbErr("статус правки страницы", err)
	}
	return pageOf(row)
}

// DropPageRevision — удалить правку (файлы правки откреплены; через сутки их удалит очистка).
func (r *Pages) DropPageRevision(ctx context.Context, id int64) (domain.Page, error) {
	var row store.ProfilePage
	err := inTx(ctx, r.db, r.q, func(q *store.Queries) error {
		var err error
		if row, err = q.DropPageRevision(ctx, id); err != nil {
			return err
		}
		live, err := pageOf(row)
		if err != nil {
			return err
		}
		return q.DetachMedia(ctx, store.DetachMediaParams{Subject: text(string(domain.SubjectPage)), SubjectID: pgInt8(id), Keep: nonNil(domain.PageMedia(live))})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Page{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Page{}, dbErr("удаление правки страницы", err)
	}
	return pageOf(row)
}

// ApplyPageRevision — заменить опубликованное содержимое правкой (одобрение — log; снятие с публикации с правкой —
// ещё и chain) одной транзакцией; статус или статус правки уже другой — ErrConflict.
func (r *Pages) ApplyPageRevision(ctx context.Context, id int64, revStatus domain.Status, log domain.StatusChange, chain ...domain.StatusChange) (domain.Page, error) {
	var row store.ProfilePage
	err := inTx(ctx, r.db, r.q, func(q *store.Queries) error {
		cur, err := q.GetPageByID(ctx, id)
		if err != nil {
			return err
		}
		if cur.Status != string(domain.StatusPublished) || !cur.RevisionStatus.Valid || cur.RevisionStatus.String != string(revStatus) {
			return pgx.ErrNoRows
		}
		rev, err := pageContentOf(cur.Revision)
		if err != nil {
			return err
		}
		blocks, err := pageBlocksJSON(rev.Blocks)
		if err != nil {
			return err
		}
		row, err = q.ReplacePageContent(ctx, store.ReplacePageContentParams{
			ID: id, Status: string(domain.StatusPublished), RevisionStatus: string(revStatus),
			Title: rev.Title, City: rev.City, Spheres: nonNil(rev.Spheres), Logo: text(rev.Logo), Cover: text(rev.Cover),
			Blocks: blocks, Media: nonNil(domain.PageMedia(rev)),
		})
		if err != nil {
			return err
		}
		if err := q.LogStatusChange(ctx, logParams(domain.SubjectPage, revisionLog(log))); err != nil {
			return err
		}
		for _, ch := range chain {
			if row, err = q.SetPageStatus(ctx, store.SetPageStatusParams{ID: id, FromStatus: string(ch.From), ToStatus: string(ch.To), Reason: ch.Reason, Notes: notesJSON(ch.Notes)}); err != nil {
				return err
			}
			if err := q.LogStatusChange(ctx, logParams(domain.SubjectPage, ch)); err != nil {
				return err
			}
		}
		return attach(ctx, q, row.UserID, domain.SubjectPage, id, domain.PageMedia(rev))
	})
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, domain.ErrConflict) {
		return domain.Page{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Page{}, dbErr("применение правки страницы", err)
	}
	return pageOf(row)
}

// PublishedPage — опубликованная страница по адресу (нынешнему или прежнему — тогда Slug страницы другой);
// нет, не опубликована или владелец заблокирован — ErrNotFound.
func (r *Pages) PublishedPage(ctx context.Context, kind domain.PageKind, slug string) (domain.Page, error) {
	row, err := r.q.GetPublishedPageBySlug(ctx, store.GetPublishedPageBySlugParams{Kind: string(kind), Slug: slug})
	if err != nil {
		return domain.Page{}, dbErr("опубликованная страница", err)
	}
	p, err := pageOf(store.ProfilePage{
		ID: row.ID, UserID: row.UserID, Kind: row.Kind, Slug: row.Slug, Status: row.Status, RejectReason: row.RejectReason,
		Title: row.Title, City: row.City, Spheres: row.Spheres, Logo: row.Logo, Cover: row.Cover, Blocks: row.Blocks,
		Media: row.Media, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, SubmittedAt: row.SubmittedAt, PublishedAt: row.PublishedAt,
	})
	if err != nil {
		return domain.Page{}, err
	}
	p.SphereTitles = nonNil(row.SphereTitles)
	return p, nil
}

// PublishedSlugs — адреса опубликованных страниц вида kind у пользователей (нет страницы — нет в ответе).
func (r *Pages) PublishedSlugs(ctx context.Context, kind domain.PageKind, userIDs []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := r.q.PublishedPageSlugs(ctx, store.PublishedPageSlugsParams{Kind: string(kind), UserIds: userIDs})
	if err != nil {
		return nil, dbErr("адреса страниц", err)
	}
	for _, x := range rows {
		out[x.UserID] = x.Slug
	}
	return out, nil
}

// OpenTasks — открытые задания бизнеса (опубликованные, срок не прошёл), новые сверху.
func (r *Pages) OpenTasks(ctx context.Context, ownerID int64, limit int) ([]domain.Task, error) {
	rows, err := r.q.ListOwnerOpenTasks(ctx, store.ListOwnerOpenTasksParams{OwnerID: ownerID, Lim: int32(limit)})
	if err != nil {
		return nil, dbErr("открытые задания", err)
	}
	out := make([]domain.Task, 0, len(rows))
	for _, row := range rows {
		t, err := taskOf(store.Task{
			ID: row.ID, OwnerID: row.OwnerID, Status: row.Status, Title: row.Title, City: row.City, Nationwide: row.Nationwide,
			Categories: row.Categories, Formats: row.Formats, Access: row.Access, MinViews: row.MinViews,
			RewardKopecks: row.RewardKopecks, PublishBy: row.PublishBy, PublishTime: row.PublishTime, DeadlineAt: row.DeadlineAt,
			Blocks: row.Blocks, Cover: row.Cover, Photos: row.Photos, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			SubmittedAt: row.SubmittedAt, PublishedAt: row.PublishedAt,
		})
		if err != nil {
			return nil, err
		}
		t.CategoryTitles = nonNil(row.CategoryTitles)
		out = append(out, t)
	}
	return out, nil
}

// CategoryTitles — названия сфер и тем по slug (нет — нет в ответе).
func (r *Pages) CategoryTitles(ctx context.Context, slugs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(slugs) == 0 {
		return out, nil
	}
	rows, err := r.q.CategoryTitlesBySlug(ctx, slugs)
	if err != nil {
		return nil, dbErr("названия тем", err)
	}
	for _, x := range rows {
		out[x.Slug] = x.Title
	}
	return out, nil
}

// ===== преобразования (теги JSON — только здесь, домен о хранении не знает) =====

type pageVideoRow struct {
	Source string `json:"source"`
	Media  string `json:"media,omitempty"`
	Poster string `json:"poster,omitempty"`
	ID     string `json:"id,omitempty"`
	OID    string `json:"oid,omitempty"`
	Hash   string `json:"hash,omitempty"`
}

type pageContactsRow struct {
	City    string   `json:"city,omitempty"`
	Address string   `json:"address,omitempty"`
	Hours   string   `json:"hours,omitempty"`
	Site    string   `json:"site,omitempty"`
	Phone   string   `json:"phone,omitempty"`
	Socials []string `json:"socials,omitempty"`
}

type pageBlockRow struct {
	Type     string           `json:"type"`
	Title    string           `json:"title,omitempty"`
	Text     string           `json:"text,omitempty"`
	Items    []string         `json:"items,omitempty"`
	Photos   []string         `json:"photos,omitempty"`
	Videos   []pageVideoRow   `json:"videos,omitempty"`
	Links    []string         `json:"links,omitempty"`
	Contacts *pageContactsRow `json:"contacts,omitempty"`
}

type pageContentRow struct {
	Title   string         `json:"title"`
	City    string         `json:"city"`
	Spheres []string       `json:"spheres"`
	Logo    string         `json:"logo,omitempty"`
	Cover   string         `json:"cover,omitempty"`
	Blocks  []pageBlockRow `json:"blocks"`
}

func pageBlockRows(bs []domain.PageBlock) []pageBlockRow {
	rows := make([]pageBlockRow, 0, len(bs))
	for _, b := range bs {
		row := pageBlockRow{Type: b.Type, Title: b.Title, Text: b.Text, Items: b.Items, Photos: b.Photos, Links: b.Links}
		for _, v := range b.Videos {
			row.Videos = append(row.Videos, pageVideoRow(v))
		}
		if c := b.Contacts; c != nil {
			row.Contacts = &pageContactsRow{City: c.City, Address: c.Address, Hours: c.Hours, Site: c.Site, Phone: c.Phone, Socials: c.Socials}
		}
		rows = append(rows, row)
	}
	return rows
}

func pageBlocksFromRows(rows []pageBlockRow) []domain.PageBlock {
	out := make([]domain.PageBlock, 0, len(rows))
	for _, r := range rows {
		b := domain.PageBlock{Type: r.Type, Title: r.Title, Text: r.Text, Items: nonNil(r.Items), Photos: nonNil(r.Photos),
			Links: nonNil(r.Links), Videos: []domain.PageVideo{}}
		for _, v := range r.Videos {
			b.Videos = append(b.Videos, domain.PageVideo(v))
		}
		if c := r.Contacts; c != nil {
			b.Contacts = &domain.PageContacts{City: c.City, Address: c.Address, Hours: c.Hours, Site: c.Site, Phone: c.Phone, Socials: nonNil(c.Socials)}
		}
		out = append(out, b)
	}
	return out
}

func pageBlocksJSON(bs []domain.PageBlock) ([]byte, error) {
	b, err := json.Marshal(pageBlockRows(bs))
	if err != nil {
		return nil, fmt.Errorf("блоки страницы в JSON: %w", err)
	}
	return b, nil
}

func pageContentJSON(p domain.Page) ([]byte, error) {
	b, err := json.Marshal(pageContentRow{Title: p.Title, City: p.City, Spheres: nonNil(p.Spheres), Logo: p.Logo, Cover: p.Cover, Blocks: pageBlockRows(p.Blocks)})
	if err != nil {
		return nil, fmt.Errorf("правка страницы в JSON: %w", err)
	}
	return b, nil
}

func pageContentOf(raw []byte) (domain.Page, error) {
	var r pageContentRow
	if err := json.Unmarshal(raw, &r); err != nil {
		return domain.Page{}, fmt.Errorf("правка страницы из JSON: %w", err)
	}
	return domain.Page{Title: r.Title, City: r.City, Spheres: nonNil(r.Spheres), Logo: r.Logo, Cover: r.Cover, Blocks: pageBlocksFromRows(r.Blocks)}, nil
}

func pageOf(p store.ProfilePage) (domain.Page, error) {
	var rows []pageBlockRow
	if len(p.Blocks) > 0 {
		if err := json.Unmarshal(p.Blocks, &rows); err != nil {
			return domain.Page{}, fmt.Errorf("блоки страницы из JSON: %w", err)
		}
	}
	out := domain.Page{
		ID: p.ID, UserID: p.UserID, Kind: domain.PageKind(p.Kind), Slug: p.Slug, Status: domain.Status(p.Status),
		RejectReason: p.RejectReason, Title: p.Title, City: p.City, Spheres: nonNil(p.Spheres), Logo: p.Logo.String,
		Cover: p.Cover.String, Blocks: pageBlocksFromRows(rows), SphereTitles: []string{}, CreatedAt: p.CreatedAt.Time,
		UpdatedAt: p.UpdatedAt.Time, SubmittedAt: tsPtr(p.SubmittedAt), PublishedAt: tsPtr(p.PublishedAt),
		ReviewNotes: notesOf(p.ReviewNotes),
	}
	if p.RevisionStatus.Valid && len(p.Revision) > 0 {
		rev, err := pageContentOf(p.Revision)
		if err != nil {
			return domain.Page{}, err
		}
		rev.Kind, rev.Slug = out.Kind, out.Slug
		out.Revision, out.RevisionStatus = &rev, domain.Status(p.RevisionStatus.String)
		out.RevisionReason, out.RevisionSubmittedAt = p.RevisionReason, tsPtr(p.RevisionSubmittedAt)
	}
	return out, nil
}

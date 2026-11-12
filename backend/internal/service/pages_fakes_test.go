package service

import (
	"context"
	"sort"
	"sync"

	"ohvat/backend/internal/domain"
)

// memPages — страницы в памяти (PageRepository + PageModeration). Журнал — общий с memListings (лимит отправок).
type memPages struct {
	mu     sync.Mutex
	pages  map[int64]*domain.Page
	slugs  map[string]int64 // kind/slug → id страницы
	tasks  map[int64][]domain.Task
	nextID int64
	clock  Clock
	list   *memListings
	media  *memMedia
}

func newMemPages(c Clock, list *memListings, media *memMedia) *memPages {
	return &memPages{pages: map[int64]*domain.Page{}, slugs: map[string]int64{}, tasks: map[int64][]domain.Task{}, clock: c, list: list, media: media}
}

func (m *memPages) logCh(ch domain.StatusChange) {
	m.list.mu.Lock()
	defer m.list.mu.Unlock()
	ch.At = m.clock.Now()
	m.list.log = append(m.list.log, ch)
}

func (m *memPages) attach(id int64, ids []string, detach bool) {
	m.media.mu.Lock()
	defer m.media.mu.Unlock()
	keep := map[string]bool{}
	for _, x := range ids {
		keep[x] = true
		if md, ok := m.media.items[x]; ok {
			md.Subject, md.SubjectID = string(domain.SubjectPage), id
		}
	}
	if detach {
		for _, md := range m.media.items {
			if md.Subject == string(domain.SubjectPage) && md.SubjectID == id && !keep[md.ID] {
				md.Subject, md.SubjectID = "", 0
			}
		}
	}
}

func (m *memPages) Page(_ context.Context, userID int64, kind domain.PageKind) (domain.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pages {
		if p.UserID == userID && p.Kind == kind {
			return clonePage(*p), nil
		}
	}
	return domain.Page{}, domain.ErrNotFound
}

func (m *memPages) PageByID(_ context.Context, id int64) (domain.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pages[id]
	if !ok {
		return domain.Page{}, domain.ErrNotFound
	}
	return clonePage(*p), nil
}

func clonePage(p domain.Page) domain.Page {
	if p.Revision != nil {
		r := *p.Revision
		p.Revision = &r
	}
	return p
}

func (m *memPages) CreatePage(_ context.Context, p domain.Page) (domain.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := string(p.Kind) + "/" + p.Slug
	if _, ok := m.slugs[key]; ok {
		return domain.Page{}, domain.ErrConflict
	}
	for _, x := range m.pages {
		if x.UserID == p.UserID && x.Kind == p.Kind {
			return domain.Page{}, domain.ErrConflict
		}
	}
	m.nextID++
	p.ID, p.Status, p.CreatedAt, p.UpdatedAt = m.nextID, domain.StatusDraft, m.clock.Now(), m.clock.Now()
	m.pages[p.ID] = &p
	m.slugs[key] = p.ID
	m.attach(p.ID, domain.PageMedia(p), true)
	return clonePage(p), nil
}

func (m *memPages) SavePage(_ context.Context, p domain.Page) (domain.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.pages[p.ID]
	if !ok || !cur.Status.Editable() {
		return domain.Page{}, domain.ErrConflict
	}
	cur.Title, cur.City, cur.Spheres, cur.Logo, cur.Cover, cur.Blocks = p.Title, p.City, p.Spheres, p.Logo, p.Cover, p.Blocks
	cur.UpdatedAt = m.clock.Now()
	m.attach(p.ID, domain.PageMedia(p), true)
	return clonePage(*cur), nil
}

func (m *memPages) SetPageSlug(_ context.Context, id int64, kind domain.PageKind, slug string, maxSlugs int) (domain.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := string(kind) + "/" + slug
	if owner, ok := m.slugs[key]; ok && owner != id {
		return domain.Page{}, domain.ErrConflict
	} else if !ok {
		n := 0
		for _, o := range m.slugs {
			if o == id {
				n++
			}
		}
		if n >= maxSlugs {
			return domain.Page{}, domain.ErrSlugLimit
		}
	}
	m.slugs[key] = id
	m.pages[id].Slug = slug
	return clonePage(*m.pages[id]), nil
}

func (m *memPages) SlugTaken(_ context.Context, kind domain.PageKind, slug string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.slugs[string(kind)+"/"+slug]
	return ok, nil
}

func (m *memPages) SetPageStatus(_ context.Context, chain ...domain.StatusChange) (domain.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out domain.Page
	for _, ch := range chain {
		p, ok := m.pages[ch.SubjectID]
		if !ok || p.Status != ch.From {
			return domain.Page{}, domain.ErrConflict
		}
		p.Status = ch.To
		p.RejectReason, p.ReviewNotes = "", nil
		if ch.To == domain.StatusRejected {
			p.RejectReason = ch.Reason
		}
		if ch.To == domain.StatusChangesRequested {
			p.ReviewNotes = ch.Notes
		}
		if ch.To != domain.StatusPublished {
			p.Revision, p.RevisionStatus = nil, ""
		}
		m.logCh(ch)
		out = clonePage(*p)
	}
	return out, nil
}

func (m *memPages) SavePageRevision(_ context.Context, id int64, rev domain.Page) (domain.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pages[id]
	if p == nil || p.Status != domain.StatusPublished || !domain.RevisionEditable(p.RevisionStatus) {
		return domain.Page{}, domain.ErrConflict
	}
	rev.Kind, rev.Slug = p.Kind, p.Slug
	p.Revision, p.RevisionStatus = &rev, domain.StatusDraft
	m.attach(id, append(domain.PageMedia(*p), domain.PageMedia(rev)...), true)
	return clonePage(*p), nil
}

func (m *memPages) SetPageRevision(_ context.Context, ch domain.StatusChange) (domain.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pages[ch.SubjectID]
	if p == nil || p.Status != domain.StatusPublished || p.RevisionStatus != ch.From {
		return domain.Page{}, domain.ErrConflict
	}
	p.RevisionStatus = ch.To
	m.logCh(ch)
	return clonePage(*p), nil
}

func (m *memPages) DropPageRevision(_ context.Context, id int64) (domain.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pages[id]
	if p == nil || p.Revision == nil {
		return domain.Page{}, domain.ErrConflict
	}
	p.Revision, p.RevisionStatus = nil, ""
	m.attach(id, domain.PageMedia(*p), true)
	return clonePage(*p), nil
}

func (m *memPages) ApplyPageRevision(_ context.Context, id int64, revStatus domain.Status, log domain.StatusChange, chain ...domain.StatusChange) (domain.Page, error) {
	m.mu.Lock()
	p := m.pages[id]
	if p == nil || p.Status != domain.StatusPublished || p.Revision == nil || p.RevisionStatus != revStatus {
		m.mu.Unlock()
		return domain.Page{}, domain.ErrConflict
	}
	r := p.Revision
	p.Title, p.City, p.Spheres, p.Logo, p.Cover, p.Blocks = r.Title, r.City, r.Spheres, r.Logo, r.Cover, r.Blocks
	p.Revision, p.RevisionStatus = nil, ""
	m.logCh(log)
	m.attach(id, domain.PageMedia(*p), true)
	out := clonePage(*p)
	m.mu.Unlock()
	if len(chain) > 0 {
		return m.SetPageStatus(context.Background(), chain...)
	}
	return out, nil
}

func (m *memPages) PublishedPage(_ context.Context, kind domain.PageKind, slug string) (domain.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.slugs[string(kind)+"/"+slug]
	if !ok || m.pages[id].Status != domain.StatusPublished {
		return domain.Page{}, domain.ErrNotFound
	}
	return clonePage(*m.pages[id]), nil
}

func (m *memPages) PublishedSlugs(_ context.Context, kind domain.PageKind, ids []int64) (map[int64]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[int64]string{}
	for _, p := range m.pages {
		for _, id := range ids {
			if p.UserID == id && p.Kind == kind && p.Status == domain.StatusPublished {
				out[id] = p.Slug
			}
		}
	}
	return out, nil
}

func (m *memPages) OpenTasks(_ context.Context, owner int64, limit int) ([]domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ts := append([]domain.Task(nil), m.tasks[owner]...)
	sort.Slice(ts, func(i, j int) bool { return ts[i].ID > ts[j].ID })
	if len(ts) > limit {
		ts = ts[:limit]
	}
	return ts, nil
}

// fakePublicCards — опубликованные анкеты (блок «Анкета»).
type fakePublicCards map[int64]domain.CreatorCard

func (f fakePublicCards) Public(_ context.Context, id int64) (domain.CreatorCard, error) {
	c, ok := f[id]
	if !ok {
		return domain.CreatorCard{}, domain.ErrNotFound
	}
	return c, nil
}

func (m *memPages) CategoryTitles(_ context.Context, slugs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, c := range m.list.cats.list {
		for _, s := range slugs {
			if c.Slug == s {
				out[s] = c.Title
			}
		}
	}
	return out, nil
}

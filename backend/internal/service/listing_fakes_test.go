package service

import (
	"context"
	"sort"
	"sync"
	"time"

	"ohvat/backend/internal/domain"
)

// memListings — задания, анкеты, журнал в памяти (TaskRepository, CardRepository, SubmitCounter, ModerationRepository).
type memListings struct {
	mu     sync.Mutex
	tasks  map[int64]*domain.Task
	cards  map[int64]*domain.CreatorCard
	log    []domain.StatusChange
	nextID int64
	clock  Clock
	fail   error
	cats   *memCats
}

func newMemListings(c Clock) *memListings {
	return &memListings{tasks: map[int64]*domain.Task{}, cards: map[int64]*domain.CreatorCard{}, clock: c}
}

func (m *memListings) CreateTask(_ context.Context, t domain.Task) (domain.Task, error) {
	if m.fail != nil {
		return domain.Task{}, m.fail
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	t.ID, t.Status, t.CreatedAt, t.UpdatedAt = m.nextID, domain.StatusDraft, m.clock.Now(), m.clock.Now()
	m.tasks[t.ID] = &t
	return t, nil
}

func (m *memListings) OwnTask(_ context.Context, owner, id int64) (domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok || t.OwnerID != owner {
		return domain.Task{}, domain.ErrNotFound
	}
	return *t, nil
}

func (m *memListings) Task(_ context.Context, id int64) (domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return domain.Task{}, domain.ErrNotFound
	}
	return *t, nil
}

func (m *memListings) OwnTasks(_ context.Context, owner int64, limit int) ([]domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Task
	for _, t := range m.tasks {
		if t.OwnerID == owner {
			out = append(out, *t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memListings) CountTasks(ctx context.Context, owner int64) (int, error) {
	ts, _ := m.OwnTasks(ctx, owner, 1<<30)
	return len(ts), nil
}

func (m *memListings) UpdateTask(_ context.Context, t domain.Task) (domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.tasks[t.ID]
	if !ok || cur.OwnerID != t.OwnerID || !cur.Status.Editable() {
		return domain.Task{}, domain.ErrConflict
	}
	t.Status, t.RejectReason, t.CreatedAt, t.UpdatedAt = cur.Status, cur.RejectReason, cur.CreatedAt, m.clock.Now()
	m.tasks[t.ID] = &t
	return t, nil
}

func (m *memListings) DeleteTask(_ context.Context, owner, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok || t.OwnerID != owner || !t.Status.Editable() {
		return domain.ErrConflict
	}
	delete(m.tasks, id)
	return nil
}

func (m *memListings) SetTaskStatus(_ context.Context, ch domain.StatusChange) (domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[ch.SubjectID]
	if !ok || t.Status != ch.From {
		return domain.Task{}, domain.ErrConflict
	}
	now := m.clock.Now()
	t.Status, t.RejectReason = ch.To, ch.Reason
	if ch.To == domain.StatusPublished {
		t.PublishedAt = &now
	}
	ch.Subject, ch.At = domain.SubjectTask, now
	m.log = append(m.log, ch)
	return *t, nil
}

func (m *memListings) PublishedTasks(_ context.Context, after *domain.FeedCursor, limit int) ([]domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Task
	for _, t := range m.tasks {
		if t.Status == domain.StatusPublished && (after == nil || t.ID < after.ID) {
			out = append(out, *t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memListings) Card(_ context.Context, uid int64) (domain.CreatorCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cards[uid]
	if !ok {
		return domain.CreatorCard{}, domain.ErrNotFound
	}
	return *c, nil
}

func (m *memListings) SaveCard(_ context.Context, c domain.CreatorCard) (domain.CreatorCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.cards[c.UserID]
	c.Status = domain.StatusDraft
	if ok {
		if !cur.Status.Editable() {
			return domain.CreatorCard{}, domain.ErrConflict
		}
		c.Status, c.RejectReason = cur.Status, cur.RejectReason
	}
	c.UpdatedAt = m.clock.Now()
	m.cards[c.UserID] = &c
	return c, nil
}

func (m *memListings) SetCardStatus(_ context.Context, ch domain.StatusChange) (domain.CreatorCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cards[ch.SubjectID]
	if !ok || c.Status != ch.From {
		return domain.CreatorCard{}, domain.ErrConflict
	}
	c.Status, c.RejectReason = ch.To, ch.Reason
	ch.Subject, ch.At = domain.SubjectCard, m.clock.Now()
	m.log = append(m.log, ch)
	return *c, nil
}

func (m *memListings) SubmitStats(_ context.Context, actor int64, since time.Time) (int, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, first := 0, time.Time{}
	for _, l := range m.log {
		if l.ActorID == actor && l.To == domain.StatusPending && l.At.After(since) {
			if n == 0 {
				first = l.At
			}
			n++
		}
	}
	return n, first, nil
}

func (m *memListings) History(_ context.Context, s domain.Subject, id int64) ([]domain.StatusChange, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.StatusChange
	for _, l := range m.log {
		if l.Subject == s && l.SubjectID == id {
			out = append(out, l)
		}
	}
	return out, nil
}

func (m *memListings) Category(ctx context.Context, id int64) (domain.CategoryInfo, error) {
	return m.cats.Category(ctx, id)
}

func (m *memListings) SetCategoryStatus(ctx context.Context, ch domain.StatusChange) (domain.CategoryInfo, error) {
	return m.cats.SetCategoryStatus(ctx, ch)
}

// memCats — справочник и свои категории (CategoryChecker, CategoryRepository).
type memCats struct {
	mu   sync.Mutex
	list []domain.CategoryInfo
}

func newMemCats() *memCats {
	return &memCats{list: []domain.CategoryInfo{
		{ID: 1, Slug: "food", Title: "Еда и кафе", Icon: "coffee", Status: domain.CategoryApproved},
		{ID: 2, Slug: "beauty", Title: "Красота", Icon: "scissors", Status: domain.CategoryApproved},
		{ID: 3, Slug: "sport", Title: "Спорт и фитнес", Icon: "dumbbell", Status: domain.CategoryApproved},
	}}
}

func (m *memCats) Usable(_ context.Context, uid int64, slugs []string) (int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, custom := 0, 0
	for _, s := range slugs {
		for _, c := range m.list {
			if c.Slug == s && (c.Status == domain.CategoryApproved || (c.AuthorID == uid && c.Status == domain.CategoryPending)) {
				n++
				if c.Own() {
					custom++
				}
			}
		}
	}
	return n, custom, nil
}

func (m *memCats) Common(context.Context) ([]domain.CategoryInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.CategoryInfo
	for _, c := range m.list {
		if c.Status == domain.CategoryApproved {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memCats) ByAuthor(_ context.Context, uid int64) ([]domain.CategoryInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.CategoryInfo
	for _, c := range m.list {
		if c.AuthorID == uid && c.Own() {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memCats) FindByNorm(_ context.Context, uid int64, norm string) (domain.CategoryInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.list {
		if NormCategory(c.Title) == norm && (c.Status == domain.CategoryApproved || (c.AuthorID == uid && c.Status == domain.CategoryPending)) {
			return c, nil
		}
	}
	return domain.CategoryInfo{}, domain.ErrNotFound
}

func (m *memCats) CountPending(_ context.Context, uid int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.list {
		if c.AuthorID == uid && c.Status == domain.CategoryPending {
			n++
		}
	}
	return n, nil
}

func (m *memCats) CreateCustom(_ context.Context, uid int64, slug, title, _ string) (domain.CategoryInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := domain.CategoryInfo{ID: int64(len(m.list) + 1), Slug: slug, Title: title, Icon: "tag", Status: domain.CategoryPending, AuthorID: uid}
	m.list = append(m.list, c)
	return c, nil
}

func (m *memCats) Category(_ context.Context, id int64) (domain.CategoryInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.list {
		if c.ID == id {
			return c, nil
		}
	}
	return domain.CategoryInfo{}, domain.ErrNotFound
}

func (m *memCats) SetCategoryStatus(_ context.Context, ch domain.StatusChange) (domain.CategoryInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, c := range m.list {
		if c.ID == ch.SubjectID && c.Status == string(ch.From) {
			m.list[i].Status = string(ch.To)
			return m.list[i], nil
		}
	}
	return domain.CategoryInfo{}, domain.ErrConflict
}

// memMedia — метаданные фото (MediaRepository, MediaLookup).
type memMedia struct {
	mu      sync.Mutex
	items   map[string]*domain.Media
	touched map[string]time.Time
	avatar  map[int64]string
	clock   Clock
}

func newMemMedia(c Clock) *memMedia {
	return &memMedia{items: map[string]*domain.Media{}, touched: map[string]time.Time{}, avatar: map[int64]string{}, clock: c}
}

func (m *memMedia) add(md domain.Media) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[md.ID] = &md
	m.touched[md.ID] = m.clock.Now()
}

func (m *memMedia) CreateMedia(_ context.Context, md domain.Media) (domain.Media, error) {
	m.add(md)
	return md, nil
}

func (m *memMedia) OwnMedia(_ context.Context, owner int64, ids []string) ([]domain.Media, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Media
	for _, id := range ids {
		if md, ok := m.items[id]; ok && md.OwnerID == owner {
			out = append(out, *md)
		}
	}
	return out, nil
}

func (m *memMedia) Usage(_ context.Context, owner int64) (int, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, b := 0, int64(0)
	for _, md := range m.items {
		if md.OwnerID == owner {
			n++
			b += md.Bytes
		}
	}
	return n, b, nil
}

func (m *memMedia) Access(_ context.Context, id string) (domain.MediaAccess, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	md, ok := m.items[id]
	if !ok {
		return domain.MediaAccess{}, domain.ErrNotFound
	}
	return domain.MediaAccess{ID: id, OwnerID: md.OwnerID, Public: md.Subject == "user"}, nil
}

func (m *memMedia) DeleteLoose(_ context.Context, owner int64, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	md, ok := m.items[id]
	if !ok || md.OwnerID != owner || md.Subject != "" {
		return domain.ErrNotFound
	}
	delete(m.items, id)
	return nil
}

func (m *memMedia) SetAvatar(_ context.Context, uid int64, id, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id != "" {
		md, ok := m.items[id]
		if !ok || md.OwnerID != uid || md.Purpose != domain.PurposeAvatar {
			return domain.ErrConflict
		}
		md.Subject = "user"
	}
	m.avatar[uid] = id
	return nil
}

func (m *memMedia) Orphans(_ context.Context, before time.Time, limit int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id, md := range m.items {
		if md.Subject == "" && m.touched[id].Before(before) && len(out) < limit {
			out = append(out, id)
		}
	}
	return out, nil
}

func (m *memMedia) DeleteMedia(_ context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		delete(m.items, id)
	}
	return nil
}

// memBlobs — хранилище файлов в памяти.
type memBlobs struct {
	mu   sync.Mutex
	data map[string][]byte
	fail error
}

func (b *memBlobs) Put(_ context.Context, k, _ string, d []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fail != nil {
		return b.fail
	}
	b.data[k] = d
	return nil
}

func (b *memBlobs) Get(_ context.Context, k string) (Blob, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.data[k]
	if !ok {
		return Blob{}, domain.ErrNotFound
	}
	return Blob{ContentType: "image/webp", Data: d}, nil
}

func (b *memBlobs) Delete(_ context.Context, keys []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, k := range keys {
		delete(b.data, k)
	}
	return nil
}

// fakeImages — обработчик фото: «фото» — любые данные, кроме "bad" и "huge".
type fakeImages struct{ last ImageOptions }

func (f *fakeImages) Process(_ context.Context, data []byte, opt ImageOptions) (ProcessedImage, error) {
	f.last = opt
	switch string(data) {
	case "bad":
		return ProcessedImage{}, ErrImageType
	case "huge":
		return ProcessedImage{}, ErrImageTooLarge
	}
	out := ProcessedImage{Width: 1600, Height: 900, Variants: map[int][]byte{}}
	for _, s := range opt.Sizes {
		out.Variants[s] = []byte("webp")
	}
	return out, nil
}

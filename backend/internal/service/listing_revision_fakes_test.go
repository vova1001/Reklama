package service

import (
	"context"

	"ohvat/backend/internal/domain"
)

// Правки опубликованного в памяти (как repository/revision.go).

func (m *memListings) SaveTaskRevision(_ context.Context, owner, id int64, rev domain.Task) (domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok || t.OwnerID != owner || t.Status != domain.StatusPublished || !domain.RevisionEditable(t.RevisionStatus) {
		return domain.Task{}, domain.ErrConflict
	}
	r := rev
	t.Revision, t.RevisionStatus, t.RevisionReason = &r, domain.StatusDraft, ""
	return *t, nil
}

func (m *memListings) SetTaskRevision(_ context.Context, ch domain.StatusChange) (domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[ch.SubjectID]
	if !ok || t.Status != domain.StatusPublished || t.Revision == nil || t.RevisionStatus != ch.From {
		return domain.Task{}, domain.ErrConflict
	}
	t.RevisionStatus, t.RevisionReason, t.ReviewNotes = ch.To, "", nil
	if ch.To == domain.StatusRejected {
		t.RevisionReason = ch.Reason
	}
	if ch.To == domain.StatusChangesRequested {
		t.ReviewNotes = ch.Notes
	}
	ch.From, ch.To, ch.Subject = "revision:"+ch.From, "revision:"+ch.To, domain.SubjectTask
	m.log = append(m.log, ch)
	return *t, nil
}

func (m *memListings) DropTaskRevision(_ context.Context, owner, id int64) (domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok || t.OwnerID != owner || t.Status != domain.StatusPublished || t.Revision == nil {
		return domain.Task{}, domain.ErrConflict
	}
	t.Revision, t.RevisionStatus, t.RevisionReason, t.ReviewNotes = nil, "", "", nil
	return *t, nil
}

func (m *memListings) ApplyTaskRevision(_ context.Context, id int64, revStatus domain.Status, log domain.StatusChange, chain ...domain.StatusChange) (domain.Task, error) {
	m.mu.Lock()
	t, ok := m.tasks[id]
	if !ok || t.Status != domain.StatusPublished || t.Revision == nil || t.RevisionStatus != revStatus {
		m.mu.Unlock()
		return domain.Task{}, domain.ErrConflict
	}
	rev := *t.Revision
	rev.ID, rev.OwnerID, rev.Status, rev.PublishedAt, rev.CreatedAt = t.ID, t.OwnerID, t.Status, t.PublishedAt, t.CreatedAt
	*t = rev
	log.From, log.To, log.Subject = "revision:"+log.From, "revision:"+log.To, domain.SubjectTask
	m.log = append(m.log, log)
	m.mu.Unlock()
	if len(chain) > 0 {
		return m.SetTaskStatus(context.Background(), chain...)
	}
	return rev, nil
}

func (m *memListings) SaveCardRevision(_ context.Context, uid int64, rev domain.CreatorCard) (domain.CreatorCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cards[uid]
	if !ok || c.Status != domain.StatusPublished || !domain.RevisionEditable(c.RevisionStatus) {
		return domain.CreatorCard{}, domain.ErrConflict
	}
	r := rev
	c.Revision, c.RevisionStatus, c.RevisionReason = &r, domain.StatusDraft, ""
	return *c, nil
}

func (m *memListings) SetCardRevision(_ context.Context, ch domain.StatusChange) (domain.CreatorCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cards[ch.SubjectID]
	if !ok || c.Status != domain.StatusPublished || c.Revision == nil || c.RevisionStatus != ch.From {
		return domain.CreatorCard{}, domain.ErrConflict
	}
	c.RevisionStatus, c.RevisionReason, c.ReviewNotes = ch.To, "", nil
	if ch.To == domain.StatusRejected {
		c.RevisionReason = ch.Reason
	}
	if ch.To == domain.StatusChangesRequested {
		c.ReviewNotes = ch.Notes
	}
	ch.From, ch.To, ch.Subject = "revision:"+ch.From, "revision:"+ch.To, domain.SubjectCard
	m.log = append(m.log, ch)
	return *c, nil
}

func (m *memListings) DropCardRevision(_ context.Context, uid int64) (domain.CreatorCard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cards[uid]
	if !ok || c.Status != domain.StatusPublished || c.Revision == nil {
		return domain.CreatorCard{}, domain.ErrConflict
	}
	c.Revision, c.RevisionStatus, c.RevisionReason, c.ReviewNotes = nil, "", "", nil
	return *c, nil
}

func (m *memListings) ApplyCardRevision(_ context.Context, uid int64, revStatus domain.Status, log domain.StatusChange, chain ...domain.StatusChange) (domain.CreatorCard, error) {
	m.mu.Lock()
	c, ok := m.cards[uid]
	if !ok || c.Status != domain.StatusPublished || c.Revision == nil || c.RevisionStatus != revStatus {
		m.mu.Unlock()
		return domain.CreatorCard{}, domain.ErrConflict
	}
	rev := *c.Revision
	rev.UserID, rev.Status, rev.PublishedAt = c.UserID, c.Status, c.PublishedAt
	*c = rev
	log.From, log.To, log.Subject = "revision:"+log.From, "revision:"+log.To, domain.SubjectCard
	m.log = append(m.log, log)
	m.mu.Unlock()
	if len(chain) > 0 {
		return m.SetCardStatus(context.Background(), chain...)
	}
	return rev, nil
}

package service

import (
	"context"
	"errors"
	"testing"

	"ohvat/backend/internal/domain"
)

// Правка опубликованного: новая версия ждёт, опубликованная видна; автоматический режим — заменяет сразу;
// «на доработку» с замечаниями; снять с публикации с правкой — черновиком становится правка.
func TestTaskRevisions(t *testing.T) {
	ctx := context.Background()
	e := newAutoEnv()
	d, _ := e.tasks.Create(ctx, biz, fullTask())
	if _, err := e.tasks.Submit(ctx, biz, d.ID); err != nil {
		t.Fatal(err)
	}
	// автоматически: правка сразу заменяет опубликованное
	in := fullTask()
	in.Title = "Новая версия задания"
	got, err := e.tasks.Update(ctx, biz, d.ID, in)
	if err != nil || got.Status != domain.StatusPublished || got.Revision == nil || got.RevisionStatus != domain.StatusDraft || got.Title == in.Title {
		t.Fatalf("правка: %v %+v", err, got)
	}
	if got, err = e.tasks.Submit(ctx, biz, d.ID); err != nil || got.Title != in.Title || got.Revision != nil {
		t.Fatalf("автоматическая правка: %v %+v", err, got)
	}

	// вручную: правка ждёт модератора
	m := newListingEnv(ListingLimits{})
	d, _ = m.tasks.Create(ctx, biz, fullTask())
	_, _ = m.tasks.Submit(ctx, biz, d.ID)
	if _, err := m.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, ModeratorID: 1, Approve: true}); err != nil {
		t.Fatal(err)
	}
	in.Title = "Ручная правка"
	if _, err := m.tasks.Update(ctx, biz, d.ID, in); err != nil {
		t.Fatal(err)
	}
	if got, _ = m.tasks.Submit(ctx, biz, d.ID); got.RevisionStatus != domain.StatusPending || got.Title == in.Title {
		t.Fatalf("правка на проверке: %+v", got)
	}
	if _, err := m.tasks.Update(ctx, biz, d.ID, in); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("правка на проверке не меняется: %v", err)
	}
	if pub, _ := m.repo.PublishedTask(ctx, d.ID); pub.Title == in.Title {
		t.Error("до одобрения видна старая версия")
	}
	st, err := m.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, ModeratorID: 1, Revision: true, Action: domain.ActionRework,
		Notes: map[string]string{"photos": "Добавьте фото меню"}})
	if err != nil || st != domain.StatusChangesRequested {
		t.Fatalf("доработка правки: %v %v", err, st)
	}
	if got, _ = m.tasks.Get(ctx, biz, d.ID); got.ReviewNotes["photos"] == "" {
		t.Errorf("замечания: %+v", got)
	}
	if _, err := m.tasks.Update(ctx, biz, d.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := m.tasks.Submit(ctx, biz, d.ID); err != nil {
		t.Fatal(err)
	}
	if st, err := m.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, ModeratorID: 1, Revision: true, Approve: true}); err != nil || st != domain.StatusPublished {
		t.Fatalf("одобрение правки: %v %v", err, st)
	}
	if pub, _ := m.repo.PublishedTask(ctx, d.ID); pub.Title != in.Title {
		t.Errorf("после одобрения: %q", pub.Title)
	}
	if _, err := m.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, ModeratorID: 1, Revision: true, Approve: true}); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("правки уже нет: %v", err)
	}
	// удалить правку; снять с публикации с правкой — черновиком становится правка
	in.Title = "Удалим"
	_, _ = m.tasks.Update(ctx, biz, d.ID, in)
	if got, err = m.tasks.DiscardRevision(ctx, biz, d.ID); err != nil || got.Revision != nil {
		t.Fatalf("удаление правки: %v %+v", err, got)
	}
	if _, err := m.tasks.DiscardRevision(ctx, biz, d.ID); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("удалять нечего: %v", err)
	}
	if _, err := m.tasks.DiscardRevision(ctx, domain.User{ID: 99}, d.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("чужое: %v", err)
	}
	in.Title = "Снимем с правкой"
	_, _ = m.tasks.Update(ctx, biz, d.ID, in)
	if got, err = m.tasks.Withdraw(ctx, biz, d.ID); err != nil || got.Status != domain.StatusDraft || got.Title != in.Title {
		t.Fatalf("снять с правкой: %v %+v", err, got)
	}
	// снять опубликованное по жалобе — только модератор, с причиной
	_, _ = m.tasks.Submit(ctx, biz, d.ID)
	_, _ = m.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, ModeratorID: 1, Approve: true})
	if st, err := m.mod.Decide(ctx, Decision{Subject: domain.SubjectTask, ID: d.ID, ModeratorID: 1, Action: domain.ActionReject, Reason: "Жалоба подтвердилась"}); err != nil || st != domain.StatusRejected {
		t.Errorf("снять по жалобе: %v %v", err, st)
	}
}

func TestCardRevisions(t *testing.T) {
	ctx := context.Background()
	m := newAutoEnv()
	m.tasks.Mode = domain.ModerationManual
	m.cards.Mode = domain.ModerationManual
	if _, err := m.cards.Save(ctx, author, fullCard()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.cards.Submit(ctx, author); err != nil {
		t.Fatal(err)
	}
	if _, err := m.mod.Decide(ctx, Decision{Subject: domain.SubjectCard, ID: author.ID, ModeratorID: 1, Approve: true}); err != nil {
		t.Fatal(err)
	}
	in := fullCard()
	in.Name = "Новое имя"
	c, err := m.cards.Save(ctx, author, in)
	if err != nil || c.Revision == nil || c.Name == in.Name {
		t.Fatalf("правка анкеты: %v %+v", err, c)
	}
	if c, err = m.cards.Submit(ctx, author); err != nil || c.RevisionStatus != domain.StatusPending {
		t.Fatalf("на проверку: %v %+v", err, c)
	}
	if st, err := m.mod.Decide(ctx, Decision{Subject: domain.SubjectCard, ID: author.ID, ModeratorID: 1, Revision: true, Action: domain.ActionReject, Reason: "Имя не совпадает"}); err != nil || st != domain.StatusRejected {
		t.Fatalf("отказ правки: %v %v", err, st)
	}
	if c, _ := m.cards.Get(ctx, author); c.Status != domain.StatusPublished || c.RevisionReason == "" {
		t.Errorf("анкета опубликована, правка отклонена: %+v", c)
	}
	_, _ = m.cards.Save(ctx, author, in)
	_, _ = m.cards.Submit(ctx, author)
	if _, err := m.mod.Decide(ctx, Decision{Subject: domain.SubjectCard, ID: author.ID, ModeratorID: 1, Revision: true, Approve: true}); err != nil {
		t.Fatal(err)
	}
	if c, _ := m.cards.Get(ctx, author); c.Name != in.Name || c.Revision != nil {
		t.Errorf("правка применена: %+v", c)
	}
	in.Name = "Ещё одно"
	_, _ = m.cards.Save(ctx, author, in)
	if c, err := m.cards.DiscardRevision(ctx, author); err != nil || c.Revision != nil {
		t.Errorf("удалить правку: %v", err)
	}
	_, _ = m.cards.Save(ctx, author, in)
	if c, err := m.cards.Withdraw(ctx, author); err != nil || c.Status != domain.StatusDraft || c.Name != in.Name {
		t.Errorf("снять с правкой: %v %+v", err, c)
	}
}

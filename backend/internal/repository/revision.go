package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// Правка опубликованного: новая версия хранится в revision (jsonb) рядом с опубликованной; опубликованная видна
// всем, пока правку не одобрят. Фото правки прикрепляются к тому же заданию (анкете), но публичными становятся
// только после одобрения (MediaAccess смотрит на опубликованное содержимое).

// taskContentRow — содержимое задания в jsonb правки (теги JSON — только здесь).
type taskContentRow struct {
	Title         string     `json:"title"`
	City          string     `json:"city"`
	Nationwide    bool       `json:"nationwide"`
	Categories    []string   `json:"categories"`
	Formats       []string   `json:"formats"`
	Access        string     `json:"access"`
	MinViews      int64      `json:"minViews"`
	RewardKopecks int64      `json:"rewardKopecks"`
	PublishBy     *time.Time `json:"publishBy,omitempty"`
	PublishTime   string     `json:"publishTime,omitempty"`
	DeadlineAt    *time.Time `json:"deadlineAt,omitempty"`
	Blocks        []blockRow `json:"blocks"`
	Cover         string     `json:"cover,omitempty"`
	Photos        []string   `json:"photos"`
}

type cardContentRow struct {
	Name   string     `json:"name"`
	City   string     `json:"city"`
	Topics []string   `json:"topics"`
	Blocks []blockRow `json:"blocks"`
	Avatar string     `json:"avatar,omitempty"`
	Works  []string   `json:"works"`
}

func blockRows(bs []domain.Block) []blockRow {
	rows := make([]blockRow, 0, len(bs))
	for _, b := range bs {
		rows = append(rows, blockRow{Kind: b.Kind, Title: b.Title, Items: nonNil(b.Items)})
	}
	return rows
}

func blocksFromRows(rows []blockRow) []domain.Block {
	out := make([]domain.Block, 0, len(rows))
	for _, b := range rows {
		out = append(out, domain.Block{Kind: b.Kind, Title: b.Title, Items: nonNil(b.Items)})
	}
	return out
}

func taskRevisionJSON(t domain.Task) ([]byte, error) {
	b, err := json.Marshal(taskContentRow{
		Title: t.Title, City: t.City, Nationwide: t.Nationwide, Categories: nonNil(t.Categories), Formats: nonNil(t.Formats),
		Access: t.Access, MinViews: t.MinViews, RewardKopecks: t.RewardKopecks, PublishBy: t.PublishBy,
		PublishTime: t.PublishTime, DeadlineAt: t.DeadlineAt, Blocks: blockRows(t.Blocks), Cover: t.Cover, Photos: nonNil(t.Photos),
	})
	if err != nil {
		return nil, fmt.Errorf("правка задания в JSON: %w", err)
	}
	return b, nil
}

func taskRevisionOf(raw []byte) (domain.Task, error) {
	var r taskContentRow
	if err := json.Unmarshal(raw, &r); err != nil {
		return domain.Task{}, fmt.Errorf("правка задания из JSON: %w", err)
	}
	return domain.Task{
		Title: r.Title, City: r.City, Nationwide: r.Nationwide, Categories: nonNil(r.Categories), Formats: nonNil(r.Formats),
		Access: r.Access, MinViews: r.MinViews, RewardKopecks: r.RewardKopecks, PublishBy: r.PublishBy,
		PublishTime: r.PublishTime, DeadlineAt: r.DeadlineAt, Blocks: blocksFromRows(r.Blocks), Cover: r.Cover, Photos: nonNil(r.Photos),
	}, nil
}

func cardRevisionJSON(c domain.CreatorCard) ([]byte, error) {
	b, err := json.Marshal(cardContentRow{
		Name: c.Name, City: c.City, Topics: nonNil(c.Topics), Blocks: blockRows(c.Blocks), Avatar: c.Avatar, Works: nonNil(c.Works),
	})
	if err != nil {
		return nil, fmt.Errorf("правка анкеты в JSON: %w", err)
	}
	return b, nil
}

func cardRevisionOf(raw []byte) (domain.CreatorCard, error) {
	var r cardContentRow
	if err := json.Unmarshal(raw, &r); err != nil {
		return domain.CreatorCard{}, fmt.Errorf("правка анкеты из JSON: %w", err)
	}
	return domain.CreatorCard{
		Name: r.Name, City: r.City, Topics: nonNil(r.Topics), Platforms: []domain.Platform{}, Blocks: blocksFromRows(r.Blocks),
		Avatar: r.Avatar, Works: nonNil(r.Works),
	}, nil
}

// notesJSON — замечания по полям в jsonb (всегда объект).
func notesJSON(n map[string]string) []byte {
	if len(n) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(n)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func notesOf(raw []byte) map[string]string {
	out := map[string]string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// attachOnly — прикрепить фото правки, не открепляя опубликованные (они ещё видны всем).
func attachOnly(ctx context.Context, q *store.Queries, ownerID int64, s domain.Subject, id int64, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	n, err := q.AttachMedia(ctx, store.AttachMediaParams{Subject: text(string(s)), SubjectID: pgInt8(id), OwnerID: ownerID, Ids: ids})
	if err != nil {
		return err
	}
	if int(n) != len(ids) {
		return domain.ErrConflict
	}
	return nil
}

func union(a, b []string) []string {
	out := append([]string(nil), a...)
	for _, x := range b {
		found := false
		for _, y := range out {
			if x == y {
				found = true
				break
			}
		}
		if !found {
			out = append(out, x)
		}
	}
	return out
}

// ===== задания =====

// SaveTaskRevision — правка опубликованного задания (фото прикрепляются, опубликованные остаются); на проверке
// или не опубликовано — ErrConflict.
func (r *Listings) SaveTaskRevision(ctx context.Context, ownerID, id int64, rev domain.Task) (domain.Task, error) {
	raw, err := taskRevisionJSON(rev)
	if err != nil {
		return domain.Task{}, err
	}
	var row store.Task
	err = r.inTx(ctx, func(q *store.Queries) error {
		var err error
		if row, err = q.SaveTaskRevision(ctx, store.SaveTaskRevisionParams{ID: id, OwnerID: ownerID, Revision: raw}); err != nil {
			return err
		}
		live, err := taskOf(row)
		if err != nil {
			return err
		}
		// фото, которых нет ни в опубликованном, ни в правке, — открепить (через сутки удалит очистка)
		keep := union(taskMedia(live), taskMedia(rev))
		if err := q.DetachMedia(ctx, store.DetachMediaParams{Subject: text(string(domain.SubjectTask)), SubjectID: pgInt8(id), Keep: keep}); err != nil {
			return err
		}
		return attachOnly(ctx, q, ownerID, domain.SubjectTask, id, taskMedia(rev))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Task{}, dbErr("правка задания", err)
	}
	return taskOf(row)
}

// SetTaskRevision — смена статуса правки (из ch.From в ch.To) и запись в журнал («revision:…»); статус уже
// другой — ErrConflict.
func (r *Listings) SetTaskRevision(ctx context.Context, ch domain.StatusChange) (domain.Task, error) {
	var row store.Task
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		row, err = q.SetTaskRevisionStatus(ctx, store.SetTaskRevisionStatusParams{
			ID: ch.SubjectID, FromStatus: string(ch.From), ToStatus: string(ch.To), Reason: ch.Reason, Notes: notesJSON(ch.Notes),
		})
		if err != nil {
			return err
		}
		return q.LogStatusChange(ctx, logParams(domain.SubjectTask, revisionLog(ch)))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Task{}, dbErr("статус правки задания", err)
	}
	return taskOf(row)
}

// DropTaskRevision — удалить правку (фото только правки — открепляются).
func (r *Listings) DropTaskRevision(ctx context.Context, ownerID, id int64) (domain.Task, error) {
	var row store.Task
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		if row, err = q.DropTaskRevision(ctx, store.DropTaskRevisionParams{ID: id, OwnerID: ownerID}); err != nil {
			return err
		}
		live, err := taskOf(row)
		if err != nil {
			return err
		}
		return q.DetachMedia(ctx, store.DetachMediaParams{Subject: text(string(domain.SubjectTask)), SubjectID: pgInt8(id), Keep: nonNil(taskMedia(live))})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Task{}, dbErr("удаление правки", err)
	}
	return taskOf(row)
}

// ApplyTaskRevision — правка становится опубликованным содержимым (одобрение) или содержимым черновика (снятие с
// публикации с правкой): content — новая версия, chain — смены статуса после замены (может быть пустой), log —
// запись журнала о правке. Всё — одной транзакцией; статус или статус правки уже другие — ErrConflict.
func (r *Listings) ApplyTaskRevision(ctx context.Context, id int64, revStatus domain.Status, log domain.StatusChange, chain ...domain.StatusChange) (domain.Task, error) {
	var row store.Task
	err := r.inTx(ctx, func(q *store.Queries) error {
		cur, err := q.GetTask(ctx, id)
		if err != nil {
			return err
		}
		if cur.Status != string(domain.StatusPublished) || !cur.RevisionStatus.Valid || cur.RevisionStatus.String != string(revStatus) {
			return pgx.ErrNoRows
		}
		rev, err := taskRevisionOf(cur.Revision)
		if err != nil {
			return err
		}
		blocks, err := blocksJSON(rev.Blocks)
		if err != nil {
			return err
		}
		row, err = q.ReplaceTaskContent(ctx, store.ReplaceTaskContentParams{
			ID: id, Status: string(domain.StatusPublished), RevisionStatus: string(revStatus),
			Title: rev.Title, City: rev.City, Nationwide: rev.Nationwide, Categories: nonNil(rev.Categories), Formats: nonNil(rev.Formats),
			Access: rev.Access, MinViews: rev.MinViews, RewardKopecks: rev.RewardKopecks, PublishBy: date(rev.PublishBy),
			PublishTime: clockTime(rev.PublishTime), DeadlineAt: tsOpt(rev.DeadlineAt), Blocks: blocks, Cover: text(rev.Cover), Photos: nonNil(rev.Photos),
		})
		if err != nil {
			return err
		}
		if err := q.LogStatusChange(ctx, logParams(domain.SubjectTask, revisionLog(log))); err != nil {
			return err
		}
		for _, ch := range chain {
			if row, err = q.SetTaskStatus(ctx, store.SetTaskStatusParams{ID: id, FromStatus: string(ch.From), ToStatus: string(ch.To), Reason: ch.Reason, Notes: notesJSON(ch.Notes)}); err != nil {
				return err
			}
			if err := q.LogStatusChange(ctx, logParams(domain.SubjectTask, ch)); err != nil {
				return err
			}
		}
		return attach(ctx, q, cur.OwnerID, domain.SubjectTask, id, taskMedia(rev))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Task{}, dbErr("применение правки задания", err)
	}
	return taskOf(row)
}

// ===== анкеты =====

// SaveCardRevision — правка опубликованной анкеты.
func (r *Listings) SaveCardRevision(ctx context.Context, userID int64, rev domain.CreatorCard) (domain.CreatorCard, error) {
	raw, err := cardRevisionJSON(rev)
	if err != nil {
		return domain.CreatorCard{}, err
	}
	var row store.CreatorCard
	err = r.inTx(ctx, func(q *store.Queries) error {
		var err error
		if row, err = q.SaveCardRevision(ctx, store.SaveCardRevisionParams{UserID: userID, Revision: raw}); err != nil {
			return err
		}
		live, err := cardOf(row)
		if err != nil {
			return err
		}
		keep := union(cardMedia(live), cardMedia(rev))
		if err := q.DetachMedia(ctx, store.DetachMediaParams{Subject: text(string(domain.SubjectCard)), SubjectID: pgInt8(userID), Keep: keep}); err != nil {
			return err
		}
		return attachOnly(ctx, q, userID, domain.SubjectCard, userID, cardMedia(rev))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CreatorCard{}, domain.ErrConflict
	}
	if err != nil {
		return domain.CreatorCard{}, dbErr("правка анкеты", err)
	}
	return cardOf(row)
}

// SetCardRevision — смена статуса правки анкеты и запись в журнал.
func (r *Listings) SetCardRevision(ctx context.Context, ch domain.StatusChange) (domain.CreatorCard, error) {
	var row store.CreatorCard
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		row, err = q.SetCardRevisionStatus(ctx, store.SetCardRevisionStatusParams{
			UserID: ch.SubjectID, FromStatus: string(ch.From), ToStatus: string(ch.To), Reason: ch.Reason, Notes: notesJSON(ch.Notes),
		})
		if err != nil {
			return err
		}
		return q.LogStatusChange(ctx, logParams(domain.SubjectCard, revisionLog(ch)))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CreatorCard{}, domain.ErrConflict
	}
	if err != nil {
		return domain.CreatorCard{}, dbErr("статус правки анкеты", err)
	}
	return cardOf(row)
}

// DropCardRevision — удалить правку анкеты.
func (r *Listings) DropCardRevision(ctx context.Context, userID int64) (domain.CreatorCard, error) {
	var row store.CreatorCard
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		if row, err = q.DropCardRevision(ctx, userID); err != nil {
			return err
		}
		live, err := cardOf(row)
		if err != nil {
			return err
		}
		return q.DetachMedia(ctx, store.DetachMediaParams{Subject: text(string(domain.SubjectCard)), SubjectID: pgInt8(userID), Keep: nonNil(cardMedia(live))})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CreatorCard{}, domain.ErrConflict
	}
	if err != nil {
		return domain.CreatorCard{}, dbErr("удаление правки анкеты", err)
	}
	return cardOf(row)
}

// ApplyCardRevision — как ApplyTaskRevision для анкеты.
func (r *Listings) ApplyCardRevision(ctx context.Context, userID int64, revStatus domain.Status, log domain.StatusChange, chain ...domain.StatusChange) (domain.CreatorCard, error) {
	var row store.CreatorCard
	err := r.inTx(ctx, func(q *store.Queries) error {
		cur, err := q.GetCard(ctx, userID)
		if err != nil {
			return err
		}
		if cur.Status != string(domain.StatusPublished) || !cur.RevisionStatus.Valid || cur.RevisionStatus.String != string(revStatus) {
			return pgx.ErrNoRows
		}
		rev, err := cardRevisionOf(cur.Revision)
		if err != nil {
			return err
		}
		blocks, err := blocksJSON(rev.Blocks)
		if err != nil {
			return err
		}
		row, err = q.ReplaceCardContent(ctx, store.ReplaceCardContentParams{
			UserID: userID, Status: string(domain.StatusPublished), RevisionStatus: string(revStatus),
			Name: rev.Name, City: rev.City, Topics: nonNil(rev.Topics), Blocks: blocks, Avatar: text(rev.Avatar), Works: nonNil(rev.Works),
		})
		if err != nil {
			return err
		}
		if err := q.LogStatusChange(ctx, logParams(domain.SubjectCard, revisionLog(log))); err != nil {
			return err
		}
		for _, ch := range chain {
			if row, err = q.SetCardStatus(ctx, store.SetCardStatusParams{UserID: userID, FromStatus: string(ch.From), ToStatus: string(ch.To), Reason: ch.Reason, Notes: notesJSON(ch.Notes)}); err != nil {
				return err
			}
			if err := q.LogStatusChange(ctx, logParams(domain.SubjectCard, ch)); err != nil {
				return err
			}
		}
		return attach(ctx, q, userID, domain.SubjectCard, userID, cardMedia(rev))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CreatorCard{}, domain.ErrConflict
	}
	if err != nil {
		return domain.CreatorCard{}, dbErr("применение правки анкеты", err)
	}
	return cardOf(row)
}

// revisionLog — запись журнала о правке: статусы с приставкой «revision:», чтобы история отличала правку
// от основного статуса.
func revisionLog(ch domain.StatusChange) domain.StatusChange {
	ch.From = "revision:" + ch.From
	ch.To = "revision:" + ch.To
	return ch
}

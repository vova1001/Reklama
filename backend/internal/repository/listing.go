package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// Listings — задания бизнеса, анкеты авторов и журнал модерации в PostgreSQL. SQL — только в queries/listing.sql.
// Смена статуса и запись в журнал — в одной транзакции; смена только из ожидаемого статуса (без гонок).
type Listings struct {
	db DB
	q  *store.Queries
}

// NewListings создаёт репозиторий заданий и анкет.
func NewListings(db DB) *Listings { return &Listings{db: db, q: store.New(db)} }

func (r *Listings) inTx(ctx context.Context, fn func(q *store.Queries) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(r.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ===== задания =====

// CreateTask — новый черновик; фото (обложка и примеры) прикрепляются в той же транзакции.
func (r *Listings) CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	blocks, err := blocksJSON(t.Blocks)
	if err != nil {
		return domain.Task{}, err
	}
	var row store.Task
	err = r.inTx(ctx, func(q *store.Queries) error {
		var err error
		row, err = q.CreateTask(ctx, store.CreateTaskParams{
			OwnerID: t.OwnerID, Title: t.Title, City: t.City, Nationwide: t.Nationwide, Category: text(t.Category),
			Formats: nonNil(t.Formats), Access: t.Access, MinViews: t.MinViews,
			RewardKopecks: t.RewardKopecks, PublishBy: date(t.PublishBy), PublishTime: clockTime(t.PublishTime), DeadlineAt: tsOpt(t.DeadlineAt), Blocks: blocks,
			Cover: text(t.Cover), Photos: nonNil(t.Photos),
		})
		if err != nil {
			return err
		}
		return attach(ctx, q, t.OwnerID, domain.SubjectTask, row.ID, taskMedia(t))
	})
	if err != nil {
		return domain.Task{}, dbErr("новое задание", err)
	}
	return taskOf(row)
}

// OwnTask — задание владельца; чужое или нет — ErrNotFound.
func (r *Listings) OwnTask(ctx context.Context, ownerID, id int64) (domain.Task, error) {
	row, err := r.q.GetOwnTask(ctx, store.GetOwnTaskParams{ID: id, OwnerID: ownerID})
	if err != nil {
		return domain.Task{}, dbErr("задание", err)
	}
	return taskOf(row)
}

// Task — задание по id (для модерации).
func (r *Listings) Task(ctx context.Context, id int64) (domain.Task, error) {
	row, err := r.q.GetTask(ctx, id)
	if err != nil {
		return domain.Task{}, dbErr("задание", err)
	}
	return taskOf(row)
}

// OwnTasks — задания владельца, недавно изменённые сверху.
func (r *Listings) OwnTasks(ctx context.Context, ownerID int64, limit int) ([]domain.Task, error) {
	rows, err := r.q.ListOwnTasks(ctx, store.ListOwnTasksParams{OwnerID: ownerID, Lim: int32(limit)})
	if err != nil {
		return nil, dbErr("мои задания", err)
	}
	out := make([]domain.Task, 0, len(rows))
	for _, row := range rows {
		t, err := taskOf(row)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// CountTasks — сколько заданий у владельца (лимит на создание).
func (r *Listings) CountTasks(ctx context.Context, ownerID int64) (int, error) {
	n, err := r.q.CountOwnTasks(ctx, ownerID)
	return int(n), dbErr("число заданий", err)
}

// UpdateTask — правка черновика или отклонённого (с фото — в одной транзакции); нет такого или уже не
// редактируется — ErrConflict.
func (r *Listings) UpdateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	blocks, err := blocksJSON(t.Blocks)
	if err != nil {
		return domain.Task{}, err
	}
	var row store.Task
	err = r.inTx(ctx, func(q *store.Queries) error {
		var err error
		row, err = q.UpdateOwnTask(ctx, store.UpdateOwnTaskParams{
			ID: t.ID, OwnerID: t.OwnerID, Title: t.Title, City: t.City, Nationwide: t.Nationwide, Category: text(t.Category),
			Formats: nonNil(t.Formats), Access: t.Access, MinViews: t.MinViews,
			RewardKopecks: t.RewardKopecks, PublishBy: date(t.PublishBy), PublishTime: clockTime(t.PublishTime), DeadlineAt: tsOpt(t.DeadlineAt), Blocks: blocks,
			Cover: text(t.Cover), Photos: nonNil(t.Photos),
		})
		if err != nil {
			return err
		}
		return attach(ctx, q, t.OwnerID, domain.SubjectTask, t.ID, taskMedia(t))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Task{}, dbErr("правка задания", err)
	}
	return taskOf(row)
}

// DeleteTask — удалить черновик или отклонённое (его фото открепляются); иначе — ErrConflict.
func (r *Listings) DeleteTask(ctx context.Context, ownerID, id int64) error {
	err := r.inTx(ctx, func(q *store.Queries) error {
		n, err := q.DeleteOwnTask(ctx, store.DeleteOwnTaskParams{ID: id, OwnerID: ownerID})
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrConflict
		}
		return q.DetachMedia(ctx, store.DetachMediaParams{Subject: text(string(domain.SubjectTask)), SubjectID: pgInt8(id), Keep: []string{}})
	})
	if errors.Is(err, domain.ErrConflict) {
		return err
	}
	return dbErr("удаление задания", err)
}

// SetTaskStatus — смены статуса по цепочке (каждая — из ch.From в ch.To) и записи в журнал — одной транзакцией;
// статус уже другой — ErrConflict, ничего не меняется. Возвращает задание после последней смены.
func (r *Listings) SetTaskStatus(ctx context.Context, chain ...domain.StatusChange) (domain.Task, error) {
	if len(chain) == 0 {
		return domain.Task{}, errors.New("статус задания: пустая цепочка")
	}
	var row store.Task
	err := r.inTx(ctx, func(q *store.Queries) error {
		for _, ch := range chain {
			var err error
			row, err = q.SetTaskStatus(ctx, store.SetTaskStatusParams{
				ID: ch.SubjectID, FromStatus: string(ch.From), ToStatus: string(ch.To), Reason: ch.Reason,
			})
			if err != nil {
				return err
			}
			if err := q.LogStatusChange(ctx, logParams(domain.SubjectTask, ch)); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Task{}, dbErr("статус задания", err)
	}
	return taskOf(row)
}

// PublishedTasks — опубликованные задания (новые сверху) с названием бизнеса и сферы; after — курсор или nil.
func (r *Listings) PublishedTasks(ctx context.Context, f domain.FeedFilter, after *domain.FeedCursor, limit int) ([]domain.Task, error) {
	p := store.ListPublishedTasksParams{Lim: int32(limit), Category: f.Category, City: f.City, Format: f.Format}
	if after != nil {
		p.BeforeAt, p.BeforeID = ts(after.PublishedAt), after.ID
	}
	rows, err := r.q.ListPublishedTasks(ctx, p)
	if err != nil {
		return nil, dbErr("лента заданий", err)
	}
	out := make([]domain.Task, 0, len(rows))
	for _, row := range rows {
		t, err := publishedTaskOf(store.GetPublishedTaskRow(row))
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// PublishedTask — опубликованное задание по id; нет или не опубликовано — ErrNotFound.
func (r *Listings) PublishedTask(ctx context.Context, id int64) (domain.Task, error) {
	row, err := r.q.GetPublishedTask(ctx, id)
	if err != nil {
		return domain.Task{}, dbErr("опубликованное задание", err)
	}
	return publishedTaskOf(row)
}

func publishedTaskOf(row store.GetPublishedTaskRow) (domain.Task, error) {
	t, err := taskOf(store.Task{
		ID: row.ID, OwnerID: row.OwnerID, Status: row.Status, RejectReason: row.RejectReason, Title: row.Title,
		City: row.City, Nationwide: row.Nationwide, Category: row.Category, Formats: row.Formats, Access: row.Access,
		MinViews: row.MinViews, RewardKopecks: row.RewardKopecks, PublishBy: row.PublishBy, PublishTime: row.PublishTime, DeadlineAt: row.DeadlineAt,
		Blocks: row.Blocks, Cover: row.Cover, Photos: row.Photos, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		SubmittedAt: row.SubmittedAt, PublishedAt: row.PublishedAt,
	})
	if err != nil {
		return domain.Task{}, err
	}
	t.Brand, t.CategoryTitle = row.Brand, row.CategoryTitle
	return t, nil
}

// ===== анкеты =====

// Card — анкета автора; нет — ErrNotFound.
func (r *Listings) Card(ctx context.Context, userID int64) (domain.CreatorCard, error) {
	row, err := r.q.GetCard(ctx, userID)
	if err != nil {
		return domain.CreatorCard{}, dbErr("анкета", err)
	}
	return cardOf(row)
}

// SaveCard — создать или обновить анкету (только черновик или отклонённую; иначе — ErrConflict).
func (r *Listings) SaveCard(ctx context.Context, c domain.CreatorCard) (domain.CreatorCard, error) {
	blocks, err := blocksJSON(c.Blocks)
	if err != nil {
		return domain.CreatorCard{}, err
	}
	plats := make([]platformRow, 0, len(c.Platforms))
	for _, p := range c.Platforms {
		plats = append(plats, platformRow{Kind: p.Kind, URL: p.URL, Followers: p.Followers})
	}
	pj, err := json.Marshal(plats)
	if err != nil {
		return domain.CreatorCard{}, fmt.Errorf("площадки в JSON: %w", err)
	}
	var row store.CreatorCard
	err = r.inTx(ctx, func(q *store.Queries) error {
		var err error
		row, err = q.UpsertCard(ctx, store.UpsertCardParams{
			UserID: c.UserID, Name: c.Name, City: c.City, Topics: nonNil(c.Topics), Platforms: pj, Blocks: blocks,
			Avatar: text(c.Avatar), Works: nonNil(c.Works),
		})
		if err != nil {
			return err
		}
		return attach(ctx, q, c.UserID, domain.SubjectCard, c.UserID, cardMedia(c))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CreatorCard{}, domain.ErrConflict
	}
	if err != nil {
		return domain.CreatorCard{}, dbErr("сохранение анкеты", err)
	}
	return cardOf(row)
}

// SetCardStatus — смены статуса анкеты по цепочке и записи в журнал — одной транзакцией; статус уже другой —
// ErrConflict, ничего не меняется.
func (r *Listings) SetCardStatus(ctx context.Context, chain ...domain.StatusChange) (domain.CreatorCard, error) {
	if len(chain) == 0 {
		return domain.CreatorCard{}, errors.New("статус анкеты: пустая цепочка")
	}
	var row store.CreatorCard
	err := r.inTx(ctx, func(q *store.Queries) error {
		for _, ch := range chain {
			var err error
			row, err = q.SetCardStatus(ctx, store.SetCardStatusParams{
				UserID: ch.SubjectID, FromStatus: string(ch.From), ToStatus: string(ch.To), Reason: ch.Reason,
			})
			if err != nil {
				return err
			}
			if err := q.LogStatusChange(ctx, logParams(domain.SubjectCard, ch)); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CreatorCard{}, domain.ErrConflict
	}
	if err != nil {
		return domain.CreatorCard{}, dbErr("статус анкеты", err)
	}
	return cardOf(row)
}

// PublishedCards — опубликованные анкеты (новые сверху) с названиями тем; after — курсор (ID — user_id) или nil.
func (r *Listings) PublishedCards(ctx context.Context, f domain.FeedFilter, after *domain.FeedCursor, limit int) ([]domain.CreatorCard, error) {
	p := store.ListPublishedCardsParams{Lim: int32(limit), Topic: f.Category, City: f.City}
	if after != nil {
		p.BeforeAt, p.BeforeID = ts(after.PublishedAt), after.ID
	}
	rows, err := r.q.ListPublishedCards(ctx, p)
	if err != nil {
		return nil, dbErr("лента анкет", err)
	}
	out := make([]domain.CreatorCard, 0, len(rows))
	for _, row := range rows {
		c, err := publishedCardOf(store.GetPublishedCardRow(row))
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// PublishedCard — опубликованная анкета автора; нет или не опубликована — ErrNotFound.
func (r *Listings) PublishedCard(ctx context.Context, userID int64) (domain.CreatorCard, error) {
	row, err := r.q.GetPublishedCard(ctx, userID)
	if err != nil {
		return domain.CreatorCard{}, dbErr("опубликованная анкета", err)
	}
	return publishedCardOf(row)
}

func publishedCardOf(row store.GetPublishedCardRow) (domain.CreatorCard, error) {
	c, err := cardOf(store.CreatorCard{
		UserID: row.UserID, Status: row.Status, RejectReason: row.RejectReason, Name: row.Name, City: row.City,
		Topics: row.Topics, Platforms: row.Platforms, Blocks: row.Blocks, Avatar: row.Avatar, Works: row.Works,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, SubmittedAt: row.SubmittedAt, PublishedAt: row.PublishedAt,
	})
	if err != nil {
		return domain.CreatorCard{}, err
	}
	c.TopicTitles = nonNil(row.TopicTitles)
	return c, nil
}

// ===== журнал =====

// SubmitStats — сколько раз пользователь отправлял на проверку с момента since и когда впервые (лимит в сутки).
func (r *Listings) SubmitStats(ctx context.Context, actorID int64, since time.Time) (int, time.Time, error) {
	row, err := r.q.SubmitStatsSince(ctx, store.SubmitStatsSinceParams{ActorID: pgtype.Int8{Int64: actorID, Valid: true}, Since: ts(since)})
	return int(row.Sent), row.FirstAt.Time, dbErr("число отправок", err)
}

// History — журнал смен статуса задания или анкеты по порядку.
func (r *Listings) History(ctx context.Context, subject domain.Subject, id int64) ([]domain.StatusChange, error) {
	rows, err := r.q.StatusHistory(ctx, store.StatusHistoryParams{Subject: string(subject), SubjectID: id})
	if err != nil {
		return nil, dbErr("журнал статусов", err)
	}
	out := make([]domain.StatusChange, 0, len(rows))
	for _, l := range rows {
		out = append(out, domain.StatusChange{
			Subject: domain.Subject(l.Subject), SubjectID: l.SubjectID, From: domain.Status(l.FromStatus),
			To: domain.Status(l.ToStatus), Reason: l.Reason, ActorID: l.ActorID.Int64, Auto: l.Auto, At: l.CreatedAt.Time,
		})
	}
	return out, nil
}

// ===== преобразования =====

// blockRow, platformRow — вид в jsonb (теги JSON — только здесь, домен о хранении не знает).
type blockRow struct {
	Kind  string   `json:"kind"`
	Title string   `json:"title"`
	Items []string `json:"items"`
}

type platformRow struct {
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	Followers int64  `json:"followers"`
}

func blocksJSON(bs []domain.Block) ([]byte, error) {
	rows := make([]blockRow, 0, len(bs))
	for _, b := range bs {
		rows = append(rows, blockRow{Kind: b.Kind, Title: b.Title, Items: nonNil(b.Items)})
	}
	out, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("блоки в JSON: %w", err)
	}
	return out, nil
}

func blocksOf(raw []byte) ([]domain.Block, error) {
	var rows []blockRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("блоки из JSON: %w", err)
	}
	out := make([]domain.Block, 0, len(rows))
	for _, b := range rows {
		out = append(out, domain.Block{Kind: b.Kind, Title: b.Title, Items: nonNil(b.Items)})
	}
	return out, nil
}

func taskOf(t store.Task) (domain.Task, error) {
	blocks, err := blocksOf(t.Blocks)
	if err != nil {
		return domain.Task{}, err
	}
	out := domain.Task{
		ID: t.ID, OwnerID: t.OwnerID, Status: domain.Status(t.Status), RejectReason: t.RejectReason, Title: t.Title,
		City: t.City, Nationwide: t.Nationwide, Category: t.Category.String, Formats: nonNil(t.Formats), Access: t.Access,
		MinViews: t.MinViews, RewardKopecks: t.RewardKopecks, Blocks: blocks,
		Cover: t.Cover.String, Photos: nonNil(t.Photos), CreatedAt: t.CreatedAt.Time, UpdatedAt: t.UpdatedAt.Time,
		SubmittedAt: tsPtr(t.SubmittedAt), PublishedAt: tsPtr(t.PublishedAt), DeadlineAt: tsPtr(t.DeadlineAt),
	}
	if t.PublishTime.Valid {
		m := t.PublishTime.Microseconds / 60e6
		out.PublishTime = fmt.Sprintf("%02d:%02d", m/60, m%60)
	}
	if t.PublishBy.Valid {
		d := t.PublishBy.Time.UTC()
		out.PublishBy = &d
	}
	return out, nil
}

func cardOf(c store.CreatorCard) (domain.CreatorCard, error) {
	blocks, err := blocksOf(c.Blocks)
	if err != nil {
		return domain.CreatorCard{}, err
	}
	var plats []platformRow
	if err := json.Unmarshal(c.Platforms, &plats); err != nil {
		return domain.CreatorCard{}, fmt.Errorf("площадки из JSON: %w", err)
	}
	out := domain.CreatorCard{
		UserID: c.UserID, Status: domain.Status(c.Status), RejectReason: c.RejectReason, Name: c.Name, City: c.City,
		Topics: nonNil(c.Topics), Platforms: make([]domain.Platform, 0, len(plats)), Blocks: blocks,
		Avatar: c.Avatar.String, Works: nonNil(c.Works), CreatedAt: c.CreatedAt.Time, UpdatedAt: c.UpdatedAt.Time,
		SubmittedAt: tsPtr(c.SubmittedAt), PublishedAt: tsPtr(c.PublishedAt),
	}
	for _, p := range plats {
		out.Platforms = append(out.Platforms, domain.Platform{Kind: p.Kind, URL: p.URL, Followers: p.Followers})
	}
	return out, nil
}

// attach — прикрепить фото к заданию или анкете и открепить те, что из них убрали. Чужое или прикреплённое
// к другому фото не прикрепится — тогда ErrConflict (сервис проверяет заранее, это защита от гонки).
func attach(ctx context.Context, q *store.Queries, ownerID int64, s domain.Subject, id int64, ids []string) error {
	subj, sid := text(string(s)), pgInt8(id)
	if err := q.DetachMedia(ctx, store.DetachMediaParams{Subject: subj, SubjectID: sid, Keep: nonNil(ids)}); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	n, err := q.AttachMedia(ctx, store.AttachMediaParams{Subject: subj, SubjectID: sid, OwnerID: ownerID, Ids: ids})
	if err != nil {
		return err
	}
	if int(n) != len(ids) {
		return domain.ErrConflict
	}
	return nil
}

func taskMedia(t domain.Task) []string {
	ids := append([]string(nil), t.Photos...)
	if t.Cover != "" {
		ids = append(ids, t.Cover)
	}
	return ids
}

func cardMedia(c domain.CreatorCard) []string {
	ids := append([]string(nil), c.Works...)
	if c.Avatar != "" {
		ids = append(ids, c.Avatar)
	}
	return ids
}

func pgInt8(n int64) pgtype.Int8 { return pgtype.Int8{Int64: n, Valid: true} }

func logParams(s domain.Subject, ch domain.StatusChange) store.LogStatusChangeParams {
	return store.LogStatusChangeParams{
		Subject: string(s), SubjectID: ch.SubjectID, FromStatus: string(ch.From), ToStatus: string(ch.To),
		Reason: ch.Reason, ActorID: pgtype.Int8{Int64: ch.ActorID, Valid: ch.ActorID != 0}, Auto: ch.Auto,
	}
}

func date(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *t, Valid: true}
}

func tsPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// clockTime — «ЧЧ:ММ» → time в базе ("" — NULL).
func clockTime(hm string) pgtype.Time {
	var h, m int64
	if _, err := fmt.Sscanf(hm, "%d:%d", &h, &m); err != nil || hm == "" {
		return pgtype.Time{}
	}
	return pgtype.Time{Microseconds: (h*60 + m) * 60e6, Valid: true}
}

func tsOpt(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return ts(*t)
}

// ExpireTasks — опубликованные со сроком не позже now — в «завершено» (с журналом); id завершённых.
func (r *Listings) ExpireTasks(ctx context.Context, now time.Time) ([]int64, error) {
	ids, err := r.q.ExpireTasks(ctx, ts(now))
	return ids, dbErr("завершение просроченных заданий", err)
}

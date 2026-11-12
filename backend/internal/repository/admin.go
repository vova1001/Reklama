package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/store"
)

// Admin — данные админки в PostgreSQL: 2FA, журнал действий, настройки, очередь проверки (аренда, решения),
// свои категории и форматы, жалобы, пользователи, сводка. SQL — только в queries/admin.sql.
// Каждое изменение сотрудника пишется в журнал действий в той же транзакции.
type Admin struct {
	db DB
	q  *store.Queries
}

// NewAdmin создаёт репозиторий админки.
func NewAdmin(db DB) *Admin { return &Admin{db: db, q: store.New(db)} }

func (r *Admin) inTx(ctx context.Context, fn func(q *store.Queries) error) error {
	return inTx(ctx, r.db, r.q, fn)
}

// ErrLastAdmin — нельзя оставить сайт без администратора.
var ErrLastAdmin = errors.New("последний администратор")

// ===== 2FA =====

// TOTP — секрет 2FA сотрудника; нет — ErrNotFound.
func (r *Admin) TOTP(ctx context.Context, userID int64) (domain.TOTP, error) {
	row, err := r.q.GetStaffTOTP(ctx, userID)
	if err != nil {
		return domain.TOTP{}, dbErr("2FA", err)
	}
	return domain.TOTP{UserID: row.UserID, Secret: row.Secret, ConfirmedAt: tsPtr(row.ConfirmedAt), LastStep: row.LastStep, LockedUntil: tsPtr(row.LockedUntil)}, nil
}

// SaveTOTPSecret — новый (неподтверждённый) секрет; 2FA уже подтверждена — ErrConflict.
func (r *Admin) SaveTOTPSecret(ctx context.Context, userID int64, secret []byte) error {
	_, err := r.q.UpsertStaffTOTP(ctx, store.UpsertStaffTOTPParams{UserID: userID, Secret: secret})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrConflict
	}
	return dbErr("секрет 2FA", err)
}

// AcceptTOTPStep — принять шаг кода (больше последнего принятого); confirming — подтверждение настройки.
// false — шаг уже использован (повтор кода) или состояние не то.
func (r *Admin) AcceptTOTPStep(ctx context.Context, userID, step int64, confirming bool, now time.Time) (bool, error) {
	n, err := r.q.AcceptTOTPStep(ctx, store.AcceptTOTPStepParams{UserID: userID, Step: step, Confirming: confirming, Now: ts(now)})
	return n == 1, dbErr("шаг 2FA", err)
}

// TOTPFailure — неверный код; true — достигнут предел, 2FA заблокирована до lockUntil.
func (r *Admin) TOTPFailure(ctx context.Context, userID int64, maxFailures int, lockUntil time.Time) (bool, error) {
	row, err := r.q.TOTPFailure(ctx, store.TOTPFailureParams{UserID: userID, MaxFailures: int32(maxFailures), LockUntil: ts(lockUntil)})
	if err != nil {
		return false, dbErr("попытка 2FA", err)
	}
	return row.Failures == 0 && row.LockedUntil.Valid && row.LockedUntil.Time.Equal(lockUntil.Truncate(time.Microsecond)), nil
}

// ReplaceBackupCodes — заменить резервные коды (хеши) и записать в журнал — одной транзакцией.
func (r *Admin) ReplaceBackupCodes(ctx context.Context, userID int64, hashes [][]byte, e domain.AuditEntry) error {
	err := r.inTx(ctx, func(q *store.Queries) error {
		if err := q.DeleteBackupCodes(ctx, userID); err != nil {
			return err
		}
		for _, h := range hashes {
			if err := q.InsertBackupCode(ctx, store.InsertBackupCodeParams{UserID: userID, CodeHash: h}); err != nil {
				return err
			}
		}
		return insertAudit(ctx, q, e)
	})
	return dbErr("резервные коды", err)
}

// UseBackupCode — погасить резервный код; false — нет такого или уже использован.
func (r *Admin) UseBackupCode(ctx context.Context, userID int64, hash []byte, now time.Time) (bool, error) {
	n, err := r.q.UseBackupCode(ctx, store.UseBackupCodeParams{UserID: userID, CodeHash: hash, Now: ts(now)})
	return n == 1, dbErr("резервный код", err)
}

// BackupCodesLeft — сколько резервных кодов не использовано.
func (r *Admin) BackupCodesLeft(ctx context.Context, userID int64) (int, error) {
	n, err := r.q.CountBackupCodes(ctx, userID)
	return int(n), dbErr("резервные коды", err)
}

// ResetTwoFactor — удалить 2FA и резервные коды, снять подтверждение во всех сессиях; с записью в журнал.
func (r *Admin) ResetTwoFactor(ctx context.Context, userID int64, e domain.AuditEntry) error {
	err := r.inTx(ctx, func(q *store.Queries) error {
		if err := q.DeleteStaffTOTP(ctx, userID); err != nil {
			return err
		}
		if err := q.DeleteBackupCodes(ctx, userID); err != nil {
			return err
		}
		if err := q.ClearUserSessionsMFA(ctx, userID); err != nil {
			return err
		}
		return insertAudit(ctx, q, e)
	})
	return dbErr("сброс 2FA", err)
}

// ===== журнал действий =====

// Audit — записать действие в журнал.
func (r *Admin) Audit(ctx context.Context, e domain.AuditEntry) error {
	return dbErr("журнал действий", insertAudit(ctx, r.q, e))
}

func insertAudit(ctx context.Context, q *store.Queries, e domain.AuditEntry) error {
	before, err := jsonOpt(e.Before)
	if err != nil {
		return err
	}
	after, err := jsonOpt(e.After)
	if err != nil {
		return err
	}
	_, err = q.InsertAudit(ctx, store.InsertAuditParams{
		ActorID: pgtype.Int8{Int64: e.ActorID, Valid: e.ActorID != 0}, ActorLabel: e.ActorLabel, ActorRole: string(e.ActorRole),
		Action: e.Action, Subject: e.Subject, SubjectID: e.SubjectID, Before: before, After: after, Reason: e.Reason,
		Ip: e.IP, UserAgent: e.UserAgent,
	})
	return err
}

func jsonOpt(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("журнал: JSON: %w", err)
	}
	return b, nil
}

// SearchAudit — поиск по журналу, новые сверху.
func (r *Admin) SearchAudit(ctx context.Context, f domain.AuditFilter) ([]domain.AuditEntry, error) {
	rows, err := r.q.SearchAudit(ctx, store.SearchAuditParams{
		Q: f.Query, ActorID: f.ActorID, Action: f.Action, Subject: f.Subject, SubjectID: f.SubjectID,
		Since: tsOpt(f.Since), Until: tsOpt(f.Until), BeforeID: f.BeforeID, Lim: int32(f.Limit),
	})
	if err != nil {
		return nil, dbErr("поиск по журналу", err)
	}
	out := make([]domain.AuditEntry, 0, len(rows))
	for _, a := range rows {
		out = append(out, domain.AuditEntry{
			ID: a.ID, ActorID: a.ActorID.Int64, ActorLabel: a.ActorLabel, ActorRole: domain.StaffRole(a.ActorRole), Action: a.Action,
			Subject: a.Subject, SubjectID: a.SubjectID, BeforeJSON: a.Before, AfterJSON: a.After, Reason: a.Reason,
			IP: a.Ip, UserAgent: a.UserAgent, At: a.CreatedAt.Time,
		})
	}
	return out, nil
}

// ===== настройки =====

// Setting — значение настройки; ok=false — не задана (значение по умолчанию — из окружения).
func (r *Admin) Setting(ctx context.Context, key string) (string, bool, error) {
	row, err := r.q.GetSetting(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, dbErr("настройка", err)
	}
	return row.Value, true, nil
}

// SetSetting — сохранить настройку и записать в журнал.
func (r *Admin) SetSetting(ctx context.Context, key, value string, by int64, e domain.AuditEntry) error {
	err := r.inTx(ctx, func(q *store.Queries) error {
		if _, err := q.UpsertSetting(ctx, store.UpsertSettingParams{Key: key, Value: value, UpdatedBy: pgtype.Int8{Int64: by, Valid: by != 0}}); err != nil {
			return err
		}
		return insertAudit(ctx, q, e)
	})
	return dbErr("сохранение настройки", err)
}

// ===== аренда =====

// ClaimLease — взять в работу до until (своё — продлить); в работе у другого — ErrConflict.
func (r *Admin) ClaimLease(ctx context.Context, kind domain.ReviewKind, itemID string, moderatorID int64, now, until time.Time) (domain.Lease, error) {
	row, err := r.q.ClaimLease(ctx, store.ClaimLeaseParams{Kind: string(kind), ItemID: itemID, ModeratorID: moderatorID, Now: ts(now), ExpiresAt: ts(until)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Lease{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Lease{}, dbErr("аренда", err)
	}
	return leaseOf(row), nil
}

// Lease — кто держит элемент; нет — ErrNotFound.
func (r *Admin) Lease(ctx context.Context, kind domain.ReviewKind, itemID string) (domain.Lease, error) {
	row, err := r.q.GetLease(ctx, store.GetLeaseParams{Kind: string(kind), ItemID: itemID})
	if err != nil {
		return domain.Lease{}, dbErr("аренда", err)
	}
	return leaseOf(row), nil
}

// ReleaseLease — отпустить свою аренду.
func (r *Admin) ReleaseLease(ctx context.Context, kind domain.ReviewKind, itemID string, moderatorID int64) error {
	return dbErr("аренда", r.q.ReleaseLease(ctx, store.ReleaseLeaseParams{Kind: string(kind), ItemID: itemID, ModeratorID: moderatorID}))
}

// DeleteExpiredLeases — убрать истёкшие аренды старше before.
func (r *Admin) DeleteExpiredLeases(ctx context.Context, before time.Time) (int64, error) {
	n, err := r.q.DeleteExpiredLeases(ctx, ts(before))
	return n, dbErr("истёкшие аренды", err)
}

func leaseOf(l store.ReviewLease) domain.Lease {
	return domain.Lease{Kind: domain.ReviewKind(l.Kind), ItemID: l.ItemID, ModeratorID: l.ModeratorID, TakenAt: l.TakenAt.Time, ExpiresAt: l.ExpiresAt.Time}
}

// ===== очередь =====

func kindsOf(ks []domain.ReviewKind) []string {
	out := make([]string, 0, len(ks))
	for _, k := range ks {
		out = append(out, string(k))
	}
	return out
}

// Queue — страница очереди (без взятых другими и без ждущих применения), всего по отбору и самое старое.
func (r *Admin) Queue(ctx context.Context, f domain.QueueFilter, me int64, now time.Time) ([]domain.QueueItem, int, *time.Time, error) {
	rows, err := r.q.ListQueue(ctx, store.ListQueueParams{
		Me: me, Now: ts(now), Kinds: kindsOf(f.Kinds), Categories: nonNil(f.Categories), City: f.City,
		Since: tsOpt(f.Since), Until: tsOpt(f.Until), Q: f.Query, Newest: f.Newest, Lim: int32(f.Limit), Off: int32(f.Offset),
	})
	if err != nil {
		return nil, 0, nil, dbErr("очередь", err)
	}
	cnt, err := r.q.CountQueue(ctx, store.CountQueueParams{
		Me: me, Now: ts(now), Kinds: kindsOf(f.Kinds), Categories: nonNil(f.Categories), City: f.City,
		Since: tsOpt(f.Since), Until: tsOpt(f.Until), Q: f.Query,
	})
	if err != nil {
		return nil, 0, nil, dbErr("очередь: число", err)
	}
	out := make([]domain.QueueItem, 0, len(rows))
	for _, q := range rows {
		out = append(out, domain.QueueItem{
			Kind: domain.ReviewKind(q.Kind), ID: q.ItemID, Title: q.Title, OwnerID: q.OwnerID, OwnerName: q.OwnerName,
			Categories: nonNil(q.Categories), City: q.City, WaitingSince: q.WaitingSince.Time, Mine: q.Mine,
		})
	}
	return out, int(cnt.Total), tsPtr(cnt.Oldest), nil
}

// QueueItem — элемент, если он ещё ждёт проверки; иначе ErrNotFound.
func (r *Admin) QueueItem(ctx context.Context, kind domain.ReviewKind, itemID string) (domain.QueueItem, error) {
	q, err := r.q.QueueItem(ctx, store.QueueItemParams{Kind: string(kind), ItemID: itemID})
	if err != nil {
		return domain.QueueItem{}, dbErr("элемент очереди", err)
	}
	return domain.QueueItem{Kind: domain.ReviewKind(q.Kind), ID: q.ItemID, Title: q.Title, OwnerID: q.OwnerID,
		Categories: nonNil(q.Categories), City: q.City, WaitingSince: q.WaitingSince.Time}, nil
}

// QueueByKind — сколько ждёт по видам.
func (r *Admin) QueueByKind(ctx context.Context) ([]domain.QueueCount, error) {
	rows, err := r.q.QueueByKind(ctx)
	if err != nil {
		return nil, dbErr("очередь по видам", err)
	}
	out := make([]domain.QueueCount, 0, len(rows))
	for _, x := range rows {
		out = append(out, domain.QueueCount{Kind: domain.ReviewKind(x.Kind), Total: int(x.Total), Oldest: tsPtr(x.Oldest)})
	}
	return out, nil
}

// ===== решения =====

// CreateDecision — решение (применится в d.ApplyAt), аренда продлевается до применения, запись в журнал — одной
// транзакцией. По элементу уже есть ждущее решение — ErrConflict.
func (r *Admin) CreateDecision(ctx context.Context, d domain.ReviewDecision, leaseUntil time.Time, e domain.AuditEntry) (domain.ReviewDecision, error) {
	var row store.ReviewDecision
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		row, err = q.CreateDecision(ctx, store.CreateDecisionParams{
			Kind: string(d.Kind), ItemID: d.ItemID, OwnerID: d.OwnerID, ModeratorID: d.ModeratorID, Action: string(d.Action),
			Reason: d.Reason, Notes: notesJSON(d.Notes), WaitingSince: tsOpt(d.WaitingSince), ApplyAt: ts(d.ApplyAt),
		})
		if err != nil {
			return err
		}
		if _, err := q.ClaimLease(ctx, store.ClaimLeaseParams{Kind: string(d.Kind), ItemID: strconv.FormatInt(d.ItemID, 10),
			ModeratorID: d.ModeratorID, Now: ts(d.CreatedAt), ExpiresAt: ts(leaseUntil)}); err != nil {
			return err
		}
		e.SubjectID = strconv.FormatInt(d.ItemID, 10)
		e.After = map[string]any{"decision": row.ID, "action": d.Action, "reason": d.Reason, "notes": d.Notes}
		return insertAudit(ctx, q, e)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ReviewDecision{}, domain.ErrConflict
	}
	if err != nil {
		return domain.ReviewDecision{}, dbErr("решение", err)
	}
	return decisionOf(row), nil
}

// UndoDecision — отменить своё ждущее решение до применения; поздно или чужое — ErrNotFound.
func (r *Admin) UndoDecision(ctx context.Context, id, moderatorID int64, now time.Time, e domain.AuditEntry) (domain.ReviewDecision, error) {
	var row store.ReviewDecision
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		if row, err = q.UndoDecision(ctx, store.UndoDecisionParams{ID: id, ModeratorID: moderatorID, Now: ts(now)}); err != nil {
			return err
		}
		e.SubjectID = strconv.FormatInt(row.ItemID, 10)
		e.Before = map[string]any{"decision": row.ID, "action": row.Action}
		return insertAudit(ctx, q, e)
	})
	if err != nil {
		return domain.ReviewDecision{}, dbErr("отмена решения", err)
	}
	return decisionOf(row), nil
}

// ClaimDueDecision — одно наступившее решение под аренду исполнителя; нет — ErrNotFound.
func (r *Admin) ClaimDueDecision(ctx context.Context, now, lockUntil time.Time) (domain.ReviewDecision, error) {
	row, err := r.q.ClaimDueDecision(ctx, store.ClaimDueDecisionParams{Now: ts(now), LockUntil: ts(lockUntil)})
	if err != nil {
		return domain.ReviewDecision{}, dbErr("решение к применению", err)
	}
	return decisionOf(row), nil
}

// FinishDecision — решение применено (applied) или не применилось (failed, с причиной); аренда элемента снимается,
// в журнал — итог.
func (r *Admin) FinishDecision(ctx context.Context, d domain.ReviewDecision, state, errText string, at time.Time, e domain.AuditEntry) error {
	err := r.inTx(ctx, func(q *store.Queries) error {
		applied := pgtype.Timestamptz{}
		if state == domain.DecisionApplied {
			applied = ts(at)
		}
		if err := q.FinishDecision(ctx, store.FinishDecisionParams{ID: d.ID, State: state, Error: errText, AppliedAt: applied}); err != nil {
			return err
		}
		if err := q.DeleteLease(ctx, store.DeleteLeaseParams{Kind: string(d.Kind), ItemID: strconv.FormatInt(d.ItemID, 10)}); err != nil {
			return err
		}
		return insertAudit(ctx, q, e)
	})
	return dbErr("итог решения", err)
}

// NextDecisionAt — когда применить ближайшее ждущее решение; nil — ждущих нет.
func (r *Admin) NextDecisionAt(ctx context.Context) (*time.Time, error) {
	t, err := r.q.NextDecisionAt(ctx)
	return tsPtr(t), dbErr("ближайшее решение", err)
}

// Decision — решение по id.
func (r *Admin) Decision(ctx context.Context, id int64) (domain.ReviewDecision, error) {
	row, err := r.q.GetDecision(ctx, id)
	if err != nil {
		return domain.ReviewDecision{}, dbErr("решение", err)
	}
	return decisionOf(row), nil
}

// OwnerDecisions — прошлые решения по заданиям и анкете владельца, новые сверху.
func (r *Admin) OwnerDecisions(ctx context.Context, ownerID int64, limit int) ([]domain.ReviewDecision, error) {
	rows, err := r.q.OwnerDecisions(ctx, store.OwnerDecisionsParams{OwnerID: ownerID, Lim: int32(limit)})
	if err != nil {
		return nil, dbErr("история решений", err)
	}
	out := make([]domain.ReviewDecision, 0, len(rows))
	for _, x := range rows {
		d := decisionOf(store.ReviewDecision{ID: x.ID, Kind: x.Kind, ItemID: x.ItemID, OwnerID: x.OwnerID, ModeratorID: x.ModeratorID,
			Action: x.Action, Reason: x.Reason, Notes: x.Notes, WaitingSince: x.WaitingSince, State: x.State, ApplyAt: x.ApplyAt,
			AppliedAt: x.AppliedAt, Error: x.Error, CreatedAt: x.CreatedAt})
		d.Moderator = x.ModeratorName
		out = append(out, d)
	}
	return out, nil
}

func decisionOf(d store.ReviewDecision) domain.ReviewDecision {
	return domain.ReviewDecision{
		ID: d.ID, Kind: domain.ReviewKind(d.Kind), ItemID: d.ItemID, OwnerID: d.OwnerID, ModeratorID: d.ModeratorID,
		Action: domain.ReviewAction(d.Action), Reason: d.Reason, Notes: notesOf(d.Notes), WaitingSince: tsPtr(d.WaitingSince),
		State: d.State, ApplyAt: d.ApplyAt.Time, AppliedAt: tsPtr(d.AppliedAt), Error: d.Error, CreatedAt: d.CreatedAt.Time,
	}
}

// ===== свои категории =====

// CategoryUsage — в скольких заданиях и анкетах выбрана категория.
func (r *Admin) CategoryUsage(ctx context.Context, slug string) (int, int, error) {
	row, err := r.q.CategoryUsage(ctx, slug)
	return int(row.Tasks), int(row.Cards), dbErr("использование категории", err)
}

// ApprovedCategory — общая категория по slug; нет — ErrNotFound.
func (r *Admin) ApprovedCategory(ctx context.Context, slug string) (domain.CategoryInfo, error) {
	row, err := r.q.ApprovedCategory(ctx, slug)
	if err != nil {
		return domain.CategoryInfo{}, dbErr("общая категория", err)
	}
	return categoryOf(row), nil
}

// MergeCategory — своя категория заменяется общей в заданиях и анкетах, помечается «объединена»; журнал модерации
// и журнал действий — одной транзакцией. Уже не на проверке — ErrConflict.
func (r *Admin) MergeCategory(ctx context.Context, ch domain.StatusChange, fromSlug, intoSlug string, e domain.AuditEntry) error {
	err := r.inTx(ctx, func(q *store.Queries) error {
		if _, err := q.MarkCategoryMerged(ctx, store.MarkCategoryMergedParams{ID: ch.SubjectID, IntoSlug: text(intoSlug)}); err != nil {
			return err
		}
		if err := q.ReplaceCategoryInTasks(ctx, store.ReplaceCategoryInTasksParams{FromSlug: fromSlug, ToSlug: intoSlug}); err != nil {
			return err
		}
		if err := q.ReplaceCategoryInCards(ctx, store.ReplaceCategoryInCardsParams{FromSlug: fromSlug, ToSlug: intoSlug}); err != nil {
			return err
		}
		if err := q.LogCategoryChange(ctx, store.LogCategoryChangeParams{SubjectID: ch.SubjectID, FromStatus: string(ch.From),
			ToStatus: "merged", Reason: ch.Reason, ActorID: pgtype.Int8{Int64: ch.ActorID, Valid: ch.ActorID != 0}}); err != nil {
			return err
		}
		return insertAudit(ctx, q, e)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrConflict
	}
	return dbErr("объединение категории", err)
}

// ===== свои форматы =====

// FormatUsage — в скольких заданиях формат и как он написан.
func (r *Admin) FormatUsage(ctx context.Context, norm string) (int, string, error) {
	row, err := r.q.FormatUsage(ctx, norm)
	return int(row.Tasks), row.Title, dbErr("использование формата", err)
}

// DecideFormat — решение по своему формату (при объединении — замена в заданиях) и журнал — одной транзакцией;
// решение уже есть — ErrConflict.
func (r *Admin) DecideFormat(ctx context.Context, norm, title, status, into string, by int64, e domain.AuditEntry) error {
	err := r.inTx(ctx, func(q *store.Queries) error {
		if _, err := q.InsertFormatDecision(ctx, store.InsertFormatDecisionParams{Norm: norm, Title: title, Status: status, MergedInto: into,
			DecidedBy: pgtype.Int8{Int64: by, Valid: by != 0}}); err != nil {
			return err
		}
		if status == "merged" {
			if _, err := q.ReplaceFormatInTasks(ctx, store.ReplaceFormatInTasksParams{Norm: norm, ToFormat: into}); err != nil {
				return err
			}
		}
		return insertAudit(ctx, q, e)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrConflict
	}
	return dbErr("решение по формату", err)
}

// FormatStatus — решение по своему формату (approved | rejected | merged и во что); нет решения — "".
func (r *Admin) FormatStatus(ctx context.Context, norm string) (string, string, error) {
	row, err := r.q.GetFormatDecision(ctx, norm)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", dbErr("решение по формату", err)
	}
	return row.Status, row.MergedInto, nil
}

// ApprovedFormats — свои форматы, одобренные в общие.
func (r *Admin) ApprovedFormats(ctx context.Context) ([]string, error) {
	out, err := r.q.ApprovedFormats(ctx)
	return nonNil(out), dbErr("одобренные форматы", err)
}

// ===== жалобы =====

// CreateComplaint — жалоба; такая же открытая уже есть — created=false (без новой строки).
func (r *Admin) CreateComplaint(ctx context.Context, c domain.Complaint) (bool, error) {
	_, err := r.q.CreateComplaint(ctx, store.CreateComplaintParams{
		ReporterID: c.ReporterID, Subject: c.Subject, SubjectID: c.SubjectID, TargetUserID: c.TargetUserID, Reason: c.Reason, Comment: c.Comment,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, dbErr("жалоба", err)
}

// ComplaintsSince — сколько жалоб человек отправил после since (лимит).
func (r *Admin) ComplaintsSince(ctx context.Context, reporterID int64, since time.Time) (int, error) {
	n, err := r.q.CountComplaintsSince(ctx, store.CountComplaintsSinceParams{ReporterID: reporterID, Since: ts(since)})
	return int(n), dbErr("число жалоб", err)
}

// ComplaintGroups — открытые жалобы по предметам (старые первыми) и сколько всего предметов.
func (r *Admin) ComplaintGroups(ctx context.Context, subject string, limit, offset int) ([]domain.ComplaintGroup, int, error) {
	rows, err := r.q.OpenComplaintGroups(ctx, store.OpenComplaintGroupsParams{Subject: subject, Lim: int32(limit), Off: int32(offset)})
	if err != nil {
		return nil, 0, dbErr("жалобы", err)
	}
	total, err := r.q.CountOpenComplaintGroups(ctx)
	if err != nil {
		return nil, 0, dbErr("жалобы: число", err)
	}
	out := make([]domain.ComplaintGroup, 0, len(rows))
	for _, g := range rows {
		out = append(out, domain.ComplaintGroup{Subject: g.Subject, SubjectID: g.SubjectID, TargetUserID: g.TargetUserID,
			TargetName: g.TargetName, Total: int(g.Total), Reasons: nonNil(g.Reasons), FirstAt: g.FirstAt.Time})
	}
	return out, int(total), nil
}

// ComplaintsFor — все жалобы на предмет (новые сверху).
func (r *Admin) ComplaintsFor(ctx context.Context, subject string, id int64) ([]domain.Complaint, error) {
	rows, err := r.q.ComplaintsFor(ctx, store.ComplaintsForParams{Subject: subject, SubjectID: id})
	if err != nil {
		return nil, dbErr("жалобы на предмет", err)
	}
	out := make([]domain.Complaint, 0, len(rows))
	for _, c := range rows {
		x := complaintOf(store.Complaint{ID: c.ID, ReporterID: c.ReporterID, Subject: c.Subject, SubjectID: c.SubjectID,
			TargetUserID: c.TargetUserID, Reason: c.Reason, Comment: c.Comment, Status: c.Status, Action: c.Action,
			Resolution: c.Resolution, ResolvedBy: c.ResolvedBy, ResolvedAt: c.ResolvedAt, CreatedAt: c.CreatedAt})
		x.Reporter = c.ReporterName
		out = append(out, x)
	}
	return out, nil
}

// ResolveComplaints — закрыть все открытые жалобы на предмет и записать в журнал; сколько закрыто
// (0 — уже закрыты, ErrNotFound).
func (r *Admin) ResolveComplaints(ctx context.Context, subject string, id int64, status, action, resolution string, by int64, now time.Time, e domain.AuditEntry) (int, error) {
	var n int64
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		n, err = q.ResolveComplaints(ctx, store.ResolveComplaintsParams{Subject: subject, SubjectID: id, Status: status, Action: action,
			Resolution: resolution, ResolvedBy: pgtype.Int8{Int64: by, Valid: by != 0}, Now: ts(now)})
		if err != nil {
			return err
		}
		if n == 0 {
			return pgx.ErrNoRows
		}
		e.After = map[string]any{"status": status, "action": action, "closed": n}
		return insertAudit(ctx, q, e)
	})
	if err != nil {
		return 0, dbErr("решение по жалобам", err)
	}
	return int(n), nil
}

// ComplaintsAgainst — жалобы на контент и профиль пользователя (новые сверху).
func (r *Admin) ComplaintsAgainst(ctx context.Context, userID int64, limit int) ([]domain.Complaint, error) {
	rows, err := r.q.ComplaintsAgainst(ctx, store.ComplaintsAgainstParams{TargetUserID: userID, Lim: int32(limit)})
	if err != nil {
		return nil, dbErr("жалобы на пользователя", err)
	}
	out := make([]domain.Complaint, 0, len(rows))
	for _, c := range rows {
		out = append(out, complaintOf(c))
	}
	return out, nil
}

func complaintOf(c store.Complaint) domain.Complaint {
	return domain.Complaint{ID: c.ID, ReporterID: c.ReporterID, Subject: c.Subject, SubjectID: c.SubjectID, TargetUserID: c.TargetUserID,
		Reason: c.Reason, Comment: c.Comment, Status: c.Status, Action: c.Action, Resolution: c.Resolution,
		ResolvedBy: c.ResolvedBy.Int64, ResolvedAt: tsPtr(c.ResolvedAt), CreatedAt: c.CreatedAt.Time}
}

// ===== пользователи =====

// SearchUsers — поиск по id, почте и имени; новые сверху.
func (r *Admin) SearchUsers(ctx context.Context, q string, staffOnly, blockedOnly bool, limit, offset int) ([]domain.UserSummary, error) {
	rows, err := r.q.SearchUsers(ctx, store.SearchUsersParams{Q: q, StaffOnly: staffOnly, BlockedOnly: blockedOnly, Lim: int32(limit), Off: int32(offset)})
	if err != nil {
		return nil, dbErr("поиск пользователей", err)
	}
	out := make([]domain.UserSummary, 0, len(rows))
	for _, u := range rows {
		out = append(out, domain.UserSummary{User: userOf(store.User{ID: u.ID, Name: u.Name, Email: u.Email, AvatarUrl: u.AvatarUrl,
			City: u.City, OnboardedAt: u.OnboardedAt, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt, LastLoginAt: u.LastLoginAt,
			ActiveMode: u.ActiveMode, StaffRole: u.StaffRole, BlockedAt: u.BlockedAt, BlockedReason: u.BlockedReason}),
			BusinessName: u.BusinessName, CreatorName: u.CreatorName})
	}
	return out, nil
}

// UserFull — пользователь с названиями профилей; нет — ErrNotFound.
func (r *Admin) UserFull(ctx context.Context, id int64) (domain.UserSummary, error) {
	u, err := r.q.GetUserFull(ctx, id)
	if err != nil {
		return domain.UserSummary{}, dbErr("пользователь", err)
	}
	return domain.UserSummary{User: userOf(store.User{ID: u.ID, Name: u.Name, Email: u.Email, AvatarUrl: u.AvatarUrl,
		City: u.City, OnboardedAt: u.OnboardedAt, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt, LastLoginAt: u.LastLoginAt,
		ActiveMode: u.ActiveMode, StaffRole: u.StaffRole, BlockedAt: u.BlockedAt, BlockedReason: u.BlockedReason}),
		BusinessName: u.BusinessName, CreatorName: u.CreatorName}, nil
}

// BlockUser — заблокировать: вход закрыт, все сеансы завершены, контент скрыт из ленты; журнал — в той же
// транзакции. Уже заблокирован — ErrConflict. Последнего администратора заблокировать нельзя — ErrLastAdmin.
func (r *Admin) BlockUser(ctx context.Context, id int64, reason string, now time.Time, e domain.AuditEntry) (domain.User, error) {
	var row store.User
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		if row, err = q.BlockUser(ctx, store.BlockUserParams{ID: id, Reason: reason, Now: ts(now)}); err != nil {
			return err
		}
		if row.StaffRole.String == string(domain.RoleAdmin) {
			if n, err := q.CountAdmins(ctx); err != nil {
				return err
			} else if n == 0 {
				return ErrLastAdmin
			}
		}
		if _, err := q.DeleteSessionsOfUser(ctx, id); err != nil {
			return err
		}
		return insertAudit(ctx, q, e)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrConflict
	}
	if errors.Is(err, ErrLastAdmin) {
		return domain.User{}, err
	}
	return userOf(row), dbErr("блокировка", err)
}

// UnblockUser — разблокировать; не заблокирован — ErrConflict.
func (r *Admin) UnblockUser(ctx context.Context, id int64, e domain.AuditEntry) (domain.User, error) {
	var row store.User
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		if row, err = q.UnblockUser(ctx, id); err != nil {
			return err
		}
		return insertAudit(ctx, q, e)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrConflict
	}
	return userOf(row), dbErr("разблокировка", err)
}

// SetStaffRole — назначить или снять роль (подтверждение 2FA в сессиях сбрасывается); журнал — в той же
// транзакции. Без администраторов сайт не остаётся — ErrLastAdmin.
func (r *Admin) SetStaffRole(ctx context.Context, id int64, role domain.StaffRole, e domain.AuditEntry) (domain.User, error) {
	var row store.User
	err := r.inTx(ctx, func(q *store.Queries) error {
		var err error
		if row, err = q.SetStaffRole(ctx, store.SetStaffRoleParams{ID: id, StaffRole: text(string(role))}); err != nil {
			return err
		}
		if n, err := q.CountAdmins(ctx); err != nil {
			return err
		} else if n == 0 {
			return ErrLastAdmin
		}
		if err := q.ClearUserSessionsMFA(ctx, id); err != nil {
			return err
		}
		return insertAudit(ctx, q, e)
	})
	if errors.Is(err, ErrLastAdmin) {
		return domain.User{}, err
	}
	return userOf(row), dbErr("роль", err)
}

// UserLogins — последние входы пользователя.
func (r *Admin) UserLogins(ctx context.Context, userID int64, limit int) ([]domain.LoginEvent, error) {
	rows, err := r.q.UserLogins(ctx, store.UserLoginsParams{UserID: userID, Lim: int32(limit)})
	if err != nil {
		return nil, dbErr("входы", err)
	}
	out := make([]domain.LoginEvent, 0, len(rows))
	for _, l := range rows {
		out = append(out, domain.LoginEvent{Method: l.Method, IP: l.Ip, UserAgent: l.UserAgent, At: l.CreatedAt.Time})
	}
	return out, nil
}

// ===== сводка =====

// DashboardStats — решения, среднее время проверки, новые пользователи, открытые жалобы.
func (r *Admin) DashboardStats(ctx context.Context, dayStart, weekStart time.Time) (domain.DashboardStats, error) {
	row, err := r.q.DashboardStats(ctx, store.DashboardStatsParams{DayStart: ts(dayStart), WeekStart: ts(weekStart)})
	if err != nil {
		return domain.DashboardStats{}, dbErr("сводка", err)
	}
	return domain.DashboardStats{DecidedToday: int(row.DecidedToday), DecidedWeek: int(row.DecidedWeek), AvgReviewSeconds: row.AvgReviewSeconds,
		UsersToday: int(row.UsersToday), UsersWeek: int(row.UsersWeek), OpenComplaints: int(row.OpenComplaints)}, nil
}

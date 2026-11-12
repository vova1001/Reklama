package repository_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // TOTP RFC 6238
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/repository"
	"ohvat/backend/internal/secretbox"
	"ohvat/backend/internal/service"
)

// Админка насквозь на настоящей PostgreSQL: сервисы + репозитории (права, 2FA, очередь с арендой, решения с
// отменой, правки опубликованного, жалобы, блокировка, журнал).

// stepClock — время, которое двигает тест.
type stepClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *stepClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func totpAt(secretB32 string, t time.Time) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretB32)
	if err != nil {
		panic(err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(t.Unix()/30))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%06d", v%1_000_000)
}

func tokenHash(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}

type adminEnv struct {
	pool     *pgxpool.Pool
	ctx      context.Context
	clock    *stepClock
	auth     *repository.Auth
	admin    *repository.Admin
	list     *repository.Listings
	cats     *repository.Categories
	tasks    *service.Tasks
	mod      *service.Moderation
	review   *service.Review
	twoFA    *service.TwoFactor
	staff    *service.StaffUsers
	compl    *service.Complaints
	settings *service.Settings
	notified []service.DecisionNotice
	mu       sync.Mutex
}

func (e *adminEnv) NotifyDecision(_ context.Context, _ int64, n service.DecisionNotice) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.notified = append(e.notified, n)
	return nil
}

type modRepo struct {
	*repository.Listings
	*repository.Categories
}

type targets struct {
	*repository.Listings
	*repository.Admin
}

func newAdminEnv(t *testing.T) *adminEnv {
	pool := testPool(t)
	e := &adminEnv{pool: pool, ctx: context.Background(), clock: &stepClock{t: time.Now().UTC().Truncate(time.Microsecond)}}
	e.auth, e.admin = repository.NewAuth(pool), repository.NewAdmin(pool)
	e.list, e.cats = repository.NewListings(pool), repository.NewCategories(pool)
	e.settings = service.NewSettings(e.admin, domain.ModerationAuto, e.clock, nil)
	deps := service.ListingDeps{Categories: e.cats, Submits: e.list, Media: repository.NewMedia(pool), Clock: e.clock,
		Mode: domain.ModerationAuto, Modes: e.settings, FormatRules: e.admin}
	e.tasks = service.NewTasks(e.list, deps)
	mr := modRepo{e.list, e.cats}
	e.mod = service.NewModeration(mr)
	box, err := secretbox.New(service.TOTPKeyFromSecret("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	e.twoFA = service.NewTwoFactor(service.TwoFactorDeps{Repo: e.admin, Sessions: e.auth, Cipher: box, Clock: e.clock},
		service.TwoFactorConfig{Secret: []byte("test-secret")})
	e.review = service.NewReview(service.ReviewDeps{Repo: e.admin, Content: mr, Moderator: e.mod, Notifier: e, Clock: e.clock},
		service.ReviewConfig{})
	e.staff = service.NewStaffUsers(e.admin, nil, e.clock, func(err error) bool { return errors.Is(err, repository.ErrLastAdmin) })
	e.compl = service.NewComplaints(e.admin, targets{e.list, e.admin}, e.mod, e.staff, e.clock, service.ComplaintsConfig{PerDay: 3})
	return e
}

// staffUser — пользователь с ролью (роль назначается напрямую в базе через сервис первого администратора).
func (e *adminEnv) user(t *testing.T, email string, role domain.StaffRole) domain.User {
	t.Helper()
	u, err := e.auth.CreateUserWithIdentity(e.ctx, domain.NewUser{Name: email, Email: email}, domain.Identity{Provider: "email", Subject: email, Email: email})
	if err != nil {
		t.Fatal(err)
	}
	if role == domain.RoleNone {
		return u
	}
	if u, err = e.auth.GrantBootstrapAdmin(e.ctx, u.ID, domain.AuditEntry{Action: domain.AuditRoleBootstrap}); err != nil {
		t.Fatal(err)
	}
	if role == domain.RoleModerator {
		if u, err = e.admin.SetStaffRole(e.ctx, u.ID, domain.RoleModerator, domain.AuditEntry{Action: domain.AuditRoleSet}); err != nil {
			t.Fatal(err)
		}
	}
	return u
}

func (e *adminEnv) task(t *testing.T, owner domain.User, title string) domain.Task {
	t.Helper()
	by := e.clock.Now().Add(72 * time.Hour).In(time.FixedZone("MSK", 3*3600)).Format(time.DateOnly)
	tk, err := e.tasks.Create(e.ctx, owner, service.TaskInput{Title: title, City: "Казань", Categories: []string{"food"}, Formats: []string{"reels"},
		Access: domain.AccessOpen, MinViews: 1000, RewardKopecks: 50000, PublishBy: by,
		Blocks: []domain.Block{{Kind: domain.BlockText, Title: "Описание", Items: []string{"Снять обзор меню"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestAdminTwoFactorFlow(t *testing.T) {
	e := newAdminEnv(t)
	adm := e.user(t, "admin@x.ru", domain.RoleAdmin)
	plain := e.user(t, "plain@x.ru", domain.RoleNone)
	tok := "session-token-admin"
	if err := e.auth.CreateSession(e.ctx, adm.ID, tokenHash(tok), e.clock.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := e.twoFA.Require(e.ctx, adm, tok); !errors.Is(err, service.ErrTwoFactorSetup) {
		t.Fatalf("без 2FA: %v", err)
	}
	if _, err := e.twoFA.Setup(e.ctx, plain); !errors.Is(err, service.ErrNotStaff) {
		t.Errorf("обычный пользователь: %v", err)
	}
	st, err := e.twoFA.Setup(e.ctx, adm)
	if err != nil || st.Secret == "" || st.URI == "" {
		t.Fatalf("%v %+v", err, st)
	}
	if _, err := e.twoFA.Confirm(e.ctx, adm, tok, "000000"); !errors.Is(err, service.ErrTwoFactorInvalid) && totpAt(st.Secret, e.clock.Now()) != "000000" {
		t.Errorf("неверный код: %v", err)
	}
	code := totpAt(st.Secret, e.clock.Now())
	backup, err := e.twoFA.Confirm(e.ctx, adm, tok, code)
	if err != nil || len(backup) != 10 {
		t.Fatalf("подтверждение: %v %v", err, backup)
	}
	if err := e.twoFA.Require(e.ctx, adm, tok); err != nil {
		t.Fatalf("после подтверждения: %v", err)
	}
	if _, err := e.twoFA.Setup(e.ctx, adm); !errors.Is(err, service.ErrWrongStatus) {
		t.Errorf("повторная настройка: %v", err)
	}
	// через 12 часов — подтвердить снова; тот же код второй раз не проходит (повтор)
	e.clock.add(12*time.Hour + time.Minute)
	if err := e.twoFA.Require(e.ctx, adm, tok); !errors.Is(err, service.ErrTwoFactorRequired) {
		t.Fatalf("12 часов: %v", err)
	}
	code = totpAt(st.Secret, e.clock.Now())
	if err := e.twoFA.Verify(e.ctx, adm, tok, code); err != nil {
		t.Fatalf("код: %v", err)
	}
	if err := e.twoFA.Verify(e.ctx, adm, tok, code); !errors.Is(err, service.ErrTwoFactorInvalid) {
		t.Errorf("повтор кода: %v", err)
	}
	// резервный код — один раз
	if err := e.twoFA.Verify(e.ctx, adm, tok, backup[0]); err != nil {
		t.Fatalf("резервный: %v", err)
	}
	if err := e.twoFA.Verify(e.ctx, adm, tok, backup[0]); !errors.Is(err, service.ErrTwoFactorInvalid) {
		t.Errorf("резервный повторно: %v", err)
	}
	if s, _ := e.twoFA.Status(e.ctx, adm, tok); s.BackupLeft != 9 || !s.Verified {
		t.Errorf("статус: %+v", s)
	}
	// перебор — блокировка
	var lim *service.LimitError
	for i := 0; i < 6; i++ {
		err = e.twoFA.Verify(e.ctx, adm, tok, "12345x")
	}
	if !errors.As(err, &lim) {
		t.Fatalf("перебор: %v", err)
	}
	if err := e.twoFA.Verify(e.ctx, adm, tok, totpAt(st.Secret, e.clock.Now().Add(30*time.Second))); !errors.As(err, &lim) {
		t.Errorf("заблокировано даже с верным кодом: %v", err)
	}
	// сброс 2FA — только администратор и не себе; модератор — нет
	mod := e.user(t, "mod@x.ru", domain.RoleModerator)
	if err := e.twoFA.Reset(e.ctx, mod, adm.ID); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("модератор сбрасывает: %v", err)
	}
	if err := e.twoFA.Reset(e.ctx, adm, adm.ID); !errors.Is(err, service.ErrSelfAction) {
		t.Errorf("себе: %v", err)
	}
	if _, err := e.twoFA.Setup(e.ctx, mod); err != nil {
		t.Fatal(err)
	}
	if err := e.twoFA.Reset(e.ctx, adm, mod.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.TOTP(e.ctx, mod.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("после сброса: %v", err)
	}
	if codes, err := e.twoFA.RegenerateBackup(e.ctx, adm); err != nil || len(codes) != 10 {
		t.Errorf("новые резервные: %v", err)
	}
	// журнал: включение, резервный код, блокировка, сброс — и он не меняется
	es, err := e.admin.SearchAudit(e.ctx, domain.AuditFilter{ActorID: adm.ID, Limit: 50})
	if err != nil || len(es) < 3 {
		t.Fatalf("журнал: %v %d", err, len(es))
	}
	if _, err := e.pool.Exec(e.ctx, "UPDATE audit_log SET reason = 'x'"); err == nil {
		t.Error("журнал изменён")
	}
	if _, err := e.pool.Exec(e.ctx, "DELETE FROM audit_log"); err == nil {
		t.Error("журнал удалён")
	}
}

func TestAdminReviewFlow(t *testing.T) {
	e := newAdminEnv(t)
	adm := e.user(t, "admin@x.ru", domain.RoleAdmin)
	modA := e.user(t, "a@x.ru", domain.RoleModerator)
	modB := e.user(t, "b@x.ru", domain.RoleModerator)
	biz := e.user(t, "biz@x.ru", domain.RoleNone)

	// режим проверки — в базе: модератор менять не может, администратор — да
	if _, err := e.settings.SetModerationMode(e.ctx, modA, domain.ModerationManual); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("модератор меняет режим: %v", err)
	}
	if _, err := e.settings.SetModerationMode(e.ctx, biz, domain.ModerationManual); !errors.Is(err, service.ErrNotStaff) {
		t.Fatalf("пользователь меняет режим: %v", err)
	}
	if st, err := e.settings.SetModerationMode(e.ctx, adm, domain.ModerationManual); err != nil || st.ModerationMode != domain.ModerationManual {
		t.Fatalf("%v %+v", err, st)
	}

	tk := e.task(t, biz, "Обзор кофейни")
	if _, err := e.tasks.Submit(e.ctx, biz, tk.ID); err != nil {
		t.Fatal(err)
	}
	other := e.task(t, biz, "Второе задание")
	if _, err := e.tasks.Submit(e.ctx, biz, other.ID); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(tk.ID, 10)
	if _, err := e.review.Queue(e.ctx, biz, domain.QueueFilter{}); !errors.Is(err, service.ErrNotStaff) {
		t.Fatalf("пользователь видит очередь: %v", err)
	}
	p, err := e.review.Queue(e.ctx, modA, domain.QueueFilter{Kinds: []domain.ReviewKind{domain.KindTask}})
	if err != nil || p.Total != 2 || p.Items[0].ID != id || p.Oldest == nil {
		t.Fatalf("очередь: %v %+v", err, p)
	}
	if p, _ := e.review.Queue(e.ctx, modA, domain.QueueFilter{Query: "Второе"}); p.Total != 1 {
		t.Errorf("поиск: %+v", p)
	}
	if p, _ := e.review.Queue(e.ctx, modA, domain.QueueFilter{City: "Москва"}); p.Total != 0 {
		t.Errorf("отбор по городу: %+v", p)
	}

	// аренда: A взял — B не может и не видит
	if _, err := e.review.Claim(e.ctx, modA, domain.KindTask, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.review.Claim(e.ctx, modB, domain.KindTask, id); !errors.Is(err, service.ErrLeaseHeld) {
		t.Fatalf("второй модератор: %v", err)
	}
	if p, _ := e.review.Queue(e.ctx, modB, domain.QueueFilter{}); p.Total != 1 {
		t.Errorf("взятое другим скрыто: %+v", p)
	}
	if _, err := e.review.Decide(e.ctx, modB, domain.KindTask, id, service.DecisionInput{Action: domain.ActionApprove}); !errors.Is(err, service.ErrLeaseHeld) {
		t.Errorf("решение без аренды: %v", err)
	}
	// «следующий» для B — второе задание
	if next, err := e.review.Next(e.ctx, modB, domain.QueueFilter{}, "", ""); err != nil || next.ID != strconv.FormatInt(other.ID, 10) {
		t.Errorf("следующий: %v %+v", err, next)
	}

	// карточка проверки
	it, err := e.review.Item(e.ctx, modA, domain.KindTask, id)
	if err != nil || it.Task == nil || it.Owner == nil || it.Lease == nil || it.Lease.ModeratorID != modA.ID || len(it.History) == 0 || it.Task.CategoryTitles[0] == "" {
		t.Fatalf("карточка: %v %+v", err, it)
	}

	// отказ без причины — ошибка; с причиной — ждёт 10 с; отмена возвращает в работу
	if _, err := e.review.Decide(e.ctx, modA, domain.KindTask, id, service.DecisionInput{Action: domain.ActionReject, Reason: "нет"}); err == nil {
		t.Error("короткая причина")
	}
	d, err := e.review.Decide(e.ctx, modA, domain.KindTask, id, service.DecisionInput{Action: domain.ActionReject, Reason: "Нет описания результата"})
	if err != nil || d.State != domain.DecisionPending {
		t.Fatalf("%v %+v", err, d)
	}
	if p, _ := e.review.Queue(e.ctx, modA, domain.QueueFilter{Kinds: []domain.ReviewKind{domain.KindTask}}); p.Total != 0 {
		t.Errorf("с решением — не в очереди (второе в работе у B): %+v", p)
	}
	if _, err := e.review.Undo(e.ctx, modB, d.ID); !errors.Is(err, service.ErrUndoExpired) {
		t.Errorf("чужая отмена: %v", err)
	}
	if _, err := e.review.Undo(e.ctx, modA, d.ID); err != nil {
		t.Fatalf("отмена: %v", err)
	}
	if n, err := e.review.ApplyDue(e.ctx); err != nil || n != 0 {
		t.Errorf("отменённое не применяется: %d %v", n, err)
	}
	// на доработку с замечаниями — применится через 10 с
	d, err = e.review.Decide(e.ctx, modA, domain.KindTask, id, service.DecisionInput{Action: domain.ActionRework,
		Notes: map[string]string{"title": "Уточните, что за заведение", "bogus": "x"}})
	if err == nil {
		t.Error("замечание к неизвестному полю")
	}
	d, err = e.review.Decide(e.ctx, modA, domain.KindTask, id, service.DecisionInput{Action: domain.ActionRework,
		Notes: map[string]string{"title": "Уточните, что за заведение"}})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := e.review.ApplyDue(e.ctx); n != 0 {
		t.Error("применено раньше 10 с")
	}
	e.clock.add(11 * time.Second)
	if _, err := e.review.Undo(e.ctx, modA, d.ID); !errors.Is(err, service.ErrUndoExpired) {
		t.Errorf("отмена после 10 с: %v", err)
	}
	if n, err := e.review.ApplyDue(e.ctx); err != nil || n != 1 {
		t.Fatalf("применение: %d %v", n, err)
	}
	got, _ := e.list.Task(e.ctx, tk.ID)
	if got.Status != domain.StatusChangesRequested || got.ReviewNotes["title"] == "" {
		t.Fatalf("на доработке: %+v", got)
	}
	if len(e.notified) != 1 || e.notified[0].Action != domain.ActionRework || e.notified[0].Path == "" {
		t.Errorf("уведомление: %+v", e.notified)
	}
	// владелец поправил и отправил снова → одобрение → опубликовано
	in := service.TaskInput{Title: "Обзор кофейни «Зерно»", City: got.City, Categories: got.Categories, Formats: got.Formats, Access: got.Access,
		MinViews: got.MinViews, RewardKopecks: got.RewardKopecks, PublishBy: got.PublishBy.Format(time.DateOnly), Blocks: got.Blocks}
	if _, err := e.tasks.Update(e.ctx, biz, tk.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tasks.Submit(e.ctx, biz, tk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.review.Claim(e.ctx, modA, domain.KindTask, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.review.Decide(e.ctx, modA, domain.KindTask, id, service.DecisionInput{Action: domain.ActionApprove}); err != nil {
		t.Fatal(err)
	}
	e.clock.add(11 * time.Second)
	if n, _ := e.review.ApplyDue(e.ctx); n != 1 {
		t.Fatal("одобрение не применено")
	}
	if pub, err := e.list.PublishedTask(e.ctx, tk.ID); err != nil || pub.Title != "Обзор кофейни «Зерно»" {
		t.Fatalf("опубликовано: %v %+v", err, pub)
	}

	// правка опубликованного: новая версия ждёт, старая видна всем
	in.Title = "Обзор кофейни — новая версия"
	if _, err := e.tasks.Update(e.ctx, biz, tk.ID, in); err != nil {
		t.Fatalf("правка опубликованного: %v", err)
	}
	if _, err := e.tasks.Submit(e.ctx, biz, tk.ID); err != nil {
		t.Fatal(err)
	}
	if pub, _ := e.list.PublishedTask(e.ctx, tk.ID); pub.Title != "Обзор кофейни «Зерно»" {
		t.Errorf("до одобрения видна старая: %q", pub.Title)
	}
	if p, _ := e.review.Queue(e.ctx, modA, domain.QueueFilter{Kinds: []domain.ReviewKind{domain.KindTaskRevision}}); p.Total != 1 || p.Items[0].Title != in.Title {
		t.Fatalf("правка в очереди: %+v", p)
	}
	ri, err := e.review.Item(e.ctx, modA, domain.KindTaskRevision, id)
	if err != nil || ri.Task.Title != in.Title || ri.LiveTask.Title != "Обзор кофейни «Зерно»" {
		t.Fatalf("карточка правки: %v %+v", err, ri)
	}
	if _, err := e.review.Claim(e.ctx, modA, domain.KindTaskRevision, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.review.Decide(e.ctx, modA, domain.KindTaskRevision, id, service.DecisionInput{Action: domain.ActionApprove}); err != nil {
		t.Fatal(err)
	}
	e.clock.add(11 * time.Second)
	if n, _ := e.review.ApplyDue(e.ctx); n != 1 {
		t.Fatal("правка не применена")
	}
	if pub, _ := e.list.PublishedTask(e.ctx, tk.ID); pub.Title != in.Title || pub.Revision != nil {
		t.Errorf("после одобрения правки: %+v", pub)
	}

	// жалобы: на своё — нельзя; лимит; «снять» — задание отклонено
	if _, err := e.compl.Create(e.ctx, biz, service.ComplaintInput{Subject: "task", ID: tk.ID, Reason: "spam"}); !errors.Is(err, service.ErrSelfAction) {
		t.Errorf("на своё: %v", err)
	}
	if created, err := e.compl.Create(e.ctx, modB, service.ComplaintInput{Subject: "task", ID: tk.ID, Reason: "fraud", Comment: "Просят оплату вперёд"}); err != nil || !created {
		t.Fatalf("жалоба: %v", err)
	}
	if created, _ := e.compl.Create(e.ctx, modB, service.ComplaintInput{Subject: "task", ID: tk.ID, Reason: "fraud"}); created {
		t.Error("повтор той же жалобы")
	}
	if _, err := e.compl.Create(e.ctx, modB, service.ComplaintInput{Subject: "task", ID: tk.ID, Reason: "other"}); err == nil {
		t.Error("«другое» без комментария")
	}
	if _, _, err := e.compl.Groups(e.ctx, biz, "", 0, 0); !errors.Is(err, service.ErrNotStaff) {
		t.Errorf("пользователь видит жалобы: %v", err)
	}
	gs, total, err := e.compl.Groups(e.ctx, modA, "", 0, 0)
	if err != nil || total != 1 || gs[0].Total != 1 || gs[0].TargetUserID != biz.ID {
		t.Fatalf("очередь жалоб: %v %+v", err, gs)
	}
	if c, err := e.compl.Case(e.ctx, modA, "task", tk.ID); err != nil || c.Task == nil || len(c.Complaints) != 1 {
		t.Fatalf("жалоба: %v %+v", err, c)
	}
	if n, err := e.compl.Resolve(e.ctx, modA, "task", tk.ID, domain.ComplaintHide, "Мошенничество подтвердилось"); err != nil || n != 1 {
		t.Fatalf("снять: %v %d", err, n)
	}
	if got, _ := e.list.Task(e.ctx, tk.ID); got.Status != domain.StatusRejected || got.RejectReason == "" {
		t.Errorf("снято с публикации: %+v", got)
	}

	// блокировка: модератор блокирует пользователя (не сотрудника), выход везде, скрытие из ленты
	if err := e.auth.CreateSession(e.ctx, biz.ID, tokenHash("biz-session"), e.clock.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.staff.Block(e.ctx, modA, modB.ID, "Нарушение правил"); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("модератор блокирует сотрудника: %v", err)
	}
	if _, err := e.staff.Block(e.ctx, modA, modA.ID, "Нарушение правил"); !errors.Is(err, service.ErrSelfAction) {
		t.Errorf("себя: %v", err)
	}
	if _, err := e.staff.Block(e.ctx, biz, modA.ID, "Нарушение правил"); !errors.Is(err, service.ErrNotStaff) {
		t.Errorf("пользователь блокирует: %v", err)
	}
	if _, err := e.staff.Block(e.ctx, modA, biz.ID, "x"); err == nil {
		t.Error("без причины")
	}
	bu, err := e.staff.Block(e.ctx, modA, biz.ID, "Мошенничество с оплатой")
	if err != nil || !bu.Blocked() {
		t.Fatalf("блок: %v %+v", err, bu)
	}
	if _, err := e.staff.Block(e.ctx, modA, biz.ID, "Мошенничество с оплатой"); !errors.Is(err, service.ErrWrongStatus) {
		t.Errorf("повторный блок: %v", err)
	}
	if _, err := e.auth.SessionUser(e.ctx, tokenHash("biz-session"), e.clock.Now()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("сессии заблокированного: %v", err)
	}
	if ts, _ := e.list.PublishedTasks(e.ctx, domain.FeedFilter{}, nil, 50); len(ts) != 0 {
		t.Errorf("лента без заблокированных: %d", len(ts))
	}
	if _, err := e.staff.Unblock(e.ctx, modA, biz.ID, "Ошибка, разобрались"); err != nil {
		t.Fatal(err)
	}

	// роли: модератор не назначает; администратор — да; последнего администратора не снять
	if _, err := e.staff.SetRole(e.ctx, modA, biz.ID, domain.RoleModerator); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("модератор назначает: %v", err)
	}
	if u, err := e.staff.SetRole(e.ctx, adm, biz.ID, domain.RoleModerator); err != nil || u.StaffRole != domain.RoleModerator {
		t.Fatalf("назначение: %v %+v", err, u)
	}
	if _, err := e.staff.SetRole(e.ctx, adm, adm.ID, domain.RoleNone); !errors.Is(err, service.ErrSelfAction) {
		t.Errorf("себе: %v", err)
	}
	other2 := e.user(t, "admin2@x.ru", domain.RoleAdmin)
	if _, err := e.staff.SetRole(e.ctx, other2, adm.ID, domain.RoleNone); err != nil {
		t.Fatal(err)
	}
	adm, _ = e.auth.UserByID(e.ctx, adm.ID)
	if _, err := e.staff.Block(e.ctx, adm, other2.ID, "Попытка снять последнего"); !errors.Is(err, service.ErrNotStaff) {
		t.Errorf("бывший администратор: %v", err)
	}
	if _, err := e.admin.SetStaffRole(e.ctx, other2.ID, domain.RoleNone, domain.AuditEntry{Action: "test"}); !errors.Is(err, repository.ErrLastAdmin) {
		t.Errorf("последний администратор: %v", err)
	}

	// журнал: решения, блок, роль — с поиском
	if es, err := e.admin.SearchAudit(e.ctx, domain.AuditFilter{Action: domain.AuditUserBlock, Limit: 10}); err != nil || len(es) != 1 || es[0].Reason == "" {
		t.Errorf("журнал блокировок: %v %+v", err, es)
	}
	if es, _ := e.admin.SearchAudit(e.ctx, domain.AuditFilter{Query: "review.", Limit: 100}); len(es) < 5 {
		t.Errorf("журнал решений: %d", len(es))
	}
	if _, err := service.NewAuditLog(e.admin).Search(e.ctx, modA, domain.AuditFilter{}); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("модератор читает журнал: %v", err)
	}

	// сводка
	v, err := service.NewDashboard(e.admin, e.clock).Get(e.ctx, modA)
	if err != nil || v.Stats.DecidedToday < 3 || v.Stats.UsersToday < 4 {
		t.Errorf("сводка: %v %+v", err, v)
	}
}

func TestAdminCategoriesAndFormats(t *testing.T) {
	e := newAdminEnv(t)
	e.user(t, "admin@x.ru", domain.RoleAdmin)
	mod := e.user(t, "mod@x.ru", domain.RoleModerator)
	biz := e.user(t, "biz@x.ru", domain.RoleNone)
	cs := service.NewCategories(e.cats)
	c1, _, err := cs.Create(e.ctx, biz, "Настолки")
	if err != nil {
		t.Fatal(err)
	}
	c2, _, err := cs.Create(e.ctx, biz, "Кофейни у дома")
	if err != nil {
		t.Fatal(err)
	}
	by := e.clock.Now().Add(72 * time.Hour).Format(time.DateOnly)
	tk, err := e.tasks.Create(e.ctx, biz, service.TaskInput{Title: "Сторис про игры", City: "Казань", Categories: []string{c2.Slug, "food"},
		Formats: []string{"Прямой эфир", "Карусель сторис"}, Access: domain.AccessOpen, MinViews: 100, RewardKopecks: 50000, PublishBy: by,
		Blocks: []domain.Block{{Kind: domain.BlockText, Items: []string{"Текст"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.tasks.Submit(e.ctx, biz, tk.ID); err != nil {
		t.Fatal(err)
	}
	p, err := e.review.Queue(e.ctx, mod, domain.QueueFilter{Kinds: []domain.ReviewKind{domain.KindCategory, domain.KindFormat}})
	if err != nil || p.Total != 4 {
		t.Fatalf("категории и форматы в очереди: %v %+v", err, p)
	}
	// одобрить; объединить с общей (в задании заменилась, без повторов); отклонить
	if err := e.review.DecideCategory(e.ctx, mod, c1.ID, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.review.DecideCategory(e.ctx, mod, c2.ID, "merge", "nope", ""); err == nil {
		t.Error("объединение с несуществующей")
	}
	if err := e.review.DecideCategory(e.ctx, mod, c2.ID, "merge", "food", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.list.Task(e.ctx, tk.ID); len(got.Categories) != 1 || got.Categories[0] != "food" {
		t.Errorf("после объединения: %v", got.Categories)
	}
	if err := e.review.DecideCategory(e.ctx, mod, c2.ID, "approve", "", ""); !errors.Is(err, service.ErrWrongStatus) {
		t.Errorf("повторное решение: %v", err)
	}
	it, err := e.review.Item(e.ctx, mod, domain.KindFormat, "прямой эфир")
	if err != nil || it.Format == nil || it.Format.Tasks != 1 || len(it.Formats) == 0 {
		t.Fatalf("формат: %v %+v", err, it)
	}
	if err := e.review.DecideFormat(e.ctx, mod, "прямой эфир", "merge", "Чужой"); err == nil {
		t.Error("объединение с форматом не из списка")
	}
	if err := e.review.DecideFormat(e.ctx, mod, "прямой эфир", "merge", "reels"); err != nil {
		t.Fatal(err)
	}
	if err := e.review.DecideFormat(e.ctx, mod, "карусель сторис", "reject", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := e.list.Task(e.ctx, tk.ID)
	if len(got.Formats) != 2 || got.Formats[0] != "reels" {
		t.Errorf("формат заменён: %v", got.Formats)
	}
	// отклонённый формат больше не принимается
	_, err = e.tasks.Create(e.ctx, biz, service.TaskInput{Title: "Ещё", Formats: []string{"Карусель сторис"}, Access: "open"})
	var ve *service.ValidationError
	if !errors.As(err, &ve) || ve.Fields["formats"] == "" {
		t.Errorf("отклонённый формат: %v", err)
	}
	if p, _ := e.review.Queue(e.ctx, mod, domain.QueueFilter{Kinds: []domain.ReviewKind{domain.KindCategory, domain.KindFormat}}); p.Total != 0 {
		t.Errorf("очередь пуста: %+v", p)
	}
}

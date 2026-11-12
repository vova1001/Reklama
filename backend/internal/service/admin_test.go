package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
)

// RFC 6238, приложение B (SHA1, ключ «12345678901234567890»): последние 6 цифр 8-значных кодов.
func TestTOTPVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, c := range []struct {
		unix int64
		want string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}} {
		if got := totpCode(secret, totpStep(time.Unix(c.unix, 0))); got != c.want {
			t.Errorf("%d: %s, want %s", c.unix, got, c.want)
		}
	}
	now := time.Unix(1234567890, 0)
	if st, ok := totpMatch(secret, totpCode(secret, totpStep(now)-1), now); !ok || st != totpStep(now)-1 {
		t.Error("код прошлого шага (±30 с) принимается")
	}
	if _, ok := totpMatch(secret, totpCode(secret, totpStep(now)-2), now); ok {
		t.Error("код двухшаговой давности — нет")
	}
	u := totpURI("Охват", "a@b.ru", secret)
	if !strings.HasPrefix(u, "otpauth://totp/") || !strings.Contains(u, "secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ") || !strings.Contains(u, "issuer=") {
		t.Errorf("ссылка: %s", u)
	}
	if normalizeOTP(" 12 34-56 ") != "123456" {
		t.Error("код с пробелами")
	}
}

// Права: модератор — проверка и жалобы, без админских действий; пользователь и заблокированный — ничего.
func TestAuthorizeMatrix(t *testing.T) {
	now := time.Now()
	users := map[string]domain.User{
		"user":      {ID: 1},
		"moderator": {ID: 2, StaffRole: domain.RoleModerator},
		"admin":     {ID: 3, StaffRole: domain.RoleAdmin},
		"blocked":   {ID: 4, StaffRole: domain.RoleAdmin, BlockedAt: &now},
	}
	adminOnly := []domain.Permission{domain.PermManageStaff, domain.PermSettings, domain.PermAuditView, domain.PermBlockStaff}
	shared := []domain.Permission{domain.PermReview, domain.PermComplaints, domain.PermUsersView, domain.PermUsersBlock, domain.PermDashboard, domain.PermViewPrivates}
	for _, p := range append(adminOnly, shared...) {
		if err := authorize(users["user"], p); !errors.Is(err, ErrNotStaff) {
			t.Errorf("пользователь %s: %v", p, err)
		}
		if err := authorize(users["blocked"], p); !errors.Is(err, ErrNotStaff) {
			t.Errorf("заблокированный %s: %v", p, err)
		}
		if err := authorize(users["admin"], p); err != nil {
			t.Errorf("администратор %s: %v", p, err)
		}
	}
	for _, p := range adminOnly {
		if err := authorize(users["moderator"], p); !errors.Is(err, ErrForbidden) {
			t.Errorf("модератор %s: %v", p, err)
		}
	}
	for _, p := range shared {
		if err := authorize(users["moderator"], p); err != nil {
			t.Errorf("модератор %s: %v", p, err)
		}
	}
	if len(domain.RoleAdmin.Permissions()) != len(adminOnly)+len(shared) {
		t.Error("права администратора")
	}
}

// Сервисы админки отказывают без прав ещё до хранилища (nil-репозитории не трогаются).
func TestAdminServicesDenyWithoutRights(t *testing.T) {
	ctx := context.Background()
	user := domain.User{ID: 1}
	mod := domain.User{ID: 2, StaffRole: domain.RoleModerator}
	staff := NewStaffUsers(nil, nil, nil, nil)
	if _, err := staff.SetRole(ctx, mod, 5, domain.RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Errorf("модератор назначает роль: %v", err)
	}
	if _, err := staff.Search(ctx, user, UserFilter{}); !errors.Is(err, ErrNotStaff) {
		t.Errorf("пользователь ищет: %v", err)
	}
	if _, err := NewAuditLog(nil).Search(ctx, mod, domain.AuditFilter{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("модератор и журнал: %v", err)
	}
	st := NewSettings(nil, domain.ModerationManual, nil, nil)
	if _, err := st.Get(ctx, mod); !errors.Is(err, ErrForbidden) {
		t.Errorf("модератор и настройки: %v", err)
	}
	if _, err := NewDashboard(nil, nil).Get(ctx, user); !errors.Is(err, ErrNotStaff) {
		t.Errorf("пользователь и сводка: %v", err)
	}
	r := NewReview(ReviewDeps{}, ReviewConfig{})
	if _, err := r.Queue(ctx, user, domain.QueueFilter{}); !errors.Is(err, ErrNotStaff) {
		t.Errorf("пользователь и очередь: %v", err)
	}
	if _, err := r.Decide(ctx, user, domain.KindTask, "1", DecisionInput{Action: domain.ActionApprove}); !errors.Is(err, ErrNotStaff) {
		t.Errorf("пользователь решает: %v", err)
	}
	c := NewComplaints(nil, nil, nil, nil, nil, ComplaintsConfig{})
	if _, err := c.Resolve(ctx, user, "task", 1, "dismiss", ""); !errors.Is(err, ErrNotStaff) {
		t.Errorf("пользователь решает жалобу: %v", err)
	}
	tf := NewTwoFactor(TwoFactorDeps{}, TwoFactorConfig{})
	if err := tf.Reset(ctx, mod, 9); !errors.Is(err, ErrForbidden) {
		t.Errorf("модератор сбрасывает 2FA: %v", err)
	}
	if _, err := tf.Setup(ctx, user); !errors.Is(err, ErrNotStaff) {
		t.Errorf("пользователь и 2FA: %v", err)
	}
	if err := tf.Verify(ctx, user, "t", "123456"); !errors.Is(err, ErrNotStaff) {
		t.Errorf("пользователь подтверждает 2FA: %v", err)
	}
}

func TestCheckDecision(t *testing.T) {
	cases := []struct {
		name string
		d    Decision
		subj domain.Subject
		ok   bool
	}{
		{"одобрить", Decision{Action: domain.ActionApprove, Reason: "лишнее"}, domain.SubjectTask, true},
		{"отказ без причины", Decision{Action: domain.ActionReject}, domain.SubjectTask, false},
		{"отказ", Decision{Action: domain.ActionReject, Reason: "  Запрещённый   товар "}, domain.SubjectTask, true},
		{"доработка без замечаний", Decision{Action: domain.ActionRework, Notes: map[string]string{"title": " "}}, domain.SubjectTask, false},
		{"доработка", Decision{Action: domain.ActionRework, Notes: map[string]string{"title": "Уточните", "general": "Фото"}}, domain.SubjectTask, true},
		{"поле анкеты у задания", Decision{Action: domain.ActionRework, Notes: map[string]string{"works": "Добавьте"}}, domain.SubjectTask, false},
		{"поле анкеты", Decision{Action: domain.ActionRework, Notes: map[string]string{"works": "Добавьте"}}, domain.SubjectCard, true},
		{"короткое замечание", Decision{Action: domain.ActionRework, Notes: map[string]string{"title": "ок"}}, domain.SubjectTask, false},
		{"неизвестное", Decision{Action: "ban"}, domain.SubjectTask, false},
	}
	for _, c := range cases {
		d := c.d
		err := checkDecision(&d, c.subj)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
		if c.ok && d.action() == domain.ActionReject && d.Reason != "Запрещённый товар" {
			t.Errorf("%s: причина %q", c.name, d.Reason)
		}
		if c.ok && d.action() == domain.ActionRework && d.Reason == "" {
			t.Errorf("%s: сводка замечаний", c.name)
		}
		if c.ok && d.action() == domain.ActionApprove && d.Reason != "" {
			t.Errorf("%s: у одобрения нет причины", c.name)
		}
	}
	if !domain.RevisionTransition(domain.StatusRejected, domain.StatusPending, domain.ActorOwner) ||
		domain.RevisionTransition(domain.StatusPending, domain.StatusPublished, domain.ActorOwner) ||
		!domain.RevisionTransition(domain.StatusPending, domain.StatusChangesRequested, domain.ActorModerator) {
		t.Error("переходы правки")
	}
	if !domain.CanTransition(domain.StatusPublished, domain.StatusRejected, domain.ActorModerator) ||
		domain.CanTransition(domain.StatusPublished, domain.StatusRejected, domain.ActorOwner) {
		t.Error("снять по жалобе — только модератор")
	}
}

type fakeStaffAccess struct {
	granted []int64
	events  []string
}

func (f *fakeStaffAccess) AddLoginEvent(_ context.Context, uid int64, method, ip, _ string) error {
	f.events = append(f.events, method+":"+ip)
	return nil
}
func (f *fakeStaffAccess) DeleteLoginEventsBefore(context.Context, time.Time) (int64, error) { return 0, nil }
func (f *fakeStaffAccess) GrantBootstrapAdmin(_ context.Context, uid int64, e domain.AuditEntry) (domain.User, error) {
	if e.Action != domain.AuditRoleBootstrap {
		return domain.User{}, errors.New("действие журнала")
	}
	f.granted = append(f.granted, uid)
	return domain.User{ID: uid, Email: "boss@x.ru", StaffRole: domain.RoleAdmin}, nil
}

func TestBootstrapAdminAndBlockedLogin(t *testing.T) {
	staff := &fakeStaffAccess{}
	e := newEnv(t, func(d *AuthDeps, c *AuthConfig) {
		d.Staff = staff
		c.AdminEmails = ParseAdminEmails(" Boss@X.ru , ,bad, other@x.ru")
	})
	ctx := WithClient(context.Background(), ClientInfo{IP: "203.0.113.5", UserAgent: "Firefox/120"})
	res, err := e.a.login(ctx, domain.Identity{Provider: "email", Subject: "boss@x.ru", Email: "boss@x.ru"}, true, domain.NewUser{})
	if err != nil || res.User.StaffRole != domain.RoleAdmin || len(staff.granted) != 1 {
		t.Fatalf("первый администратор: %v %+v %v", err, res.User, staff.granted)
	}
	if len(staff.events) != 1 || staff.events[0] != "email:203.0.113.5" {
		t.Errorf("журнал входов: %v", staff.events)
	}
	if _, err := e.a.login(ctx, domain.Identity{Provider: "email", Subject: "nobody@x.ru", Email: "nobody@x.ru"}, true, domain.NewUser{}); err != nil || len(staff.granted) != 1 {
		t.Errorf("почта не из списка: %v %v", err, staff.granted)
	}
	// заблокированный не входит
	now := t0
	e.repo.mu.Lock()
	for _, u := range e.repo.users {
		if u.Email == "nobody@x.ru" {
			u.BlockedAt = &now
		}
	}
	e.repo.mu.Unlock()
	if _, err := e.a.login(ctx, domain.Identity{Provider: "email", Subject: "nobody@x.ru", Email: "nobody@x.ru"}, true, domain.NewUser{}); !errors.Is(err, ErrBlocked) {
		t.Errorf("заблокированный: %v", err)
	}
}

func TestModerationNotices(t *testing.T) {
	notes := OrderedNotes(map[string]string{"photos": "Светлее", "title": "Короче", "general": "Спасибо"})
	if len(notes) != 3 || notes[0].Field != "Общее" || notes[1].Field != "Название" || notes[2].Field != "Фото" {
		t.Errorf("порядок замечаний: %+v", notes)
	}
	tx := newBotTexts("https://ohvat.ru")
	m, ok := tx.notification(7, 1, Notification{Kind: NotifyModeration, Subject: "task", Status: "rework", Title: "Обзор <b>", Path: "/app/tasks/1", Notes: notes})
	if !ok || !strings.Contains(m.Text, "вернули на доработку") || !strings.Contains(m.Text, "Обзор &lt;b&gt;") || !strings.Contains(m.Text, "Название: Короче") {
		t.Errorf("доработка: %v %q", ok, m.Text)
	}
	m, ok = tx.notification(7, 1, Notification{Kind: NotifyModeration, Subject: "card_revision", Status: "reject", Reason: "Накрутка"})
	if !ok || !strings.Contains(m.Text, "Изменения анкеты не прошли проверку") || !strings.Contains(m.Text, "Причина: Накрутка") {
		t.Errorf("отказ: %q", m.Text)
	}
	if _, ok := tx.notification(7, 1, Notification{Kind: NotifyModeration, Subject: "task", Status: "bogus"}); ok {
		t.Error("неизвестное решение")
	}
	bot := &recNotifier{}
	mailer := &recDecisionMail{}
	on := &OwnerNotices{Users: userByID{domain.User{ID: 5, Email: "o@x.ru"}}, Mail: mailer, Bot: bot, SiteURL: "https://ohvat.ru"}
	if err := on.NotifyDecision(context.Background(), 5, DecisionNotice{Kind: domain.KindTask, Action: domain.ActionApprove, Title: "T", Path: "/app/tasks/9"}); err != nil {
		t.Fatal(err)
	}
	if mailer.to != "o@x.ru" || mailer.m.URL != "https://ohvat.ru/app/tasks/9" || bot.n.Kind != NotifyModeration || bot.n.Status != "approve" {
		t.Errorf("уведомления: %+v %+v", mailer, bot.n)
	}
}

type recNotifier struct{ n Notification }

func (r *recNotifier) Notify(_ context.Context, _ int64, n Notification) error {
	r.n = n
	return nil
}

type recDecisionMail struct {
	to string
	m  DecisionMail
}

func (r *recDecisionMail) SendDecision(_ context.Context, to string, m DecisionMail) error {
	r.to, r.m = to, m
	return nil
}

type userByID struct{ u domain.User }

func (u userByID) UserByID(context.Context, int64) (domain.User, error) { return u.u, nil }

type memSettings struct {
	v     string
	calls int
}

func (m *memSettings) Setting(context.Context, string) (string, bool, error) {
	m.calls++
	return m.v, m.v != "", nil
}
func (m *memSettings) SetSetting(_ context.Context, _, v string, _ int64, e domain.AuditEntry) error {
	if e.Action != domain.AuditSetting {
		return errors.New("журнал")
	}
	m.v = v
	return nil
}

func TestSettingsModeCache(t *testing.T) {
	ctx := context.Background()
	clk := &fakeClock{t: t0}
	repo := &memSettings{}
	s := NewSettings(repo, domain.ModerationAuto, clk, nil)
	if s.ModerationMode(ctx) != domain.ModerationAuto {
		t.Error("по умолчанию — из окружения")
	}
	admin := domain.User{ID: 1, StaffRole: domain.RoleAdmin}
	if _, err := s.SetModerationMode(ctx, admin, "strict"); err == nil {
		t.Error("неизвестный режим")
	}
	if st, err := s.SetModerationMode(ctx, admin, domain.ModerationManual); err != nil || st.ModerationMode != domain.ModerationManual {
		t.Fatal(err)
	}
	calls := repo.calls
	for i := 0; i < 5; i++ {
		_ = s.ModerationMode(ctx)
	}
	if repo.calls != calls || s.ModerationMode(ctx) != domain.ModerationManual {
		t.Error("кэш 5 с")
	}
	repo.v = string(domain.ModerationAuto) // поменяли на другом сервере
	clk.mu.Lock()
	clk.t = clk.t.Add(6 * time.Second)
	clk.mu.Unlock()
	if s.ModerationMode(ctx) != domain.ModerationAuto {
		t.Error("после 5 с — из базы")
	}
	if st, err := s.Get(ctx, admin); err != nil || st.ModerationDefault != domain.ModerationAuto {
		t.Errorf("%v %+v", err, st)
	}
}

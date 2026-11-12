package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// fakeTwoFA — 2FA: req — что вернуть на проверку доступа (nil — подтверждена).
type fakeTwoFA struct{ req error }

func (f *fakeTwoFA) Status(_ context.Context, u domain.User, _ string) (service.TwoFactorStatus, error) {
	if !u.Staff() {
		return service.TwoFactorStatus{}, service.ErrNotStaff
	}
	return service.TwoFactorStatus{Enabled: f.req != service.ErrTwoFactorSetup, Verified: f.req == nil}, nil
}
func (f *fakeTwoFA) Require(context.Context, domain.User, string) error { return f.req }
func (f *fakeTwoFA) Setup(context.Context, domain.User) (service.TwoFactorSetup, error) {
	return service.TwoFactorSetup{Secret: "ABC", URI: "otpauth://totp/x", QR: []byte{0x89, 'P', 'N', 'G'}}, nil
}
func (f *fakeTwoFA) Confirm(_ context.Context, _ domain.User, _, code string) ([]string, error) {
	if code != "123456" {
		return nil, service.ErrTwoFactorInvalid
	}
	return []string{"abcde-fghij"}, nil
}
func (f *fakeTwoFA) Verify(_ context.Context, _ domain.User, _, code string) error {
	if code != "123456" {
		return &service.LimitError{Reason: "2fa_locked", RetryAfter: time.Minute}
	}
	return nil
}
func (f *fakeTwoFA) RegenerateBackup(context.Context, domain.User) ([]string, error) {
	return []string{"x"}, nil
}
func (f *fakeTwoFA) Reset(_ context.Context, actor domain.User, _ int64) error {
	return service.Authorize(actor, domain.PermManageStaff)
}

// fakeReview — проверка: права — как у настоящего сервиса.
type fakeReview struct{ last map[string]any }

func (f *fakeReview) Queue(_ context.Context, u domain.User, fl domain.QueueFilter) (service.QueuePage, error) {
	f.last["filter"] = fl
	if err := service.Authorize(u, domain.PermReview); err != nil {
		return service.QueuePage{}, err
	}
	return service.QueuePage{Items: []domain.QueueItem{{Kind: domain.KindTask, ID: "7", Title: "Обзор", WaitingSince: day}}, Total: 1, Oldest: &day}, nil
}
func (f *fakeReview) Claim(_ context.Context, u domain.User, k domain.ReviewKind, id string) (domain.Lease, error) {
	if id == "held" {
		return domain.Lease{}, service.ErrLeaseHeld
	}
	return domain.Lease{Kind: k, ItemID: id, ModeratorID: u.ID, ExpiresAt: day}, service.Authorize(u, domain.PermReview)
}
func (f *fakeReview) Release(context.Context, domain.User, domain.ReviewKind, string) error {
	return nil
}
func (f *fakeReview) Next(_ context.Context, _ domain.User, _ domain.QueueFilter, _ domain.ReviewKind, skip string) (domain.QueueItem, error) {
	if skip == "last" {
		return domain.QueueItem{}, domain.ErrNotFound
	}
	return domain.QueueItem{Kind: domain.KindCard, ID: "8", WaitingSince: day}, nil
}
func (f *fakeReview) Item(_ context.Context, u domain.User, k domain.ReviewKind, id string) (service.ReviewItem, error) {
	t := sampleTask(7, domain.StatusPending)
	return service.ReviewItem{Kind: k, ID: id, InQueue: true, Task: &t, Lease: &domain.Lease{ModeratorID: u.ID, ExpiresAt: day},
		Owner: &domain.UserSummary{User: domain.User{ID: 3, Name: "Кофейня"}}, History: []domain.StatusChange{{From: "draft", To: "pending_review"}},
		Decisions: []domain.ReviewDecision{{ID: 1, Action: domain.ActionReject}}, Complaints: []domain.Complaint{{ID: 2, Reason: "spam"}}}, nil
}
func (f *fakeReview) Decide(_ context.Context, u domain.User, k domain.ReviewKind, id string, in service.DecisionInput) (domain.ReviewDecision, error) {
	f.last["decision"] = in
	if in.Action == domain.ActionReject && len(in.Reason) < 5 {
		return domain.ReviewDecision{}, &service.ValidationError{Fields: map[string]string{"reason": "Причина"}}
	}
	return domain.ReviewDecision{ID: 11, Kind: k, ItemID: 7, Action: in.Action, State: domain.DecisionPending, ApplyAt: day}, nil
}
func (f *fakeReview) Undo(_ context.Context, _ domain.User, id int64) (domain.ReviewDecision, error) {
	if id == 12 {
		return domain.ReviewDecision{}, service.ErrUndoExpired
	}
	return domain.ReviewDecision{ID: id, State: domain.DecisionUndone}, nil
}
func (f *fakeReview) DecideCategory(_ context.Context, _ domain.User, id int64, action, into, _ string) error {
	f.last["category"] = action + ":" + into
	return nil
}
func (f *fakeReview) DecideFormat(_ context.Context, _ domain.User, norm, action, into string) error {
	f.last["format"] = norm + ":" + action + ":" + into
	return nil
}
func (f *fakeReview) UndoWindow() time.Duration { return 10 * time.Second }

type fakeComplaints struct{ last map[string]any }

func (f *fakeComplaints) Create(_ context.Context, u domain.User, in service.ComplaintInput) (bool, error) {
	f.last["complaint"] = in
	return in.Reason != "dup", nil
}
func (f *fakeComplaints) Groups(_ context.Context, u domain.User, _ string, _, _ int) ([]domain.ComplaintGroup, int, error) {
	return []domain.ComplaintGroup{{Subject: "task", SubjectID: 7, Total: 2, Reasons: []string{"spam"}, FirstAt: day}}, 1, service.Authorize(u, domain.PermComplaints)
}
func (f *fakeComplaints) Case(_ context.Context, _ domain.User, subject string, id int64) (service.ComplaintCase, error) {
	t := sampleTask(id, domain.StatusPublished)
	return service.ComplaintCase{Subject: subject, ID: id, Task: &t, Complaints: []domain.Complaint{{ID: 1}},
		Target: &domain.UserSummary{User: domain.User{ID: 3}}}, nil
}
func (f *fakeComplaints) Resolve(_ context.Context, _ domain.User, subject string, id int64, action, note string) (int, error) {
	f.last["resolve"] = subject + ":" + action + ":" + note
	return 2, nil
}

type fakeStaffUsers struct{}

func (fakeStaffUsers) Search(_ context.Context, u domain.User, f service.UserFilter) ([]domain.UserSummary, error) {
	return []domain.UserSummary{{User: domain.User{ID: 4, Email: "a@b.ru"}, BusinessName: "Кофейня"}}, service.Authorize(u, domain.PermUsersView)
}
func (fakeStaffUsers) Card(_ context.Context, u domain.User, id int64) (service.UserCard, error) {
	n := int64(1500)
	return service.UserCard{User: domain.UserSummary{User: domain.User{ID: id}}, Providers: []string{"email"},
		Profiles: domain.Profiles{Business: &domain.BusinessProfile{Name: "Кофейня"}},
		Socials:  []domain.SocialAccount{{Platform: "telegram", Kind: "channel", Status: "verified", Stats: domain.SocialStats{Followers: &n}}},
		Logins:   []domain.LoginEvent{{Method: "email", IP: "1.2.3.4", UserAgent: "Firefox/1 Windows", At: day}}}, nil
}
func (fakeStaffUsers) Block(_ context.Context, u domain.User, id int64, reason string) (domain.User, error) {
	now := day
	return domain.User{ID: id, BlockedAt: &now, BlockedReason: reason}, service.Authorize(u, domain.PermUsersBlock)
}
func (fakeStaffUsers) Unblock(_ context.Context, u domain.User, id int64, _ string) (domain.User, error) {
	return domain.User{ID: id}, nil
}
func (fakeStaffUsers) SetRole(_ context.Context, u domain.User, id int64, role domain.StaffRole) (domain.User, error) {
	if err := service.Authorize(u, domain.PermManageStaff); err != nil {
		return domain.User{}, err
	}
	return domain.User{ID: id, StaffRole: role}, nil
}

type fakeAuditRepo struct{}

func (fakeAuditRepo) SearchAudit(context.Context, domain.AuditFilter) ([]domain.AuditEntry, error) {
	return []domain.AuditEntry{{ID: 9, Action: domain.AuditUserBlock, AfterJSON: []byte(`{"blocked":true}`), At: day}}, nil
}

type fakeSettingsRepo struct{ v string }

func (f *fakeSettingsRepo) Setting(context.Context, string) (string, bool, error) {
	return f.v, f.v != "", nil
}
func (f *fakeSettingsRepo) SetSetting(_ context.Context, _, v string, _ int64, _ domain.AuditEntry) error {
	f.v = v
	return nil
}

type fakeDash struct{}

func (fakeDash) Get(_ context.Context, u domain.User) (service.DashboardView, error) {
	return service.DashboardView{Queue: []domain.QueueCount{{Kind: domain.KindTask, Total: 3, Oldest: &day}},
		Stats: domain.DashboardStats{DecidedToday: 2, AvgReviewSeconds: 90.5}}, service.Authorize(u, domain.PermDashboard)
}

type adminTest struct {
	srv  *Server
	auth *fakeAuth
	tf   *fakeTwoFA
	rev  *fakeReview
	comp *fakeComplaints
	f    *fakeListings
}

func newAdminTest(role domain.StaffRole, req error) adminTest {
	a := adminTest{auth: &fakeAuth{user: domain.User{ID: 3, StaffRole: role, Email: "s@x.ru"}}, tf: &fakeTwoFA{req: req},
		rev: &fakeReview{last: map[string]any{}}, comp: &fakeComplaints{last: map[string]any{}}, f: &fakeListings{}}
	a.srv = New(Services{
		Home: fakeHome{}, Health: fakeDB{}, Auth: a.auth, Profiles: &fakeProfiles{}, Tasks: a.f, Cards: fakeCards{a.f},
		Categories: fakeCats{a.f}, Formats: fakeFormats{a.f}, Media: fakeMedia{a.f},
		TwoFactor: a.tf, Review: a.rev, Complaints: a.comp, StaffUsers: fakeStaffUsers{}, Audit: service.NewAuditLog(fakeAuditRepo{}),
		Settings: service.NewSettings(&fakeSettingsRepo{}, domain.ModerationAuto, nil, nil), Dashboard: fakeDash{},
	}, Options{Origin: testOrigin, SecureCookies: true})
	return a
}

// Матрица доступа: пользователь — ничего (404), модератор без 2FA — только /me и настройка 2FA, модератор —
// без админских действий (403), администратор — всё.
func TestAdminAccessMatrix(t *testing.T) {
	cases := []struct {
		name   string
		role   domain.StaffRole
		req    error
		method string
		path   string
		body   string
		code   int
		errc   string
	}{
		{"пользователь: me", domain.RoleNone, nil, "GET", "/api/admin/me", "", 404, "not_found"},
		{"пользователь: очередь", domain.RoleNone, nil, "GET", "/api/admin/queue", "", 404, "not_found"},
		{"пользователь: 2FA", domain.RoleNone, nil, "POST", "/api/admin/2fa/setup", "", 404, "not_found"},
		{"модератор без 2FA: me", domain.RoleModerator, service.ErrTwoFactorSetup, "GET", "/api/admin/me", "", 200, ""},
		{"модератор без 2FA: настройка", domain.RoleModerator, service.ErrTwoFactorSetup, "POST", "/api/admin/2fa/setup", "", 200, ""},
		{"модератор без 2FA: очередь", domain.RoleModerator, service.ErrTwoFactorSetup, "GET", "/api/admin/queue", "", 403, "2fa_setup_required"},
		{"модератор не подтвердил", domain.RoleModerator, service.ErrTwoFactorRequired, "GET", "/api/admin/dashboard", "", 403, "2fa_required"},
		{"модератор: очередь", domain.RoleModerator, nil, "GET", "/api/admin/queue?type=task&category=food&sort=newest&from=2026-11-01", "", 200, ""},
		{"модератор: сводка", domain.RoleModerator, nil, "GET", "/api/admin/dashboard", "", 200, ""},
		{"модератор: жалобы", domain.RoleModerator, nil, "GET", "/api/admin/complaints", "", 200, ""},
		{"модератор: блок", domain.RoleModerator, nil, "POST", "/api/admin/users/5/block", `{"reason":"Спам в заданиях"}`, 200, ""},
		{"модератор: роль", domain.RoleModerator, nil, "PUT", "/api/admin/users/5/role", `{"role":"admin"}`, 403, "forbidden"},
		{"модератор: сброс 2FA", domain.RoleModerator, nil, "POST", "/api/admin/users/5/reset-2fa", "", 403, "forbidden"},
		{"модератор: журнал", domain.RoleModerator, nil, "GET", "/api/admin/audit", "", 403, "forbidden"},
		{"модератор: настройки", domain.RoleModerator, nil, "GET", "/api/admin/settings", "", 403, "forbidden"},
		{"модератор: режим", domain.RoleModerator, nil, "PUT", "/api/admin/settings", `{"moderationMode":"manual"}`, 403, "forbidden"},
		{"администратор: роль", domain.RoleAdmin, nil, "PUT", "/api/admin/users/5/role", `{"role":"moderator"}`, 200, ""},
		{"администратор: сброс 2FA", domain.RoleAdmin, nil, "POST", "/api/admin/users/5/reset-2fa", "", 204, ""},
		{"администратор: журнал", domain.RoleAdmin, nil, "GET", "/api/admin/audit?q=user&from=2026-11-01&to=2026-11-30&actor=3", "", 200, ""},
		{"администратор: режим", domain.RoleAdmin, nil, "PUT", "/api/admin/settings", `{"moderationMode":"manual"}`, 200, ""},
		{"администратор: неверный режим", domain.RoleAdmin, nil, "PUT", "/api/admin/settings", `{"moderationMode":"x"}`, 400, "validation"},
	}
	for _, c := range cases {
		a := newAdminTest(c.role, c.req)
		rec := serve(a.srv, meReq(c.method, c.path, c.body))
		if rec.Code != c.code {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
			continue
		}
		if c.errc != "" && !strings.Contains(rec.Body.String(), `"code":"`+c.errc+`"`) {
			t.Errorf("%s: %s", c.name, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: no-store", c.name)
		}
		if c.role != domain.RoleNone && rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s: X-Frame-Options", c.name)
		}
	}
}

func TestAdminEndpoints(t *testing.T) {
	a := newAdminTest(domain.RoleModerator, nil)
	// без сессии — 401; без CSRF — 403
	if rec := serve(a.srv, httptest.NewRequest("GET", "/api/admin/me", nil)); rec.Code != 401 {
		t.Errorf("без входа: %d", rec.Code)
	}
	r := httptest.NewRequest("POST", "/api/admin/review/task/7/claim", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "good"})
	if rec := serve(a.srv, r); rec.Code != 403 || !strings.Contains(rec.Body.String(), "csrf") {
		t.Errorf("без CSRF: %d %s", rec.Code, rec.Body.String())
	}
	rec := serve(a.srv, meReq("GET", "/api/admin/me", ""))
	var me adminMeDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.Role != "moderator" || len(me.Permissions) == 0 || len(me.RejectTemplates) == 0 || me.UndoSeconds != 10 || me.ReviewFields["title"] == "" {
		t.Errorf("me: %s", rec.Body.String())
	}
	steps := []struct {
		method, path, body string
		code               int
		want               string
	}{
		{"POST", "/api/admin/2fa/setup", "", 200, `"qr":"data:image/png;base64,`},
		{"POST", "/api/admin/2fa/confirm", `{"code":"000000"}`, 400, `"2fa_invalid"`},
		{"POST", "/api/admin/2fa/confirm", `{"code":"123456"}`, 200, `"backupCodes":["abcde-fghij"]`},
		{"POST", "/api/admin/2fa/verify", `{"code":"1"}`, 429, `"rate_limited"`},
		{"POST", "/api/admin/2fa/verify", `{"code":"123456"}`, 204, ""},
		{"POST", "/api/admin/2fa/backup-codes", "", 200, `"backupCodes"`},
		{"GET", "/api/admin/queue", "", 200, `"total":1`},
		{"POST", "/api/admin/queue/next", `{"type":["task"],"from":"2026-11-01","to":"2026-11-02","skipType":"task","skipId":"7"}`, 200, `"type":"card"`},
		{"POST", "/api/admin/queue/next", `{"skipId":"last"}`, 204, ""},
		{"GET", "/api/admin/review/task/7", "", 200, `"lease":{"moderatorId":3,"mine":true`},
		{"POST", "/api/admin/review/task/7/claim", "", 200, `"mine":true`},
		{"POST", "/api/admin/review/task/held/claim", "", 409, `"lease_held"`},
		{"POST", "/api/admin/review/task/7/release", "", 204, ""},
		{"POST", "/api/admin/review/task/7/decision", `{"action":"reject","reason":"x"}`, 400, `"validation"`},
		{"POST", "/api/admin/review/task/7/decision", `{"action":"rework","notes":{"title":"Уточните"}}`, 202, `"state":"pending"`},
		{"POST", "/api/admin/decisions/11/undo", "", 200, `"undone"`},
		{"POST", "/api/admin/decisions/12/undo", "", 409, `"undo_expired"`},
		{"POST", "/api/admin/categories/5/decision", `{"action":"merge","into":"food"}`, 204, ""},
		{"POST", "/api/admin/formats/decision", `{"norm":"прямой эфир","action":"merge","into":"reels"}`, 204, ""},
		{"GET", "/api/admin/complaints", "", 200, `"total":1`},
		{"GET", "/api/admin/complaints/task/7", "", 200, `"complaints"`},
		{"GET", "/api/admin/complaints/bogus/7", "", 404, ""},
		{"POST", "/api/admin/complaints/task/7/resolve", `{"action":"hide","note":"Мошенничество"}`, 200, `"closed":2`},
		{"GET", "/api/admin/users?q=a&staff=1", "", 200, `"businessName":"Кофейня"`},
		{"GET", "/api/admin/users/4", "", 200, `"followers":1500`},
		{"POST", "/api/admin/users/4/unblock", `{"reason":"Разобрались"}`, 200, `"blocked":false`},
		{"POST", "/api/complaints", `{"subject":"task","id":7,"reason":"spam","comment":"реклама"}`, 201, `"created":true`},
		{"POST", "/api/complaints", `{"subject":"task","id":7,"reason":"dup"}`, 200, `"created":false`},
		{"DELETE", "/api/me/tasks/7/revision", "", 200, `"id":7`},
		{"DELETE", "/api/me/card/revision", "", 200, `"card"`},
	}
	for _, s := range steps {
		rec := serve(a.srv, meReq(s.method, s.path, s.body))
		if rec.Code != s.code || (s.want != "" && !strings.Contains(rec.Body.String(), s.want)) {
			t.Errorf("%s %s: %d %s", s.method, s.path, rec.Code, rec.Body.String())
		}
	}
	if d := a.rev.last["decision"].(service.DecisionInput); d.Action != domain.ActionRework || d.Notes["title"] != "Уточните" {
		t.Errorf("решение: %+v", d)
	}
	if a.rev.last["format"] != "прямой эфир:merge:reels" || a.comp.last["resolve"] != "task:hide:Мошенничество" {
		t.Errorf("в сервис: %+v %+v", a.rev.last, a.comp.last)
	}
	if fl := a.rev.last["filter"].(domain.QueueFilter); len(fl.Kinds) != 0 {
		t.Errorf("фильтр без отбора: %+v", fl)
	}
	// слишком много замечаний
	notes := map[string]string{}
	for i := 0; i < 25; i++ {
		notes[strings.Repeat("f", i+1)] = "x"
	}
	b, _ := json.Marshal(map[string]any{"action": "rework", "notes": notes})
	if rec := serve(a.srv, meReq("POST", "/api/admin/review/task/7/decision", string(b))); rec.Code != 400 {
		t.Errorf("25 замечаний: %d", rec.Code)
	}
}

// Фото на проверке: сотрудник с подтверждённой 2FA видит закрытое фото, без 2FA и обычный пользователь — нет.
func TestReviewMedia(t *testing.T) {
	for _, c := range []struct {
		role domain.StaffRole
		req  error
		code int
	}{{domain.RoleModerator, nil, 200}, {domain.RoleModerator, service.ErrTwoFactorRequired, 404}, {domain.RoleNone, nil, 404}} {
		a := newAdminTest(c.role, c.req)
		r := httptest.NewRequest("GET", "/media/missing000000000000000/800.webp", nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "good"})
		rec := serve(a.srv, r)
		if rec.Code != c.code {
			t.Errorf("%s %v: %d", c.role, c.req, rec.Code)
		}
		if c.code == 200 && !strings.HasPrefix(rec.Header().Get("Cache-Control"), "private") {
			t.Errorf("закрытое фото кэшируется публично: %s", rec.Header().Get("Cache-Control"))
		}
	}
}

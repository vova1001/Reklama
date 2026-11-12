package httpapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/service"
)

// ===== админка: /api/admin/* =====
// Доступ: вошедший сотрудник (иначе 404 — админки «нет») → 2FA подтверждена в этой сессии не раньше 12 часов
// назад (кроме /me и /2fa/*) → право на действие проверяет сервис. Все ответы — no-store, изменяющие запросы —
// с CSRF (группа /api), лимит по пользователю, 2FA — строже.

// TwoFactorService — 2FA сотрудников.
type TwoFactorService interface {
	Status(ctx context.Context, u domain.User, token string) (service.TwoFactorStatus, error)
	Require(ctx context.Context, u domain.User, token string) error
	Setup(ctx context.Context, u domain.User) (service.TwoFactorSetup, error)
	Confirm(ctx context.Context, u domain.User, token, code string) ([]string, error)
	Verify(ctx context.Context, u domain.User, token, code string) error
	RegenerateBackup(ctx context.Context, u domain.User) ([]string, error)
	Reset(ctx context.Context, actor domain.User, targetID int64) error
}

// ReviewService — очередь проверки и решения.
type ReviewService interface {
	Queue(ctx context.Context, u domain.User, f domain.QueueFilter) (service.QueuePage, error)
	Claim(ctx context.Context, u domain.User, kind domain.ReviewKind, id string) (domain.Lease, error)
	Release(ctx context.Context, u domain.User, kind domain.ReviewKind, id string) error
	Next(ctx context.Context, u domain.User, f domain.QueueFilter, skipKind domain.ReviewKind, skipID string) (domain.QueueItem, error)
	Item(ctx context.Context, u domain.User, kind domain.ReviewKind, id string) (service.ReviewItem, error)
	Decide(ctx context.Context, u domain.User, kind domain.ReviewKind, id string, in service.DecisionInput) (domain.ReviewDecision, error)
	Undo(ctx context.Context, u domain.User, decisionID int64) (domain.ReviewDecision, error)
	DecideCategory(ctx context.Context, u domain.User, id int64, action, into, reason string) error
	DecideFormat(ctx context.Context, u domain.User, norm, action, into string) error
	UndoWindow() time.Duration
}

// ComplaintService — жалобы.
type ComplaintService interface {
	Create(ctx context.Context, u domain.User, in service.ComplaintInput) (bool, error)
	Groups(ctx context.Context, u domain.User, subject string, limit, offset int) ([]domain.ComplaintGroup, int, error)
	Case(ctx context.Context, u domain.User, subject string, id int64) (service.ComplaintCase, error)
	Resolve(ctx context.Context, u domain.User, subject string, id int64, action, note string) (int, error)
}

// StaffUserService — пользователи в админке.
type StaffUserService interface {
	Search(ctx context.Context, u domain.User, f service.UserFilter) ([]domain.UserSummary, error)
	Card(ctx context.Context, u domain.User, id int64) (service.UserCard, error)
	Block(ctx context.Context, u domain.User, id int64, reason string) (domain.User, error)
	Unblock(ctx context.Context, u domain.User, id int64, reason string) (domain.User, error)
	SetRole(ctx context.Context, u domain.User, id int64, role domain.StaffRole) (domain.User, error)
}

// AuditService — журнал действий.
type AuditService interface {
	Search(ctx context.Context, u domain.User, f domain.AuditFilter) ([]domain.AuditEntry, error)
}

// SettingsService — настройки админки.
type SettingsService interface {
	Get(ctx context.Context, u domain.User) (service.AdminSettings, error)
	SetModerationMode(ctx context.Context, u domain.User, mode domain.ModerationMode) (service.AdminSettings, error)
}

// DashboardService — сводка.
type DashboardService interface {
	Get(ctx context.Context, u domain.User) (service.DashboardView, error)
}

// adminHeaders — страницы и ответы админки не встраиваются в чужие сайты и не индексируются.
func adminHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// requireStaff — только сотрудники; остальным админки «нет» (404, без подсказки, что она есть).
func (s *Server) requireStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if !u.Staff() || u.Blocked() {
			writeError(w, http.StatusNotFound, "not_found", "Такого адреса нет.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireMFA — 2FA настроена и подтверждена в этой сессии не раньше 12 часов назад.
func (s *Server) requireMFA(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.twoFactor.Require(r.Context(), currentUser(r), sessionToken(r)); err != nil {
			s.fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sessionToken(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// adminRoutes — маршруты /api/admin (вызывается внутри группы /api: CSRF и лимит по адресу уже есть).
func (s *Server) adminRoutes(r chi.Router) {
	r.Use(noStore, adminHeaders, s.requireUser, s.requireStaff, limit(s.opt.AdminLimiter, byUser("admin:")))
	r.Get("/me", s.getAdminMe)
	r.Group(func(r chi.Router) {
		r.Use(limit(s.opt.MFALimiter, byUser("mfa:")))
		r.Post("/2fa/setup", s.postTwoFactorSetup)
		r.Post("/2fa/confirm", s.postTwoFactorConfirm)
		r.Post("/2fa/verify", s.postTwoFactorVerify)
	})
	r.Group(func(r chi.Router) {
		r.Use(s.requireMFA)
		r.Post("/2fa/backup-codes", s.postBackupCodes)
		r.Get("/dashboard", s.getDashboard)
		r.Get("/queue", s.getQueue)
		r.Post("/queue/next", s.postQueueNext)
		r.Get("/review/{kind}/{id}", s.getReviewItem)
		r.Post("/review/{kind}/{id}/claim", s.postClaim)
		r.Post("/review/{kind}/{id}/release", s.postRelease)
		r.Post("/review/{kind}/{id}/decision", s.postDecision)
		r.Post("/decisions/{id}/undo", s.postUndo)
		r.Post("/categories/{id}/decision", s.postCategoryDecision)
		r.Post("/formats/decision", s.postFormatDecision)
		r.Get("/complaints", s.getComplaints)
		r.Get("/complaints/{subject}/{id}", s.getComplaintCase)
		r.Post("/complaints/{subject}/{id}/resolve", s.postResolveComplaints)
		r.Get("/users", s.getUsers)
		r.Get("/users/{id}", s.getUserCard)
		r.Post("/users/{id}/block", s.postBlockUser)
		r.Post("/users/{id}/unblock", s.postUnblockUser)
		r.Put("/users/{id}/role", s.putUserRole)
		r.Post("/users/{id}/reset-2fa", s.postResetTwoFactor)
		r.Get("/audit", s.getAudit)
		r.Get("/settings", s.getSettings)
		r.Put("/settings", s.putSettings)
	})
}

// ===== DTO =====

type adminMeDTO struct {
	User            userDTO           `json:"user"`
	Role            string            `json:"role"`
	Permissions     []string          `json:"permissions"`
	TwoFactor       twoFactorDTO      `json:"twoFactor"`
	RejectTemplates []templateDTO     `json:"rejectTemplates"`
	ReviewFields    map[string]string `json:"reviewFields"`
	UndoSeconds     int               `json:"undoSeconds"`
}

type twoFactorDTO struct {
	Enabled       bool       `json:"enabled"`
	Verified      bool       `json:"verified"`
	VerifiedUntil *time.Time `json:"verifiedUntil"`
	BackupLeft    int        `json:"backupLeft"`
}

type templateDTO struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type codeIn struct {
	Code string `json:"code"`
}

type setupDTO struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
	QR     string `json:"qr,omitempty"` // data:image/png;base64,…
}

type backupDTO struct {
	BackupCodes []string `json:"backupCodes"`
}

type queueItemDTO struct {
	Type         string    `json:"type"`
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	OwnerID      int64     `json:"ownerId"`
	OwnerName    string    `json:"ownerName"`
	Categories   []string  `json:"categories"`
	City         string    `json:"city"`
	WaitingSince time.Time `json:"waitingSince"`
	Mine         bool      `json:"mine"`
}

type queueDTO struct {
	Items  []queueItemDTO `json:"items"`
	Total  int            `json:"total"`
	Oldest *time.Time     `json:"oldest"`
}

type leaseDTO struct {
	ModeratorID int64     `json:"moderatorId"`
	Mine        bool      `json:"mine"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type statusChangeDTO struct {
	From    string    `json:"from"`
	To      string    `json:"to"`
	Reason  string    `json:"reason"`
	ActorID int64     `json:"actorId"`
	Auto    bool      `json:"auto"`
	At      time.Time `json:"at"`
}

type decisionDTO struct {
	ID        int64             `json:"id"`
	Type      string            `json:"type"`
	ItemID    int64             `json:"itemId"`
	Action    string            `json:"action"`
	Reason    string            `json:"reason"`
	Notes     map[string]string `json:"notes"`
	State     string            `json:"state"`
	Moderator string            `json:"moderator,omitempty"`
	ApplyAt   time.Time         `json:"applyAt"`
	CreatedAt time.Time         `json:"createdAt"`
}

type complaintDTO struct {
	ID         int64      `json:"id"`
	Subject    string     `json:"subject"`
	SubjectID  int64      `json:"subjectId"`
	Reporter   string     `json:"reporter,omitempty"`
	ReporterID int64      `json:"reporterId"`
	Reason     string     `json:"reason"`
	Comment    string     `json:"comment"`
	Status     string     `json:"status"`
	Action     string     `json:"action"`
	Resolution string     `json:"resolution"`
	ResolvedAt *time.Time `json:"resolvedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type adminUserDTO struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Email         string     `json:"email"`
	City          string     `json:"city"`
	AvatarURL     string     `json:"avatarUrl"`
	Role          string     `json:"role"`
	BusinessName  string     `json:"businessName"`
	CreatorName   string     `json:"creatorName"`
	Blocked       bool       `json:"blocked"`
	BlockedAt     *time.Time `json:"blockedAt"`
	BlockedReason string     `json:"blockedReason"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastLoginAt   *time.Time `json:"lastLoginAt"`
}

type adminCategoryDTO struct {
	ID       int64  `json:"id"`
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	AuthorID int64  `json:"authorId"`
}

type reviewItemDTO struct {
	Type         string             `json:"type"`
	ID           string             `json:"id"`
	InQueue      bool               `json:"inQueue"`
	WaitingSince *time.Time         `json:"waitingSince"`
	Lease        *leaseDTO          `json:"lease"`
	Task         *taskDTO           `json:"task,omitempty"`
	LiveTask     *taskDTO           `json:"liveTask,omitempty"`
	Card         *cardDTO           `json:"card,omitempty"`
	LiveCard     *cardDTO           `json:"liveCard,omitempty"`
	Category     *adminCategoryDTO  `json:"category,omitempty"`
	Usage        *usageDTO          `json:"usage,omitempty"`
	Targets      []adminCategoryDTO `json:"targets,omitempty"`
	Format       *formatReviewDTO   `json:"format,omitempty"`
	Formats      []string           `json:"formats,omitempty"`
	Owner        *adminUserDTO      `json:"owner"`
	History      []statusChangeDTO  `json:"history"`
	Decisions    []decisionDTO      `json:"decisions"`
	Complaints   []complaintDTO     `json:"complaints"`
}

type usageDTO struct {
	Tasks int `json:"tasks"`
	Cards int `json:"cards"`
}

type formatReviewDTO struct {
	Norm  string `json:"norm"`
	Title string `json:"title"`
	Tasks int    `json:"tasks"`
}

func toAdminUser(u domain.UserSummary) adminUserDTO {
	return adminUserDTO{ID: u.ID, Name: u.Name, Email: u.Email, City: u.City, AvatarURL: u.AvatarURL, Role: string(u.StaffRole),
		BusinessName: u.BusinessName, CreatorName: u.CreatorName, Blocked: u.Blocked(), BlockedAt: utcPtr(u.BlockedAt),
		BlockedReason: u.BlockedReason, CreatedAt: u.CreatedAt.UTC(), LastLoginAt: utcPtr(u.LastLoginAt)}
}

func toDecisionDTO(d domain.ReviewDecision) decisionDTO {
	n := d.Notes
	if n == nil {
		n = map[string]string{}
	}
	return decisionDTO{ID: d.ID, Type: string(d.Kind), ItemID: d.ItemID, Action: string(d.Action), Reason: d.Reason, Notes: n,
		State: d.State, Moderator: d.Moderator, ApplyAt: d.ApplyAt.UTC(), CreatedAt: d.CreatedAt.UTC()}
}

func toComplaintsDTO(cs []domain.Complaint) []complaintDTO {
	out := make([]complaintDTO, 0, len(cs))
	for _, c := range cs {
		out = append(out, complaintDTO{ID: c.ID, Subject: c.Subject, SubjectID: c.SubjectID, Reporter: c.Reporter, ReporterID: c.ReporterID,
			Reason: c.Reason, Comment: c.Comment, Status: c.Status, Action: c.Action, Resolution: c.Resolution,
			ResolvedAt: utcPtr(c.ResolvedAt), CreatedAt: c.CreatedAt.UTC()})
	}
	return out
}

func toCategoryDTO(c domain.CategoryInfo) adminCategoryDTO {
	return adminCategoryDTO{ID: c.ID, Slug: c.Slug, Title: c.Title, Status: c.Status, AuthorID: c.AuthorID}
}

// ===== 2FA =====

// GET /api/admin/me — роль, права, состояние 2FA и справочники для карточки проверки.
func (s *Server) getAdminMe(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	st, err := s.twoFactor.Status(r.Context(), u, sessionToken(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	perms := make([]string, 0)
	for _, p := range u.StaffRole.Permissions() {
		perms = append(perms, string(p))
	}
	tpl := make([]templateDTO, 0, len(domain.RejectTemplates))
	for _, t := range domain.RejectTemplates {
		tpl = append(tpl, templateDTO{ID: t.ID, Text: t.Text})
	}
	writeJSON(w, http.StatusOK, adminMeDTO{
		User: toUserDTO(u), Role: string(u.StaffRole), Permissions: perms,
		TwoFactor:       twoFactorDTO{Enabled: st.Enabled, Verified: st.Verified, VerifiedUntil: utcPtr(st.VerifiedUntil), BackupLeft: st.BackupLeft},
		RejectTemplates: tpl, ReviewFields: service.FieldTitles, UndoSeconds: int(s.review.UndoWindow().Seconds()),
	})
}

// POST /api/admin/2fa/setup — новый секрет и QR-код (пока 2FA не включена).
func (s *Server) postTwoFactorSetup(w http.ResponseWriter, r *http.Request) {
	st, err := s.twoFactor.Setup(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := setupDTO{Secret: st.Secret, URI: st.URI}
	if len(st.QR) > 0 {
		out.QR = "data:image/png;base64," + base64.StdEncoding.EncodeToString(st.QR)
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/admin/2fa/confirm — первый код: 2FA включена, резервные коды (показываются один раз).
func (s *Server) postTwoFactorConfirm(w http.ResponseWriter, r *http.Request) {
	var in codeIn
	if !decode(w, r, &in) {
		return
	}
	codes, err := s.twoFactor.Confirm(r.Context(), currentUser(r), sessionToken(r), clip(in.Code, 32))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, backupDTO{BackupCodes: codes})
}

// POST /api/admin/2fa/verify — подтвердить вход кодом или резервным кодом (на 12 часов).
func (s *Server) postTwoFactorVerify(w http.ResponseWriter, r *http.Request) {
	var in codeIn
	if !decode(w, r, &in) {
		return
	}
	if err := s.twoFactor.Verify(r.Context(), currentUser(r), sessionToken(r), clip(in.Code, 32)); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/admin/2fa/backup-codes — новые резервные коды (старые больше не действуют).
func (s *Server) postBackupCodes(w http.ResponseWriter, r *http.Request) {
	codes, err := s.twoFactor.RegenerateBackup(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, backupDTO{BackupCodes: codes})
}

// ===== сводка и очередь =====

type dashboardDTO struct {
	Queue []struct {
		Type   string     `json:"type"`
		Total  int        `json:"total"`
		Oldest *time.Time `json:"oldest"`
	} `json:"queue"`
	DecidedToday     int `json:"decidedToday"`
	DecidedWeek      int `json:"decidedWeek"`
	AvgReviewSeconds int `json:"avgReviewSeconds"`
	UsersToday       int `json:"usersToday"`
	UsersWeek        int `json:"usersWeek"`
	OpenComplaints   int `json:"openComplaints"`
}

// GET /api/admin/dashboard
func (s *Server) getDashboard(w http.ResponseWriter, r *http.Request) {
	v, err := s.dashboard.Get(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := dashboardDTO{DecidedToday: v.Stats.DecidedToday, DecidedWeek: v.Stats.DecidedWeek, AvgReviewSeconds: int(v.Stats.AvgReviewSeconds),
		UsersToday: v.Stats.UsersToday, UsersWeek: v.Stats.UsersWeek, OpenComplaints: v.Stats.OpenComplaints}
	out.Queue = make([]struct {
		Type   string     `json:"type"`
		Total  int        `json:"total"`
		Oldest *time.Time `json:"oldest"`
	}, 0, len(v.Queue))
	for _, q := range v.Queue {
		out.Queue = append(out.Queue, struct {
			Type   string     `json:"type"`
			Total  int        `json:"total"`
			Oldest *time.Time `json:"oldest"`
		}{string(q.Kind), q.Total, utcPtr(q.Oldest)})
	}
	writeJSON(w, http.StatusOK, out)
}

// queueFilter — отбор очереди из строки запроса (type и category повторяются).
func queueFilter(r *http.Request) domain.QueueFilter {
	q := r.URL.Query()
	f := domain.QueueFilter{City: clip(q.Get("city"), 200), Query: clip(q.Get("q"), 200), Newest: q.Get("sort") == "newest",
		Categories: clipAll(q["category"], 64)}
	for _, k := range clipAll(q["type"], 32) {
		f.Kinds = append(f.Kinds, domain.ReviewKind(k))
	}
	if t, err := time.Parse(time.DateOnly, q.Get("from")); err == nil {
		f.Since = &t
	}
	if t, err := time.Parse(time.DateOnly, q.Get("to")); err == nil {
		t = t.AddDate(0, 0, 1)
		f.Until = &t
	}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.Offset, _ = strconv.Atoi(q.Get("offset"))
	return f
}

func toQueueItems(items []domain.QueueItem) []queueItemDTO {
	out := make([]queueItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, queueItemDTO{Type: string(it.Kind), ID: it.ID, Title: it.Title, OwnerID: it.OwnerID, OwnerName: it.OwnerName,
			Categories: nonNilStr(it.Categories), City: it.City, WaitingSince: it.WaitingSince.UTC(), Mine: it.Mine})
	}
	return out
}

// GET /api/admin/queue — очередь проверки с отбором (type, category, city, from, to, q, sort=oldest|newest).
func (s *Server) getQueue(w http.ResponseWriter, r *http.Request) {
	p, err := s.review.Queue(r.Context(), currentUser(r), queueFilter(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, queueDTO{Items: toQueueItems(p.Items), Total: p.Total, Oldest: utcPtr(p.Oldest)})
}

type nextIn struct {
	Type     []string `json:"type"`
	Category []string `json:"category"`
	City     string   `json:"city"`
	Q        string   `json:"q"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Sort     string   `json:"sort"`
	SkipType string   `json:"skipType"`
	SkipID   string   `json:"skipId"`
}

// POST /api/admin/queue/next — взять следующий по отбору (skip — текущий, отпускается); пусто — 204.
func (s *Server) postQueueNext(w http.ResponseWriter, r *http.Request) {
	var in nextIn
	if !decode(w, r, &in) {
		return
	}
	f := domain.QueueFilter{Categories: clipAll(in.Category, 64), City: clip(in.City, 200), Query: clip(in.Q, 200), Newest: in.Sort == "newest"}
	for _, k := range clipAll(in.Type, 32) {
		f.Kinds = append(f.Kinds, domain.ReviewKind(k))
	}
	if t, err := time.Parse(time.DateOnly, in.From); err == nil {
		f.Since = &t
	}
	if t, err := time.Parse(time.DateOnly, in.To); err == nil {
		t = t.AddDate(0, 0, 1)
		f.Until = &t
	}
	it, err := s.review.Next(r.Context(), currentUser(r), f, domain.ReviewKind(clip(in.SkipType, 32)), clip(in.SkipID, 120))
	if isNotFound(err) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toQueueItems([]domain.QueueItem{it})[0])
}

func reviewRef(r *http.Request) (domain.ReviewKind, string) {
	return domain.ReviewKind(clip(chi.URLParam(r, "kind"), 32)), clip(chi.URLParam(r, "id"), 120)
}

// GET /api/admin/review/{kind}/{id} — карточка проверки (только чтение).
func (s *Server) getReviewItem(w http.ResponseWriter, r *http.Request) {
	kind, id := reviewRef(r)
	u := currentUser(r)
	it, err := s.review.Item(r.Context(), u, kind, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := reviewItemDTO{Type: string(it.Kind), ID: it.ID, InQueue: it.InQueue, WaitingSince: utcPtr(it.WaitingSince),
		History: make([]statusChangeDTO, 0, len(it.History)), Decisions: make([]decisionDTO, 0, len(it.Decisions)),
		Complaints: toComplaintsDTO(it.Complaints)}
	if it.Lease != nil {
		out.Lease = &leaseDTO{ModeratorID: it.Lease.ModeratorID, Mine: it.Lease.ModeratorID == u.ID, ExpiresAt: it.Lease.ExpiresAt.UTC()}
	}
	if it.Task != nil {
		t := toTaskDTO(*it.Task)
		t.Revision = nil
		out.Task = &t
	}
	if it.LiveTask != nil {
		t := toTaskDTO(*it.LiveTask)
		out.LiveTask = &t
	}
	if it.Card != nil {
		c := toCardDTO(it.Card)
		c.Revision = nil
		out.Card = c
	}
	out.LiveCard = toCardDTO(it.LiveCard)
	if it.Category != nil {
		c := toCategoryDTO(*it.Category)
		out.Category = &c
		out.Usage = &usageDTO{Tasks: it.Usage[0], Cards: it.Usage[1]}
		for _, t := range it.Targets {
			out.Targets = append(out.Targets, toCategoryDTO(t))
		}
	}
	if it.Format != nil {
		out.Format = &formatReviewDTO{Norm: it.Format.Norm, Title: it.Format.Title, Tasks: it.Format.Tasks}
		out.Formats = it.Formats
	}
	if it.Owner != nil {
		o := toAdminUser(*it.Owner)
		out.Owner = &o
	}
	for _, h := range it.History {
		out.History = append(out.History, statusChangeDTO{From: string(h.From), To: string(h.To), Reason: h.Reason, ActorID: h.ActorID, Auto: h.Auto, At: h.At.UTC()})
	}
	for _, d := range it.Decisions {
		out.Decisions = append(out.Decisions, toDecisionDTO(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/admin/review/{kind}/{id}/claim — взять в работу на 10 минут.
func (s *Server) postClaim(w http.ResponseWriter, r *http.Request) {
	kind, id := reviewRef(r)
	u := currentUser(r)
	l, err := s.review.Claim(r.Context(), u, kind, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, leaseDTO{ModeratorID: l.ModeratorID, Mine: l.ModeratorID == u.ID, ExpiresAt: l.ExpiresAt.UTC()})
}

// POST /api/admin/review/{kind}/{id}/release — отпустить.
func (s *Server) postRelease(w http.ResponseWriter, r *http.Request) {
	kind, id := reviewRef(r)
	if err := s.review.Release(r.Context(), currentUser(r), kind, id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type decisionIn struct {
	Action string            `json:"action"`
	Reason string            `json:"reason"`
	Notes  map[string]string `json:"notes"`
}

// POST /api/admin/review/{kind}/{id}/decision — решение (применится через 10 с; до этого — отмена).
func (s *Server) postDecision(w http.ResponseWriter, r *http.Request) {
	kind, id := reviewRef(r)
	var in decisionIn
	if !decodeN(w, r, &in, 32<<10) {
		return
	}
	if len(in.Notes) > 20 {
		writeError(w, http.StatusBadRequest, "bad_request", "Слишком много замечаний.")
		return
	}
	notes := make(map[string]string, len(in.Notes))
	for k, v := range in.Notes {
		notes[clip(k, 32)] = clip(v, 1000)
	}
	d, err := s.review.Decide(r.Context(), currentUser(r), kind, id, service.DecisionInput{
		Action: domain.ReviewAction(clip(in.Action, 16)), Reason: clip(in.Reason, 2000), Notes: notes})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toDecisionDTO(d))
}

// POST /api/admin/decisions/{id}/undo — отменить своё решение в течение 10 с.
func (s *Server) postUndo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	d, err := s.review.Undo(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDecisionDTO(d))
}

type categoryDecisionIn struct {
	Action string `json:"action"`
	Into   string `json:"into"`
	Reason string `json:"reason"`
}

// POST /api/admin/categories/{id}/decision — своя категория: approve | merge (into — slug общей) | reject (reason).
func (s *Server) postCategoryDecision(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in categoryDecisionIn
	if !decode(w, r, &in) {
		return
	}
	if err := s.review.DecideCategory(r.Context(), currentUser(r), id, clip(in.Action, 16), clip(in.Into, 64), clip(in.Reason, 2000)); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type formatDecisionIn struct {
	Norm   string `json:"norm"`
	Action string `json:"action"`
	Into   string `json:"into"`
}

// POST /api/admin/formats/decision — свой формат: approve | merge (into) | reject.
func (s *Server) postFormatDecision(w http.ResponseWriter, r *http.Request) {
	var in formatDecisionIn
	if !decode(w, r, &in) {
		return
	}
	if err := s.review.DecideFormat(r.Context(), currentUser(r), clip(in.Norm, 120), clip(in.Action, 16), clip(in.Into, 64)); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ===== жалобы =====

type complaintIn struct {
	Subject string `json:"subject"`
	ID      int64  `json:"id"`
	Reason  string `json:"reason"`
	Comment string `json:"comment"`
}

// POST /api/complaints — пожаловаться на задание, анкету или профиль.
func (s *Server) postComplaint(w http.ResponseWriter, r *http.Request) {
	var in complaintIn
	if !decode(w, r, &in) {
		return
	}
	created, err := s.complaints.Create(r.Context(), currentUser(r), service.ComplaintInput{Subject: clip(in.Subject, 32), ID: in.ID,
		Reason: clip(in.Reason, 32), Comment: clip(in.Comment, 4000)})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]bool{"created": created})
}

type complaintGroupDTO struct {
	Subject    string    `json:"subject"`
	SubjectID  int64     `json:"subjectId"`
	TargetID   int64     `json:"targetId"`
	TargetName string    `json:"targetName"`
	Total      int       `json:"total"`
	Reasons    []string  `json:"reasons"`
	FirstAt    time.Time `json:"firstAt"`
}

// GET /api/admin/complaints — открытые жалобы по предметам (subject, limit, offset).
func (s *Server) getComplaints(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lim, _ := strconv.Atoi(q.Get("limit"))
	off, _ := strconv.Atoi(q.Get("offset"))
	gs, total, err := s.complaints.Groups(r.Context(), currentUser(r), clip(q.Get("subject"), 32), lim, off)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]complaintGroupDTO, 0, len(gs))
	for _, g := range gs {
		out = append(out, complaintGroupDTO{Subject: g.Subject, SubjectID: g.SubjectID, TargetID: g.TargetUserID, TargetName: g.TargetName,
			Total: g.Total, Reasons: nonNilStr(g.Reasons), FirstAt: g.FirstAt.UTC()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

type complaintCaseDTO struct {
	Subject    string         `json:"subject"`
	ID         int64          `json:"id"`
	Complaints []complaintDTO `json:"complaints"`
	Task       *taskDTO       `json:"task,omitempty"`
	Card       *cardDTO       `json:"card,omitempty"`
	Target     *adminUserDTO  `json:"target"`
}

func complaintRef(w http.ResponseWriter, r *http.Request) (string, int64, bool) {
	subject := chi.URLParam(r, "subject")
	switch subject {
	case string(domain.SubjectTask), string(domain.SubjectCard), "user":
	default:
		writeError(w, http.StatusNotFound, "not_found", "Не найдено.")
		return "", 0, false
	}
	id, ok := pathID(w, r)
	return subject, id, ok
}

// GET /api/admin/complaints/{subject}/{id} — жалобы на предмет и сам предмет.
func (s *Server) getComplaintCase(w http.ResponseWriter, r *http.Request) {
	subject, id, ok := complaintRef(w, r)
	if !ok {
		return
	}
	c, err := s.complaints.Case(r.Context(), currentUser(r), subject, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := complaintCaseDTO{Subject: c.Subject, ID: c.ID, Complaints: toComplaintsDTO(c.Complaints), Card: toCardDTO(c.Card)}
	if c.Task != nil {
		t := toTaskDTO(*c.Task)
		out.Task = &t
	}
	if c.Target != nil {
		t := toAdminUser(*c.Target)
		out.Target = &t
	}
	writeJSON(w, http.StatusOK, out)
}

type resolveIn struct {
	Action string `json:"action"`
	Note   string `json:"note"`
}

// POST /api/admin/complaints/{subject}/{id}/resolve — dismiss | hide | block.
func (s *Server) postResolveComplaints(w http.ResponseWriter, r *http.Request) {
	subject, id, ok := complaintRef(w, r)
	if !ok {
		return
	}
	var in resolveIn
	if !decode(w, r, &in) {
		return
	}
	n, err := s.complaints.Resolve(r.Context(), currentUser(r), subject, id, clip(in.Action, 16), clip(in.Note, 2000))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"closed": n})
}

// ===== пользователи =====

// GET /api/admin/users — поиск (q, staff=1, blocked=1, limit, offset).
func (s *Server) getUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lim, _ := strconv.Atoi(q.Get("limit"))
	off, _ := strconv.Atoi(q.Get("offset"))
	us, err := s.staffUsers.Search(r.Context(), currentUser(r), service.UserFilter{Query: clip(q.Get("q"), 200),
		StaffOnly: q.Get("staff") == "1", BlockedOnly: q.Get("blocked") == "1", Limit: lim, Offset: off})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]adminUserDTO, 0, len(us))
	for _, u := range us {
		out = append(out, toAdminUser(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

type socialDTO struct {
	Platform   string     `json:"platform"`
	Type       string     `json:"type"`
	Title      string     `json:"title"`
	Handle     string     `json:"handle"`
	URL        string     `json:"url"`
	Status     string     `json:"status"`
	Followers  int64      `json:"followers"`
	VerifiedAt *time.Time `json:"verifiedAt"`
}

type loginEventDTO struct {
	Method string    `json:"method"`
	IP     string    `json:"ip"`
	Device string    `json:"device"`
	At     time.Time `json:"at"`
}

type userCardDTO struct {
	User       adminUserDTO    `json:"user"`
	Providers  []string        `json:"providers"`
	Business   *businessDTO    `json:"business"`
	Creator    *creatorDTO     `json:"creator"`
	Tasks      []taskDTO       `json:"tasks"`
	Card       *cardDTO        `json:"card"`
	Socials    []socialDTO     `json:"socials"`
	Logins     []loginEventDTO `json:"logins"`
	Decisions  []decisionDTO   `json:"decisions"`
	Complaints []complaintDTO  `json:"complaints"`
}

// GET /api/admin/users/{id} — карточка пользователя.
func (s *Server) getUserCard(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	c, err := s.staffUsers.Card(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := userCardDTO{User: toAdminUser(c.User), Providers: nonNilStr(c.Providers), Tasks: toTasksDTO(c.Tasks), Card: toCardDTO(c.Card),
		Socials: make([]socialDTO, 0, len(c.Socials)), Logins: make([]loginEventDTO, 0, len(c.Logins)),
		Decisions: make([]decisionDTO, 0, len(c.Decisions)), Complaints: toComplaintsDTO(c.Complaints)}
	out.Business, out.Creator = toBusinessDTO(c.Profiles.Business), toCreatorDTO(c.Profiles.Creator)
	for _, a := range c.Socials {
		out.Socials = append(out.Socials, socialDTO{Platform: string(a.Platform), Type: a.Kind, Title: a.Title, Handle: a.Handle,
			URL: a.URL, Status: string(a.Status), Followers: derefInt(a.Stats.Followers), VerifiedAt: utcPtr(a.VerifiedAt)})
	}
	for _, l := range c.Logins {
		out.Logins = append(out.Logins, loginEventDTO{Method: l.Method, IP: l.IP, Device: service.DescribeUA(l.UserAgent), At: l.At.UTC()})
	}
	for _, d := range c.Decisions {
		out.Decisions = append(out.Decisions, toDecisionDTO(d))
	}
	writeJSON(w, http.StatusOK, out)
}

type reasonIn struct {
	Reason string `json:"reason"`
}

// POST /api/admin/users/{id}/block — заблокировать (выход везде, скрытие из ленты).
func (s *Server) postBlockUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in reasonIn
	if !decode(w, r, &in) {
		return
	}
	u, err := s.staffUsers.Block(r.Context(), currentUser(r), id, clip(in.Reason, 2000))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminUser(domain.UserSummary{User: u}))
}

// POST /api/admin/users/{id}/unblock — разблокировать.
func (s *Server) postUnblockUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in reasonIn
	if !decode(w, r, &in) {
		return
	}
	u, err := s.staffUsers.Unblock(r.Context(), currentUser(r), id, clip(in.Reason, 2000))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminUser(domain.UserSummary{User: u}))
}

type roleIn struct {
	Role string `json:"role"` // "" | moderator | admin
}

// PUT /api/admin/users/{id}/role — назначить роль (только администратор).
func (s *Server) putUserRole(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in roleIn
	if !decode(w, r, &in) {
		return
	}
	u, err := s.staffUsers.SetRole(r.Context(), currentUser(r), id, domain.StaffRole(clip(strings.TrimSpace(in.Role), 16)))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminUser(domain.UserSummary{User: u}))
}

// POST /api/admin/users/{id}/reset-2fa — сбросить 2FA сотрудника (только администратор).
func (s *Server) postResetTwoFactor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.twoFactor.Reset(r.Context(), currentUser(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ===== журнал и настройки =====

type auditDTO struct {
	ID         int64     `json:"id"`
	ActorID    int64     `json:"actorId"`
	ActorLabel string    `json:"actorLabel"`
	ActorRole  string    `json:"actorRole"`
	Action     string    `json:"action"`
	Subject    string    `json:"subject"`
	SubjectID  string    `json:"subjectId"`
	Before     any       `json:"before"`
	After      any       `json:"after"`
	Reason     string    `json:"reason"`
	IP         string    `json:"ip"`
	Device     string    `json:"device"`
	At         time.Time `json:"at"`
}

func rawOrNil(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return rawJSON(b)
}

// GET /api/admin/audit — журнал (q, actor, action, subject, subjectId, from, to, before — курсор).
func (s *Server) getAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := domain.AuditFilter{Query: clip(q.Get("q"), 200), Action: clip(q.Get("action"), 120), Subject: clip(q.Get("subject"), 120),
		SubjectID: clip(q.Get("subjectId"), 200)}
	f.ActorID, _ = strconv.ParseInt(q.Get("actor"), 10, 64)
	f.BeforeID, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	if t, err := time.Parse(time.DateOnly, q.Get("from")); err == nil {
		f.Since = &t
	}
	if t, err := time.Parse(time.DateOnly, q.Get("to")); err == nil {
		t = t.AddDate(0, 0, 1)
		f.Until = &t
	}
	es, err := s.audit.Search(r.Context(), currentUser(r), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]auditDTO, 0, len(es))
	for _, e := range es {
		out = append(out, auditDTO{ID: e.ID, ActorID: e.ActorID, ActorLabel: e.ActorLabel, ActorRole: string(e.ActorRole), Action: e.Action,
			Subject: e.Subject, SubjectID: e.SubjectID, Before: rawOrNil(e.BeforeJSON), After: rawOrNil(e.AfterJSON), Reason: e.Reason,
			IP: e.IP, Device: service.DescribeUA(e.UserAgent), At: e.At.UTC()})
	}
	lim := f.Limit
	if lim <= 0 || lim > 100 {
		lim = 50
	}
	var next *int64
	if len(out) == lim {
		n := out[len(out)-1].ID
		next = &n
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "next": next})
}

type settingsDTO struct {
	ModerationMode    string `json:"moderationMode"`
	ModerationDefault string `json:"moderationDefault"`
}

// GET /api/admin/settings
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.settings.Get(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsDTO{ModerationMode: string(st.ModerationMode), ModerationDefault: string(st.ModerationDefault)})
}

type settingsIn struct {
	ModerationMode string `json:"moderationMode"`
}

// PUT /api/admin/settings — режим проверки (auto | manual).
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsIn
	if !decode(w, r, &in) {
		return
	}
	st, err := s.settings.SetModerationMode(r.Context(), currentUser(r), domain.ModerationMode(clip(in.ModerationMode, 16)))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsDTO{ModerationMode: string(st.ModerationMode), ModerationDefault: string(st.ModerationDefault)})
}

func derefInt(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

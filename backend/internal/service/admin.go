package service

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"ohvat/backend/internal/domain"
)

// ===== пользователи в админке: поиск, карточка, блокировка, роли =====

// AdminUserRepository — пользователи для админки.
type AdminUserRepository interface {
	SearchUsers(ctx context.Context, q string, staffOnly, blockedOnly bool, limit, offset int) ([]domain.UserSummary, error)
	UserFull(ctx context.Context, id int64) (domain.UserSummary, error)
	// BlockUser — блок, выход везде и журнал — атомарно; уже заблокирован — domain.ErrConflict.
	BlockUser(ctx context.Context, id int64, reason string, now time.Time, e domain.AuditEntry) (domain.User, error)
	UnblockUser(ctx context.Context, id int64, e domain.AuditEntry) (domain.User, error)
	SetStaffRole(ctx context.Context, id int64, role domain.StaffRole, e domain.AuditEntry) (domain.User, error)
	UserLogins(ctx context.Context, userID int64, limit int) ([]domain.LoginEvent, error)
	OwnerDecisions(ctx context.Context, ownerID int64, limit int) ([]domain.ReviewDecision, error)
	ComplaintsAgainst(ctx context.Context, userID int64, limit int) ([]domain.Complaint, error)
}

// UserContent — что есть у пользователя: способы входа, профили, задания, анкета, соцсети.
type UserContent interface {
	Providers(ctx context.Context, userID int64) ([]string, error)
	Profiles(ctx context.Context, userID int64) (domain.Profiles, error)
	OwnTasks(ctx context.Context, ownerID int64, limit int) ([]domain.Task, error)
	Card(ctx context.Context, userID int64) (domain.CreatorCard, error)
	Accounts(ctx context.Context, userID int64) ([]domain.SocialAccount, error)
}

// StaffUsers — пользователи в админке.
type StaffUsers struct {
	repo    AdminUserRepository
	content UserContent
	clock   Clock
	isLast  func(error) bool
}

// NewStaffUsers создаёт сервис пользователей админки. isLastAdmin — ошибка хранилища «последний администратор».
func NewStaffUsers(repo AdminUserRepository, content UserContent, clock Clock, isLastAdmin func(error) bool) *StaffUsers {
	if clock == nil {
		clock = SystemClock{}
	}
	if isLastAdmin == nil {
		isLastAdmin = func(error) bool { return false }
	}
	return &StaffUsers{repo: repo, content: content, clock: clock, isLast: isLastAdmin}
}

// UserFilter — поиск пользователей.
type UserFilter struct {
	Query       string
	StaffOnly   bool
	BlockedOnly bool
	Limit       int
	Offset      int
}

// Search — поиск по id, почте, имени (в т. ч. в профилях).
func (s *StaffUsers) Search(ctx context.Context, u domain.User, f UserFilter) ([]domain.UserSummary, error) {
	if err := authorize(u, domain.PermUsersView); err != nil {
		return nil, err
	}
	f.Query = squash(f.Query)
	if utf8.RuneCountInString(f.Query) > 100 {
		return nil, &ValidationError{Fields: map[string]string{"q": "Поиск — до 100 символов"}}
	}
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	if f.Offset < 0 || f.Offset > 10000 {
		f.Offset = 0
	}
	return s.repo.SearchUsers(ctx, f.Query, f.StaffOnly, f.BlockedOnly, f.Limit, f.Offset)
}

// UserCard — карточка пользователя в админке.
type UserCard struct {
	User       domain.UserSummary
	Providers  []string
	Profiles   domain.Profiles
	Tasks      []domain.Task
	Card       *domain.CreatorCard
	Socials    []domain.SocialAccount
	Logins     []domain.LoginEvent
	Decisions  []domain.ReviewDecision
	Complaints []domain.Complaint
}

// Card — всё о пользователе: профили, способы входа, задания, анкета, соцсети, входы, решения, жалобы.
func (s *StaffUsers) Card(ctx context.Context, u domain.User, id int64) (UserCard, error) {
	if err := authorize(u, domain.PermUsersView); err != nil {
		return UserCard{}, err
	}
	var c UserCard
	var err error
	if c.User, err = s.repo.UserFull(ctx, id); err != nil {
		return c, err
	}
	if c.Providers, err = s.content.Providers(ctx, id); err != nil {
		return c, err
	}
	if c.Profiles, err = s.content.Profiles(ctx, id); err != nil {
		return c, err
	}
	if c.Tasks, err = s.content.OwnTasks(ctx, id, 50); err != nil {
		return c, err
	}
	if card, err := s.content.Card(ctx, id); err == nil {
		c.Card = &card
	} else if !errors.Is(err, domain.ErrNotFound) {
		return c, err
	}
	if c.Socials, err = s.content.Accounts(ctx, id); err != nil {
		return c, err
	}
	if c.Logins, err = s.repo.UserLogins(ctx, id, 30); err != nil {
		return c, err
	}
	if c.Decisions, err = s.repo.OwnerDecisions(ctx, id, 30); err != nil {
		return c, err
	}
	if c.Complaints, err = s.repo.ComplaintsAgainst(ctx, id, 30); err != nil {
		return c, err
	}
	return c, nil
}

// checkReason — причина 5–500 символов.
func checkReason(reason string) (string, error) {
	r := squash(reason)
	if n := utf8.RuneCountInString(r); n < 5 || n > 500 {
		return "", &ValidationError{Fields: map[string]string{"reason": "Причина — от 5 до 500 символов"}}
	}
	return r, nil
}

// Block — заблокировать: вход закрыт, все сеансы завершены, задания и анкета скрыты. Сотрудника — только
// администратор; себя — нельзя.
func (s *StaffUsers) Block(ctx context.Context, u domain.User, id int64, reason string) (domain.User, error) {
	if err := authorize(u, domain.PermUsersBlock); err != nil {
		return domain.User{}, err
	}
	if id == u.ID {
		return domain.User{}, ErrSelfAction
	}
	r, err := checkReason(reason)
	if err != nil {
		return domain.User{}, err
	}
	t, err := s.repo.UserFull(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	if t.Staff() && !u.StaffRole.Can(domain.PermBlockStaff) {
		return domain.User{}, ErrForbidden
	}
	e := auditFor(ctx, u, domain.AuditUserBlock, "user", strconv.FormatInt(id, 10))
	e.Reason, e.Before, e.After = r, map[string]any{"blocked": false}, map[string]any{"blocked": true}
	out, err := s.repo.BlockUser(ctx, id, r, s.clock.Now(), e)
	switch {
	case errors.Is(err, domain.ErrConflict):
		return domain.User{}, ErrWrongStatus
	case s.isLast(err):
		return domain.User{}, ErrLastAdmin
	}
	return out, err
}

// Unblock — разблокировать (с причиной).
func (s *StaffUsers) Unblock(ctx context.Context, u domain.User, id int64, reason string) (domain.User, error) {
	if err := authorize(u, domain.PermUsersBlock); err != nil {
		return domain.User{}, err
	}
	r, err := checkReason(reason)
	if err != nil {
		return domain.User{}, err
	}
	t, err := s.repo.UserFull(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	if t.Staff() && !u.StaffRole.Can(domain.PermBlockStaff) {
		return domain.User{}, ErrForbidden
	}
	e := auditFor(ctx, u, domain.AuditUserUnblock, "user", strconv.FormatInt(id, 10))
	e.Reason, e.Before, e.After = r, map[string]any{"blocked": true, "reason": t.BlockedReason}, map[string]any{"blocked": false}
	out, err := s.repo.UnblockUser(ctx, id, e)
	if errors.Is(err, domain.ErrConflict) {
		return domain.User{}, ErrWrongStatus
	}
	return out, err
}

// SetRole — назначить модератором или администратором (или снять роль): только администратор, не себе; без
// администраторов сайт не остаётся.
func (s *StaffUsers) SetRole(ctx context.Context, u domain.User, id int64, role domain.StaffRole) (domain.User, error) {
	if err := authorize(u, domain.PermManageStaff); err != nil {
		return domain.User{}, err
	}
	if !domain.ValidStaffRole(role) {
		return domain.User{}, &ValidationError{Fields: map[string]string{"role": "Роль — модератор, администратор или без роли"}}
	}
	// Полный администратор назначается только через ADMIN_EMAILS (bootstrap при входе) — здесь модераторы.
	if role == domain.RoleAdmin {
		return domain.User{}, &ValidationError{Fields: map[string]string{"role": "Администраторов добавляет владелец через ADMIN_EMAILS. Здесь можно назначить только модератора"}}
	}
	if id == u.ID {
		return domain.User{}, ErrSelfAction
	}
	t, err := s.repo.UserFull(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	if t.Blocked() && role != domain.RoleNone {
		return domain.User{}, ErrWrongStatus
	}
	if t.StaffRole == role {
		return t.User, nil
	}
	e := auditFor(ctx, u, domain.AuditRoleSet, "user", strconv.FormatInt(id, 10))
	e.Before, e.After = map[string]string{"role": string(t.StaffRole)}, map[string]string{"role": string(role)}
	out, err := s.repo.SetStaffRole(ctx, id, role, e)
	if s.isLast(err) {
		return domain.User{}, ErrLastAdmin
	}
	return out, err
}

// ===== журнал действий =====

// AuditRepository — поиск по журналу.
type AuditRepository interface {
	SearchAudit(ctx context.Context, f domain.AuditFilter) ([]domain.AuditEntry, error)
}

// AuditLog — журнал действий сотрудников (только чтение; записи — неизменяемые).
type AuditLog struct{ repo AuditRepository }

// NewAuditLog создаёт сервис журнала.
func NewAuditLog(repo AuditRepository) *AuditLog { return &AuditLog{repo: repo} }

// Search — поиск: текст, сотрудник, действие, предмет, период; новые сверху, страница до 100.
func (s *AuditLog) Search(ctx context.Context, u domain.User, f domain.AuditFilter) ([]domain.AuditEntry, error) {
	if err := authorize(u, domain.PermAuditView); err != nil {
		return nil, err
	}
	f.Query, f.Action, f.Subject, f.SubjectID = squash(f.Query), squash(f.Action), squash(f.Subject), squash(f.SubjectID)
	ve := &ValidationError{}
	maxLen(ve, "q", f.Query, 100, "Поиск — до 100 символов")
	maxLen(ve, "action", f.Action, 60, "Действие — до 60 символов")
	maxLen(ve, "subject", f.Subject, 60, "Предмет — до 60 символов")
	maxLen(ve, "subjectId", f.SubjectID, 120, "Id — до 120 символов")
	if err := ve.orNil(); err != nil {
		return nil, err
	}
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	return s.repo.SearchAudit(ctx, f)
}

// ===== настройки =====

// SettingsRepository — настройки в базе.
type SettingsRepository interface {
	Setting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string, by int64, e domain.AuditEntry) error
}

// Settings — настройки, которые меняет администратор (режим проверки). Значение из базы кэшируется на 5 с;
// без записи в базе — значение по умолчанию из окружения (MODERATION_MODE).
type Settings struct {
	repo  SettingsRepository
	def   domain.ModerationMode
	clock Clock
	log   *slog.Logger

	mu     sync.Mutex
	cached domain.ModerationMode
	at     time.Time
}

// NewSettings создаёт сервис настроек.
func NewSettings(repo SettingsRepository, defMode domain.ModerationMode, clock Clock, log *slog.Logger) *Settings {
	if clock == nil {
		clock = SystemClock{}
	}
	if log == nil {
		log = slog.Default()
	}
	if defMode != domain.ModerationManual {
		defMode = domain.ModerationAuto
	}
	return &Settings{repo: repo, def: defMode, clock: clock, log: log}
}

var _ ModeSource = (*Settings)(nil)

// ModerationMode — текущий режим проверки (реализует ModeSource). Сбой базы — значение по умолчанию.
func (s *Settings) ModerationMode(ctx context.Context) domain.ModerationMode {
	s.mu.Lock()
	if !s.at.IsZero() && s.clock.Now().Sub(s.at) < 5*time.Second {
		m := s.cached
		s.mu.Unlock()
		return m
	}
	s.mu.Unlock()
	m := s.def
	v, ok, err := s.repo.Setting(ctx, domain.SettingModerationMode)
	if err != nil {
		s.log.WarnContext(ctx, "настройки: режим проверки — по умолчанию", "err", err)
		return s.def
	}
	if ok && (v == string(domain.ModerationAuto) || v == string(domain.ModerationManual)) {
		m = domain.ModerationMode(v)
	}
	s.mu.Lock()
	s.cached, s.at = m, s.clock.Now()
	s.mu.Unlock()
	return m
}

// AdminSettings — настройки для страницы админки.
type AdminSettings struct {
	ModerationMode    domain.ModerationMode
	ModerationDefault domain.ModerationMode // из окружения
}

// Get — настройки (только администратор).
func (s *Settings) Get(ctx context.Context, u domain.User) (AdminSettings, error) {
	if err := authorize(u, domain.PermSettings); err != nil {
		return AdminSettings{}, err
	}
	s.mu.Lock()
	s.at = time.Time{}
	s.mu.Unlock()
	return AdminSettings{ModerationMode: s.ModerationMode(ctx), ModerationDefault: s.def}
}

// SetModerationMode — переключить режим проверки (auto | manual) с записью в журнал.
func (s *Settings) SetModerationMode(ctx context.Context, u domain.User, mode domain.ModerationMode) (AdminSettings, error) {
	if err := authorize(u, domain.PermSettings); err != nil {
		return AdminSettings{}, err
	}
	if mode != domain.ModerationAuto && mode != domain.ModerationManual {
		return AdminSettings{}, &ValidationError{Fields: map[string]string{"moderationMode": "Режим — auto или manual"}}
	}
	before := s.ModerationMode(ctx)
	e := auditFor(ctx, u, domain.AuditSetting, "setting", domain.SettingModerationMode)
	e.Before, e.After = map[string]string{"moderationMode": string(before)}, map[string]string{"moderationMode": string(mode)}
	if err := s.repo.SetSetting(ctx, domain.SettingModerationMode, string(mode), u.ID, e); err != nil {
		return AdminSettings{}, err
	}
	s.mu.Lock()
	s.cached, s.at = mode, s.clock.Now()
	s.mu.Unlock()
	return AdminSettings{ModerationMode: mode, ModerationDefault: s.def}
}

// ===== сводка =====

// DashboardRepository — цифры сводки.
type DashboardRepository interface {
	QueueByKind(ctx context.Context) ([]domain.QueueCount, error)
	DashboardStats(ctx context.Context, dayStart, weekStart time.Time) (domain.DashboardStats, error)
}

// Dashboard — простые цифры для админки.
type Dashboard struct {
	repo  DashboardRepository
	clock Clock
}

// NewDashboard создаёт сервис сводки.
func NewDashboard(repo DashboardRepository, clock Clock) *Dashboard {
	if clock == nil {
		clock = SystemClock{}
	}
	return &Dashboard{repo: repo, clock: clock}
}

// DashboardView — очередь по видам и цифры за сегодня и неделю (по Москве).
type DashboardView struct {
	Queue []domain.QueueCount
	Stats domain.DashboardStats
}

// Get — сводка.
func (s *Dashboard) Get(ctx context.Context, u domain.User) (DashboardView, error) {
	if err := authorize(u, domain.PermDashboard); err != nil {
		return DashboardView{}, err
	}
	q, err := s.repo.QueueByKind(ctx)
	if err != nil {
		return DashboardView{}, err
	}
	now := s.clock.Now().In(msk)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, msk)
	st, err := s.repo.DashboardStats(ctx, day.UTC(), day.AddDate(0, 0, -6).UTC())
	if err != nil {
		return DashboardView{}, err
	}
	return DashboardView{Queue: q, Stats: st}, nil
}

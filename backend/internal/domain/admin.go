package domain

import "time"

// ===== роли сотрудников и права =====

// StaffRole — роль сотрудника; "" — обычный пользователь.
type StaffRole string

// Роли: модератор — проверка и жалобы; администратор — всё и управление модераторами.
const (
	RoleNone      StaffRole = ""
	RoleModerator StaffRole = "moderator"
	RoleAdmin     StaffRole = "admin"
)

// ValidStaffRole — роль из списка (включая «нет роли»).
func ValidStaffRole(r StaffRole) bool { return r == RoleNone || r == RoleModerator || r == RoleAdmin }

// Permission — действие в админке. Права проверяет сервис на каждом вызове (транспорт — только вход и 2FA).
type Permission string

// Права.
const (
	PermReview       Permission = "review"        // очередь, карточка проверки, решения, свои категории и форматы
	PermComplaints   Permission = "complaints"    // очередь жалоб и решения по ним
	PermUsersView    Permission = "users.view"    // поиск и карточка пользователя
	PermUsersBlock   Permission = "users.block"   // блокировка и разблокировка
	PermDashboard    Permission = "dashboard"     // сводка
	PermManageStaff  Permission = "staff.manage"  // назначение модераторов и администраторов, сброс 2FA
	PermSettings     Permission = "settings"      // режим проверки (auto | manual)
	PermAuditView    Permission = "audit.view"    // журнал действий
	PermBlockStaff   Permission = "staff.block"   // блокировка сотрудников
	PermViewPrivates Permission = "media.private" // фото на проверке (не опубликованные)
)

var rolePerms = map[StaffRole][]Permission{
	RoleModerator: {PermReview, PermComplaints, PermUsersView, PermUsersBlock, PermDashboard, PermViewPrivates},
	RoleAdmin: {PermReview, PermComplaints, PermUsersView, PermUsersBlock, PermDashboard, PermViewPrivates,
		PermManageStaff, PermSettings, PermAuditView, PermBlockStaff},
}

// Can — есть ли у роли право.
func (r StaffRole) Can(p Permission) bool {
	for _, x := range rolePerms[r] {
		if x == p {
			return true
		}
	}
	return false
}

// Permissions — все права роли (для интерфейса).
func (r StaffRole) Permissions() []Permission { return append([]Permission(nil), rolePerms[r]...) }

// Staff — роль в админке у пользователя.
func (u User) Staff() bool { return u.StaffRole == RoleModerator || u.StaffRole == RoleAdmin }

// Blocked — пользователь заблокирован.
func (u User) Blocked() bool { return u.BlockedAt != nil }

// ===== 2FA =====

// TOTP — секрет 2FA сотрудника (секрет — зашифрованный).
type TOTP struct {
	UserID      int64
	Secret      []byte
	ConfirmedAt *time.Time
	LastStep    int64
	LockedUntil *time.Time
}

// ===== журнал действий =====

// AuditEntry — запись журнала: кто, что, над чем, до/после, причина, откуда.
type AuditEntry struct {
	ID         int64
	ActorID    int64
	ActorLabel string
	ActorRole  StaffRole
	Action     string
	Subject    string
	SubjectID  string
	Before     any // сериализуется в JSON; nil — нет
	After      any
	BeforeJSON []byte // при чтении
	AfterJSON  []byte
	Reason     string
	IP         string
	UserAgent  string
	At         time.Time
}

// Действия журнала.
const (
	AuditRoleBootstrap   = "role.bootstrap"   // первый админ из ADMIN_EMAILS
	AuditRoleSet         = "role.set"         // назначение роли
	AuditTwoFAEnabled    = "2fa.enabled"      // 2FA настроена
	AuditTwoFAReset      = "2fa.reset"        // администратор сбросил 2FA сотрудника
	AuditBackupRegen     = "2fa.backup_regen" // новые резервные коды
	AuditTwoFAFailed     = "2fa.locked"       // перебор кодов — блокировка
	AuditBackupUsed      = "2fa.backup_used"  // вход по резервному коду
	AuditDecision        = "review.decision"  // решение по заданию или анкете (до применения)
	AuditDecisionUndo    = "review.undo"      // решение отменено за 10 с
	AuditDecisionApplied = "review.applied"   // решение применено
	AuditCategory        = "review.category"  // своя категория: одобрена, объединена, отклонена
	AuditFormat          = "review.format"    // свой формат: одобрен, объединён, отклонён
	AuditUserBlock       = "user.block"
	AuditUserUnblock     = "user.unblock"
	AuditComplaint       = "complaint.resolve"
	AuditSetting         = "settings.update"
)

// AuditFilter — поиск по журналу; пустые поля — без отбора.
type AuditFilter struct {
	Query     string
	ActorID   int64
	Action    string
	Subject   string
	SubjectID string
	Since     *time.Time
	Until     *time.Time
	BeforeID  int64 // курсор: записи старше этого id
	Limit     int
}

// ===== очередь проверки =====

// ReviewKind — что проверяется.
type ReviewKind string

// Виды элементов очереди.
const (
	KindTask         ReviewKind = "task"
	KindTaskRevision ReviewKind = "task_revision"
	KindCard         ReviewKind = "card"
	KindCardRevision ReviewKind = "card_revision"
	KindCategory     ReviewKind = "category"
	KindFormat       ReviewKind = "format"
	KindPage         ReviewKind = "page"          // страница бизнеса или профиль автора
	KindPageRevision ReviewKind = "page_revision" // правка опубликованной страницы
)

// ReviewKinds — все виды (порядок — для фильтра).
var ReviewKinds = []ReviewKind{KindTask, KindTaskRevision, KindCard, KindCardRevision, KindPage, KindPageRevision, KindCategory, KindFormat}

// ValidReviewKind — вид из списка.
func ValidReviewKind(k ReviewKind) bool {
	for _, x := range ReviewKinds {
		if x == k {
			return true
		}
	}
	return false
}

// Listing — задание, анкета или страница (с решением, отменой и уведомлением).
func (k ReviewKind) Listing() bool {
	return k == KindTask || k == KindTaskRevision || k == KindCard || k == KindCardRevision || k == KindPage || k == KindPageRevision
}

// Revision — правка опубликованного.
func (k ReviewKind) Revision() bool {
	return k == KindTaskRevision || k == KindCardRevision || k == KindPageRevision
}

// QueueItem — строка очереди.
type QueueItem struct {
	Kind         ReviewKind
	ID           string
	Title        string
	OwnerID      int64
	OwnerName    string
	Categories   []string
	City         string
	WaitingSince time.Time
	Mine         bool // взято в работу этим модератором
}

// QueueFilter — отбор очереди; пусто — без отбора.
type QueueFilter struct {
	Kinds      []ReviewKind
	Categories []string
	City       string
	Since      *time.Time
	Until      *time.Time
	Query      string
	Newest     bool // новые первыми (по умолчанию — старые)
	Limit      int
	Offset     int
}

// Lease — элемент в работе у модератора до ExpiresAt.
type Lease struct {
	Kind        ReviewKind
	ItemID      string
	ModeratorID int64
	TakenAt     time.Time
	ExpiresAt   time.Time
}

// ReviewAction — решение модератора.
type ReviewAction string

// Решения: опубликовать, отклонить (причина), вернуть на доработку (замечания по полям).
const (
	ActionApprove ReviewAction = "approve"
	ActionReject  ReviewAction = "reject"
	ActionRework  ReviewAction = "rework"
)

// Состояния решения.
const (
	DecisionPending = "pending"
	DecisionApplied = "applied"
	DecisionUndone  = "undone"
	DecisionFailed  = "failed"
)

// ReviewDecision — решение модератора: применяется в ApplyAt (до этого — можно отменить).
type ReviewDecision struct {
	ID           int64
	Kind         ReviewKind
	ItemID       int64
	OwnerID      int64
	ModeratorID  int64
	Moderator    string // имя (в истории)
	Action       ReviewAction
	Reason       string
	Notes        map[string]string
	WaitingSince *time.Time
	State        string
	ApplyAt      time.Time
	AppliedAt    *time.Time
	Error        string
	CreatedAt    time.Time
}

// RejectTemplate — шаблон причины отказа.
type RejectTemplate struct {
	ID   string
	Text string
}

// RejectTemplates — шаблоны причин (модератор может дописать свой текст).
var RejectTemplates = []RejectTemplate{
	{"incomplete", "Описание неполное: непонятно, что нужно сделать и как будет проверен результат"},
	{"prohibited", "Запрещённый товар или услуга (алкоголь, табак, азартные игры, лекарства, оружие и т. п.)"},
	{"contacts", "В тексте контакты или ссылки для связи в обход площадки"},
	{"photos", "Фото не подходят: чужие, низкого качества или не по теме"},
	{"misleading", "Недостоверные сведения или вводящее в заблуждение предложение"},
	{"offensive", "Оскорбительное или недопустимое содержание"},
	{"duplicate", "Повтор уже опубликованного"},
	{"socials", "Соцсети не соответствуют анкете или накрученные цифры"},
}

// ReviewFields — поля, к которым модератор оставляет замечания «на доработку».
var ReviewFields = map[ReviewKind][]string{
	KindTask: {"title", "categories", "city", "formats", "minViews", "reward", "publishBy", "blocks", "cover", "photos"},
	KindCard: {"name", "city", "topics", "platforms", "blocks", "avatar", "works"},
	KindPage: {"title", "city", "spheres", "logo", "cover", "blocks", "slug"},
}

// ===== жалобы =====

// Причины жалобы.
var ComplaintReasons = []string{"spam", "fraud", "offensive", "illegal", "fake", "other"}

// Что сделано по жалобе.
const (
	ComplaintDismiss = "dismiss" // жалоба не подтвердилась
	ComplaintHide    = "hide"    // снять с публикации
	ComplaintBlock   = "block"   // заблокировать пользователя
)

// Complaint — жалоба пользователя.
type Complaint struct {
	ID           int64
	ReporterID   int64
	Reporter     string
	Subject      string // task | creator_card | user
	SubjectID    int64
	TargetUserID int64
	Reason       string
	Comment      string
	Status       string // open | resolved | dismissed
	Action       string
	Resolution   string
	ResolvedBy   int64
	ResolvedAt   *time.Time
	CreatedAt    time.Time
}

// ComplaintGroup — открытые жалобы на один предмет.
type ComplaintGroup struct {
	Subject      string
	SubjectID    int64
	TargetUserID int64
	TargetName   string
	Total        int
	Reasons      []string
	FirstAt      time.Time
}

// ===== пользователи в админке =====

// UserSummary — строка поиска пользователей.
type UserSummary struct {
	User
	BusinessName string
	CreatorName  string
}

// LoginEvent — вход пользователя.
type LoginEvent struct {
	Method    string
	IP        string
	UserAgent string
	At        time.Time
}

// ===== настройки =====

// Ключи настроек в базе.
const SettingModerationMode = "moderation_mode"

// ===== сводка =====

// QueueCount — сколько ждёт по виду и с какого времени самое старое.
type QueueCount struct {
	Kind   ReviewKind
	Total  int
	Oldest *time.Time
}

// DashboardStats — простые цифры.
type DashboardStats struct {
	DecidedToday     int
	DecidedWeek      int
	AvgReviewSeconds float64
	UsersToday       int
	UsersWeek        int
	OpenComplaints   int
}

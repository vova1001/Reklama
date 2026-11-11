package domain

import "time"

// Status — статус модерации задания или анкеты автора.
type Status string

// Статусы: черновик → на проверке → опубликовано | отклонено.
const (
	StatusDraft     Status = "draft"
	StatusPending   Status = "pending_review"
	StatusPublished Status = "published"
	StatusRejected  Status = "rejected"
	StatusExpired   Status = "expired" // срок публикации прошёл (только задания; переводит система)
)

// Actor — кто меняет статус (битовая маска: переход может быть разрешён нескольким).
type Actor uint8

// Владелец (бизнес или автор), модератор (будущая админка) и система (автоматическая проверка).
const (
	ActorOwner Actor = 1 << iota
	ActorModerator
	ActorSystem
)

// transitions — разрешённые переходы и кто их делает. Всё, чего здесь нет, — запрещено.
// Опубликованное владелец может снять в черновик (чтобы изменить) — затем снова на проверку.
var transitions = map[Status]map[Status]Actor{
	StatusDraft:     {StatusPending: ActorOwner},
	StatusPending:   {StatusPublished: ActorModerator | ActorSystem, StatusRejected: ActorModerator, StatusDraft: ActorOwner},
	StatusRejected:  {StatusPending: ActorOwner},
	StatusPublished: {StatusDraft: ActorOwner, StatusExpired: ActorSystem},
	StatusExpired:   {StatusDraft: ActorOwner},
}

// CanTransition — можно ли перевести из from в to.
func CanTransition(from, to Status, by Actor) bool {
	who, ok := transitions[from][to]
	return ok && by != 0 && who&by == by
}

// ModerationMode — как проверяется отправленное: вручную (ждёт модератора) или автоматически (сразу публикуется).
type ModerationMode string

// Режимы проверки (MODERATION_MODE).
const (
	ModerationAuto   ModerationMode = "auto"
	ModerationManual ModerationMode = "manual"
)

// AutoApproveNote — пометка в журнале при автоматическом одобрении.
const AutoApproveNote = "одобрено автоматически"

// Editable — владелец может менять содержимое: черновик или отклонённое (после правки — снова на проверку).
func (s Status) Editable() bool { return s == StatusDraft || s == StatusRejected }

// Subject — что модерируется (для журнала смен статуса).
type Subject string

// Задание бизнеса и анкета автора.
const (
	SubjectTask     Subject = "task"
	SubjectCard     Subject = "creator_card"
	SubjectCategory Subject = "category"
)

// Виды динамических блоков: абзацы текста, пункты списка, ссылки.
const (
	BlockText  = "text"
	BlockList  = "list"
	BlockLinks = "links"
)

// Block — динамический блок: заголовок и пункты (абзацы, пункты списка или ссылки — по виду).
type Block struct {
	Kind  string
	Title string
	Items []string
}

// Доступ к заданию: любой автор берёт сразу или после одобрения бизнесом.
const (
	AccessOpen     = "open"
	AccessApproval = "approval"
)

// Task — задание бизнеса.
type Task struct {
	ID             int64
	OwnerID        int64
	Status         Status
	RejectReason   string
	Title          string
	City           string
	Nationwide     bool     // «Вся Россия» — город не важен
	Categories     []string // slug сфер по порядку выбора, до 5; пусто — не выбраны (черновик)
	Formats        []string // форматы: slug из списка (FormatReels…) или своё название, до 3
	Access         string
	MinViews       int64      // условие выполнения — просмотры
	RewardKopecks  int64      // оплата за выполнение
	PublishBy      *time.Time // срок публикации (дата, 00:00 UTC); nil — не задан
	PublishTime    string     // время срока по Москве «ЧЧ:ММ»; "" — до конца дня
	DeadlineAt     *time.Time // срок целиком в UTC (дата + время или 23:59:59 по Москве)
	Blocks         []Block
	Cover          string   // media.id обложки (16:9); "" — нет
	Photos         []string // media.id примеров «что показать», по порядку
	CategoryTitles []string // названия сфер по порядку (только в ленте и на странице задания)
	Brand          string   // название бизнеса (только в ленте)
	CreatedAt      time.Time
	UpdatedAt      time.Time
	SubmittedAt    *time.Time
	PublishedAt    *time.Time
}

// Platform — площадка автора: вид (по адресу), ссылка, подписчики.
type Platform struct {
	Kind      string
	URL       string
	Followers int64
}

// CreatorCard — анкета автора (одна на пользователя).
type CreatorCard struct {
	UserID       int64
	Status       Status
	RejectReason string
	Name         string
	City         string
	Topics       []string // slug тем
	Platforms    []Platform
	Blocks       []Block
	Avatar       string   // media.id аватара (1:1); "" — нет
	Works        []string // media.id примеров работ, по порядку
	TopicTitles  []string // названия тем по порядку (только в ленте и на странице анкеты)
	CreatedAt    time.Time
	UpdatedAt    time.Time
	SubmittedAt  *time.Time
	PublishedAt  *time.Time
}

// StatusChange — запись журнала смен статуса.
type StatusChange struct {
	Subject   Subject
	SubjectID int64
	From      Status
	To        Status
	Reason    string // причина отказа (или AutoApproveNote в журнале)
	ActorID   int64  // кто сменил; 0 — система
	Auto      bool   // автоматическое решение (MODERATION_MODE=auto)
	At        time.Time
}

// SubmitChain — смены статуса при отправке на проверку: «→ на проверке», а в автоматическом режиме сразу за ней
// «→ опубликовано» от системы. Применяются атомарно одной транзакцией.
func SubmitChain(mode ModerationMode, subject Subject, id int64, from Status, ownerID int64) []StatusChange {
	chain := []StatusChange{{Subject: subject, SubjectID: id, From: from, To: StatusPending, ActorID: ownerID}}
	if mode == ModerationAuto {
		chain = append(chain, StatusChange{Subject: subject, SubjectID: id, From: StatusPending, To: StatusPublished, Reason: AutoApproveNote, Auto: true})
	}
	return chain
}

// FeedCursor — место в ленте: последнее задание (анкета — user_id) прошлой страницы.
type FeedCursor struct {
	PublishedAt time.Time
	ID          int64
}

// FeedFilter — отбор в ленте; пустое поле — без отбора. Formats — только для заданий.
type FeedFilter struct {
	Categories []string // любая из сфер задания или тем анкеты (slug)
	City       string   // город; задания «Вся Россия» подходят любому
	Formats    []string // любой из форматов задания (slug из списка)
}

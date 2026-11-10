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
)

// Actor — кто меняет статус.
type Actor int

// Владелец (бизнес или автор) и модератор (будущая админка).
const (
	ActorOwner Actor = iota + 1
	ActorModerator
)

// transitions — разрешённые переходы и кто их делает. Всё, чего здесь нет, — запрещено.
var transitions = map[Status]map[Status]Actor{
	StatusDraft:    {StatusPending: ActorOwner},
	StatusPending:  {StatusPublished: ActorModerator, StatusRejected: ActorModerator, StatusDraft: ActorOwner},
	StatusRejected: {StatusPending: ActorOwner},
}

// CanTransition — можно ли перевести из from в to.
func CanTransition(from, to Status, by Actor) bool {
	who, ok := transitions[from][to]
	return ok && who == by
}

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
	ID            int64
	OwnerID       int64
	Status        Status
	RejectReason  string
	Title         string
	City          string
	Nationwide    bool   // «Вся Россия» — город не важен
	Category      string // slug темы; "" — не выбрана (черновик)
	Formats       []string
	Access        string
	MinViews      int64
	MinLikes      int64
	RewardKopecks int64      // оплата за выполнение
	PublishBy     *time.Time // срок публикации (дата, 00:00 UTC); nil — не задан
	Blocks        []Block
	Cover         string   // media.id обложки (16:9); "" — нет
	Photos        []string // media.id примеров «что показать», по порядку
	CategoryTitle string   // название сферы (в том числе своей, ещё на проверке)
	Brand         string   // название бизнеса (только в ленте)
	CreatedAt     time.Time
	UpdatedAt     time.Time
	SubmittedAt   *time.Time
	PublishedAt   *time.Time
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
	Reason    string // причина отказа
	ActorID   int64  // кто сменил
	At        time.Time
}

// FeedCursor — место в ленте: последнее задание прошлой страницы.
type FeedCursor struct {
	PublishedAt time.Time
	ID          int64
}

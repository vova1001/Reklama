package service

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"ohvat/backend/internal/domain"
)

// memRepo — хранилище в памяти: пользователи, способы входа, коды, сессии (фейк UserRepository,
// CodeRepository, SessionRepository). fail — ошибка по имени метода.
type memRepo struct {
	mu       sync.Mutex
	users    map[int64]*domain.User
	ids      map[string]int64 // provider:subject → user
	provs    map[int64][]string
	codes    []*memCode
	sessions map[string]memSession
	cats     map[string]bool
	nextID   int64
	fail     map[string]error
}

type memCode struct {
	domain.LoginCode
	ip       string
	consumed bool
}

type memSession struct {
	user int64
	exp  time.Time
}

func newMem() *memRepo {
	return &memRepo{
		users: map[int64]*domain.User{}, ids: map[string]int64{}, provs: map[int64][]string{},
		sessions: map[string]memSession{}, cats: map[string]bool{"food": true, "beauty": true, "sport": true},
		fail: map[string]error{},
	}
}

func (m *memRepo) err(name string) error { return m.fail[name] }

func (m *memRepo) UserByIdentity(_ context.Context, p, s string) (domain.User, error) {
	if err := m.err("UserByIdentity"); err != nil {
		return domain.User{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.ids[p+":"+s]; ok {
		return *m.users[id], nil
	}
	return domain.User{}, domain.ErrNotFound
}

func (m *memRepo) UserByEmail(_ context.Context, e string) (domain.User, error) {
	if err := m.err("UserByEmail"); err != nil {
		return domain.User{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Email == e {
			return *u, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (m *memRepo) CreateUserWithIdentity(_ context.Context, nu domain.NewUser, id domain.Identity) (domain.User, error) {
	if err := m.err("CreateUserWithIdentity"); err != nil {
		return domain.User{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.ids[id.Provider+":"+id.Subject]; ok {
		return domain.User{}, domain.ErrConflict
	}
	m.nextID++
	u := &domain.User{ID: m.nextID, Role: nu.Role, Name: nu.Name, Email: nu.Email, AvatarURL: nu.AvatarURL}
	m.users[u.ID] = u
	m.ids[id.Provider+":"+id.Subject] = u.ID
	m.provs[u.ID] = append(m.provs[u.ID], id.Provider)
	return *u, nil
}

func (m *memRepo) LinkIdentity(_ context.Context, uid int64, id domain.Identity) error {
	if err := m.err("LinkIdentity"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ids[id.Provider+":"+id.Subject] = uid
	m.provs[uid] = append(m.provs[uid], id.Provider)
	if m.users[uid].Email == "" {
		m.users[uid].Email = id.Email
	}
	return nil
}

func (m *memRepo) TouchLogin(_ context.Context, uid int64, _ domain.Identity, role string) error {
	if err := m.err("TouchLogin"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.users[uid].Role == "" {
		m.users[uid].Role = role
	}
	return nil
}

func (m *memRepo) Providers(_ context.Context, uid int64) ([]string, error) {
	if err := m.err("Providers"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := append([]string(nil), m.provs[uid]...)
	sort.Strings(p)
	return p, nil
}

func (m *memRepo) UpdateProfile(_ context.Context, uid int64, p domain.Profile) (domain.User, error) {
	if err := m.err("UpdateProfile"); err != nil {
		return domain.User{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.users[uid]
	u.Role, u.Name, u.City, u.Spheres, u.Channels = p.Role, p.Name, p.City, p.Spheres, p.Channels
	if u.OnboardedAt == nil {
		t := time.Now()
		u.OnboardedAt = &t
	}
	return *u, nil
}

func (m *memRepo) CountCategories(_ context.Context, slugs []string) (int, error) {
	if err := m.err("CountCategories"); err != nil {
		return 0, err
	}
	n := 0
	for _, s := range slugs {
		if m.cats[s] {
			n++
		}
	}
	return n, nil
}

func (m *memRepo) stats(match func(*memCode) bool, since time.Time) domain.CodeStats {
	var st domain.CodeStats
	for _, c := range m.codes {
		if !match(c) || !c.CreatedAt.After(since) {
			continue
		}
		st.Sent++
		if st.FirstAt.IsZero() || c.CreatedAt.Before(st.FirstAt) {
			st.FirstAt = c.CreatedAt
		}
		if c.CreatedAt.After(st.LastAt) {
			st.LastAt = c.CreatedAt
		}
	}
	return st
}

func (m *memRepo) CodeStatsByEmail(_ context.Context, e string, since time.Time) (domain.CodeStats, error) {
	if err := m.err("CodeStatsByEmail"); err != nil {
		return domain.CodeStats{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats(func(c *memCode) bool { return c.Email == e }, since), nil
}

func (m *memRepo) CodeStatsByIP(_ context.Context, ip string, since time.Time) (domain.CodeStats, error) {
	if err := m.err("CodeStatsByIP"); err != nil {
		return domain.CodeStats{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats(func(c *memCode) bool { return c.ip == ip }, since), nil
}

func (m *memRepo) CreateCode(_ context.Context, c domain.NewLoginCode) error {
	if err := m.err("CreateCode"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, old := range m.codes {
		if old.Email == c.Email {
			old.consumed = true
		}
	}
	m.codes = append(m.codes, &memCode{LoginCode: domain.LoginCode{
		ID: int64(len(m.codes) + 1), Email: c.Email, CodeHash: c.CodeHash, Role: c.Role, CreatedAt: c.CreatedAt, ExpiresAt: c.ExpiresAt,
	}, ip: c.IP})
	return nil
}

func (m *memRepo) ActiveCode(_ context.Context, e string, now time.Time) (domain.LoginCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.codes) - 1; i >= 0; i-- {
		c := m.codes[i]
		if c.Email == e && !c.consumed && c.ExpiresAt.After(now) {
			return c.LoginCode, nil
		}
	}
	return domain.LoginCode{}, domain.ErrNotFound
}

func (m *memRepo) IncAttempts(_ context.Context, id int64, maxN int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.codes[id-1]
	if c.Attempts >= maxN || c.consumed {
		return 0, domain.ErrNotFound
	}
	c.Attempts++
	return c.Attempts, nil
}

func (m *memRepo) ConsumeCode(_ context.Context, id int64, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.codes[id-1]
	if c.consumed {
		return false, nil
	}
	c.consumed = true
	return true, nil
}

func (m *memRepo) DeleteCodesBefore(_ context.Context, t time.Time) (int64, error) {
	if err := m.err("DeleteCodesBefore"); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	keep := m.codes[:0]
	for _, c := range m.codes {
		if c.CreatedAt.Before(t) {
			n++
			continue
		}
		keep = append(keep, c)
	}
	m.codes = keep
	return n, nil
}

func (m *memRepo) CreateSession(_ context.Context, uid int64, h []byte, exp time.Time) error {
	if err := m.err("CreateSession"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[string(h)] = memSession{user: uid, exp: exp}
	return nil
}

func (m *memRepo) SessionUser(_ context.Context, h []byte, now time.Time) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[string(h)]
	if !ok || !s.exp.After(now) {
		return domain.User{}, domain.ErrNotFound
	}
	return *m.users[s.user], nil
}

func (m *memRepo) DeleteSession(_ context.Context, h []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, string(h))
	return nil
}

func (m *memRepo) DeleteExpiredSessions(_ context.Context, now time.Time) (int64, error) {
	if err := m.err("DeleteExpiredSessions"); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for k, s := range m.sessions {
		if s.exp.Before(now) {
			delete(m.sessions, k)
			n++
		}
	}
	return n, nil
}

// fakeMailer запоминает отправленные коды.
type fakeMailer struct {
	mu   sync.Mutex
	sent map[string]string
	err  error
}

func (f *fakeMailer) SendLoginCode(_ context.Context, to, code string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sent == nil {
		f.sent = map[string]string{}
	}
	f.sent[to] = code
	return nil
}

func (f *fakeMailer) code(to string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent[to]
}

// fakeClock — время, которое двигает тест.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fakeGoogle — подставной Google.
type fakeGoogle struct {
	p   GoogleProfile
	err error
	got struct{ code, verifier string }
}

func (g *fakeGoogle) AuthURL(state, ch string) string {
	return "https://accounts.example/auth?state=" + state + "&code_challenge=" + ch
}

func (g *fakeGoogle) Exchange(_ context.Context, code, verifier string) (GoogleProfile, error) {
	g.got.code, g.got.verifier = code, verifier
	return g.p, g.err
}

var errBoom = errors.New("boom")

// fixedRand — предсказуемые «случайные» байты.
func fixedRand(b byte) *bytes.Reader { return bytes.NewReader(bytes.Repeat([]byte{b}, 1<<16)) }

func (m *memRepo) DeleteUserSessions(_ context.Context, uid int64) (int64, error) {
	if err := m.err("DeleteUserSessions"); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for k, s := range m.sessions {
		if s.user == uid {
			delete(m.sessions, k)
			n++
		}
	}
	return n, nil
}

func (m *memRepo) sessionCount(uid int64) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, s := range m.sessions {
		if s.user == uid {
			n++
		}
	}
	return n
}

// memTG — входы через бота, чаты и устройства в памяти (фейк TelegramLoginRepository, BotChatRepository,
// DeviceRepository). idents — привязки telegram id → пользователь (для NotifyChat).
type memTG struct {
	mu      sync.Mutex
	logins  []*memLogin
	chats   map[int64]*domain.TelegramChat
	devices map[string]bool
	idents  map[int64]int64 // пользователь сайта → tg id
	fail    map[string]error
}

type memLogin struct {
	domain.TelegramLogin
	token []byte
	ip    string
}

func newMemTG() *memTG {
	return &memTG{chats: map[int64]*domain.TelegramChat{}, devices: map[string]bool{}, idents: map[int64]int64{}, fail: map[string]error{}}
}

func (m *memTG) CreateTelegramLogin(_ context.Context, l domain.NewTelegramLogin) error {
	if err := m.fail["CreateTelegramLogin"]; err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logins = append(m.logins, &memLogin{TelegramLogin: domain.TelegramLogin{ID: int64(len(m.logins) + 1), BrowserHash: l.BrowserHash,
		Role: l.Role, CreatedAt: l.CreatedAt, ExpiresAt: l.ExpiresAt}, token: l.TokenHash, ip: l.IP})
	return nil
}

func (m *memTG) TelegramLoginStats(_ context.Context, ip string, since time.Time) (int, time.Time, error) {
	if err := m.fail["TelegramLoginStats"]; err != nil {
		return 0, time.Time{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n, first := 0, time.Time{}
	for _, l := range m.logins {
		if l.ip == ip && l.CreatedAt.After(since) {
			n++
			if first.IsZero() || l.CreatedAt.Before(first) {
				first = l.CreatedAt
			}
		}
	}
	return n, first, nil
}

func (m *memTG) find(match func(*memLogin) bool) *memLogin {
	for _, l := range m.logins {
		if match(l) {
			return l
		}
	}
	return nil
}

func (m *memTG) PendingTelegramLogin(_ context.Context, th []byte, now time.Time) (domain.TelegramLogin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.find(func(l *memLogin) bool {
		return bytes.Equal(l.token, th) && l.ConsumedAt == nil && l.ExpiresAt.After(now)
	})
	if l == nil {
		return domain.TelegramLogin{}, domain.ErrNotFound
	}
	return l.TelegramLogin, nil
}

func (m *memTG) ConfirmTelegramLogin(_ context.Context, th []byte, p domain.TelegramProfile, now, grace time.Time) (domain.TelegramLogin, error) {
	if err := m.fail["ConfirmTelegramLogin"]; err != nil {
		return domain.TelegramLogin{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.find(func(l *memLogin) bool {
		return bytes.Equal(l.token, th) && l.ConsumedAt == nil && l.ExpiresAt.After(now) && (l.TgUserID == 0 || l.TgUserID == p.ID)
	})
	if l == nil {
		return domain.TelegramLogin{}, domain.ErrNotFound
	}
	l.TgUserID, l.TgName, l.TgUsername = p.ID, p.Name(), p.Username
	if l.ConfirmedAt == nil {
		t := now
		l.ConfirmedAt = &t
	}
	if grace.After(l.ExpiresAt) {
		l.ExpiresAt = grace
	}
	return l.TelegramLogin, nil
}

func (m *memTG) CancelTelegramLogin(_ context.Context, th []byte, now time.Time) (domain.TelegramLogin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.find(func(l *memLogin) bool { return bytes.Equal(l.token, th) && l.ConsumedAt == nil && l.ConfirmedAt == nil })
	if l == nil {
		return domain.TelegramLogin{}, domain.ErrNotFound
	}
	t := now
	l.ConsumedAt, l.Cancelled = &t, true
	return l.TelegramLogin, nil
}

func (m *memTG) TelegramLoginByBrowser(_ context.Context, bh []byte) (domain.TelegramLogin, error) {
	if err := m.fail["TelegramLoginByBrowser"]; err != nil {
		return domain.TelegramLogin{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.find(func(l *memLogin) bool { return bytes.Equal(l.BrowserHash, bh) })
	if l == nil {
		return domain.TelegramLogin{}, domain.ErrNotFound
	}
	return l.TelegramLogin, nil
}

func (m *memTG) ConsumeTelegramLogin(_ context.Context, bh []byte, now time.Time) (domain.TelegramLogin, error) {
	if err := m.fail["ConsumeTelegramLogin"]; err != nil {
		return domain.TelegramLogin{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.find(func(l *memLogin) bool {
		return bytes.Equal(l.BrowserHash, bh) && l.ConfirmedAt != nil && l.ConsumedAt == nil && l.ExpiresAt.After(now)
	})
	if l == nil {
		return domain.TelegramLogin{}, domain.ErrNotFound
	}
	t := now
	l.ConsumedAt = &t
	return l.TelegramLogin, nil
}

func (m *memTG) DeleteTelegramLoginsBefore(_ context.Context, t time.Time) (int64, error) {
	if err := m.fail["DeleteTelegramLoginsBefore"]; err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	keep := m.logins[:0]
	for _, l := range m.logins {
		if l.CreatedAt.Before(t) {
			n++
			continue
		}
		keep = append(keep, l)
	}
	m.logins = keep
	return n, nil
}

func (m *memTG) SaveChat(_ context.Context, tg, chat int64, username string) (domain.TelegramChat, error) {
	if err := m.fail["SaveChat"]; err != nil {
		return domain.TelegramChat{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.chats[tg]
	if !ok {
		c = &domain.TelegramChat{TgUserID: tg, Notify: true}
		m.chats[tg] = c
	}
	c.ChatID, c.Username, c.Blocked = chat, username, false
	return *c, nil
}

func (m *memTG) SetNotify(_ context.Context, tg int64, on bool) error {
	if err := m.fail["SetNotify"]; err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.chats[tg]
	if !ok {
		return domain.ErrNotFound
	}
	c.Notify = on
	return nil
}

func (m *memTG) BlockChat(_ context.Context, chat int64) error {
	if err := m.fail["BlockChat"]; err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.chats {
		if c.ChatID == chat {
			c.Blocked = true
		}
	}
	return nil
}

func (m *memTG) Chat(_ context.Context, tg int64) (domain.TelegramChat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.chats[tg]; ok {
		return *c, nil
	}
	return domain.TelegramChat{}, domain.ErrNotFound
}

func (m *memTG) NotifyChat(_ context.Context, uid int64) (domain.TelegramChat, error) {
	if err := m.fail["NotifyChat"]; err != nil {
		return domain.TelegramChat{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.chats[m.idents[uid]]; ok && c.Notify && !c.Blocked {
		return *c, nil
	}
	return domain.TelegramChat{}, domain.ErrNotFound
}

func (m *memTG) TouchDevice(_ context.Context, uid int64, h []byte) (bool, error) {
	if err := m.fail["TouchDevice"]; err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	k := strconv.FormatInt(uid, 10) + ":" + string(h)
	if m.devices[k] {
		return false, nil
	}
	m.devices[k] = true
	return true, nil
}

func (m *memTG) chat(tg int64) domain.TelegramChat {
	c, _ := m.Chat(context.Background(), tg)
	return c
}

// fakeMessenger — исходящие сообщения бота.
type fakeMessenger struct {
	mu      sync.Mutex
	sent    []BotMessage
	answers []string
	err     error
}

func (f *fakeMessenger) Send(_ context.Context, m BotMessage) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeMessenger) AnswerCallback(_ context.Context, _, text string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = append(f.answers, text)
	return nil
}

func (f *fakeMessenger) last() BotMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return BotMessage{}
	}
	return f.sent[len(f.sent)-1]
}

func (f *fakeMessenger) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// fakeNotifier — запоминает уведомления.
type fakeNotifier struct {
	mu  sync.Mutex
	got []Notification
	err error
}

func (f *fakeNotifier) Notify(_ context.Context, _ int64, n Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, n)
	return f.err
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

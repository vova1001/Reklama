package service

import (
	"bytes"
	"context"
	"errors"
	"sort"
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

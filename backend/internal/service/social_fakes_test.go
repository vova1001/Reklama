package service

import (
	"context"
	"encoding/base64"
	"sort"
	"strconv"
	"sync"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/secretbox"
)

// memSocial — SocialRepository в памяти.
type memSocial struct {
	mu       sync.Mutex
	accs     map[int64]*domain.SocialAccount
	creds    map[int64]*domain.SocialCredential
	locks    map[int64]time.Time
	states   map[string]domain.SocialState
	chans    map[int64]domain.TelegramChannel
	tg       map[int64]int64 // user → tg id
	history  map[int64]int
	seq      int64
	failNext error
}

func newMemSocial() *memSocial {
	return &memSocial{accs: map[int64]*domain.SocialAccount{}, creds: map[int64]*domain.SocialCredential{}, locks: map[int64]time.Time{},
		states: map[string]domain.SocialState{}, chans: map[int64]domain.TelegramChannel{}, tg: map[int64]int64{}, history: map[int64]int{}}
}

func (m *memSocial) list(match func(*domain.SocialAccount) bool) []domain.SocialAccount {
	out := []domain.SocialAccount{}
	for _, a := range m.accs {
		if match(a) {
			out = append(out, *a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *memSocial) Accounts(_ context.Context, uid int64) ([]domain.SocialAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.list(func(a *domain.SocialAccount) bool { return a.UserID == uid }), nil
}

func (m *memSocial) VerifiedAccounts(_ context.Context, ids []int64) ([]domain.SocialAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	set := map[int64]bool{}
	for _, id := range ids {
		set[id] = true
	}
	return m.list(func(a *domain.SocialAccount) bool { return set[a.UserID] && a.Status == domain.SocialVerified }), nil
}

func (m *memSocial) Account(_ context.Context, uid, id int64) (domain.SocialAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accs[id]
	if !ok || a.UserID != uid {
		return domain.SocialAccount{}, domain.ErrNotFound
	}
	return *a, nil
}

func (m *memSocial) CountAccounts(_ context.Context, uid int64) (int, error) {
	a, _ := m.Accounts(context.Background(), uid)
	return len(a), nil
}

func (m *memSocial) SaveAccount(_ context.Context, a domain.SocialAccount, _ time.Time) (domain.SocialAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failNext != nil {
		err := m.failNext
		m.failNext = nil
		return domain.SocialAccount{}, err
	}
	for _, x := range m.accs {
		if x.UserID == a.UserID && x.Platform == a.Platform && x.ExternalID == a.ExternalID {
			a.ID, a.CreatedAt = x.ID, x.CreatedAt
			if a.VerifiedAt == nil {
				a.VerifiedAt = x.VerifiedAt
			}
			*x = a
			if a.Status == domain.SocialVerified {
				m.history[a.ID]++
			}
			return a, nil
		}
	}
	m.seq++
	a.ID = m.seq
	cp := a
	m.accs[a.ID] = &cp
	if a.Status == domain.SocialVerified {
		m.history[a.ID]++
	}
	return a, nil
}

func (m *memSocial) SaveStats(_ context.Context, upd domain.SocialAccount, at, next time.Time) (domain.SocialAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accs[upd.ID]
	if !ok {
		return domain.SocialAccount{}, domain.ErrNotFound
	}
	a.Handle, a.Title, a.URL, a.Stats = upd.Handle, upd.Title, upd.URL, upd.Stats
	a.Status, a.FailReason, a.Failures = domain.SocialVerified, "", 0
	a.RefreshedAt, a.NextRefreshAt = &at, &next
	if a.VerifiedAt == nil {
		a.VerifiedAt = &at
	}
	m.history[a.ID]++
	return *a, nil
}

func (m *memSocial) SetRetry(_ context.Context, id int64, next time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.accs[id]; ok {
		a.Failures++
		a.NextRefreshAt = &next
	}
	return nil
}

func (m *memSocial) SetFailed(_ context.Context, id int64, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.accs[id]; ok {
		a.Status, a.FailReason, a.NextRefreshAt = domain.SocialFailed, reason, nil
	}
	return nil
}

func (m *memSocial) ClaimDue(_ context.Context, now, lease time.Time, limit int) ([]domain.SocialAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	due := m.list(func(a *domain.SocialAccount) bool { return a.NextRefreshAt != nil && !a.NextRefreshAt.After(now) })
	if len(due) > limit {
		due = due[:limit]
	}
	for _, a := range due {
		l := lease
		m.accs[a.ID].NextRefreshAt = &l
	}
	return due, nil
}

func (m *memSocial) DeleteAccount(_ context.Context, uid, id int64) (domain.SocialAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accs[id]
	if !ok || a.UserID != uid {
		return domain.SocialAccount{}, domain.ErrNotFound
	}
	delete(m.accs, id)
	return *a, nil
}

func (m *memSocial) AccountsByExternal(_ context.Context, p domain.SocialPlatform, ext string) ([]domain.SocialAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.list(func(a *domain.SocialAccount) bool { return a.Platform == p && a.ExternalID == ext }), nil
}

func (m *memSocial) SaveCredential(_ context.Context, c domain.SocialCredential) (domain.SocialCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.creds {
		if x.UserID == c.UserID && x.Platform == c.Platform && x.Subject == c.Subject {
			c.ID = x.ID
			*x = c
			delete(m.locks, c.ID)
			return c, nil
		}
	}
	m.seq++
	c.ID = m.seq
	cp := c
	m.creds[c.ID] = &cp
	return c, nil
}

func (m *memSocial) LockCredential(_ context.Context, id int64, now, until time.Time) (domain.SocialCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.creds[id]
	if !ok {
		return domain.SocialCredential{}, domain.ErrNotFound
	}
	if l, ok := m.locks[id]; ok && l.After(now) {
		return domain.SocialCredential{}, domain.ErrConflict
	}
	m.locks[id] = until
	return *c, nil
}

func (m *memSocial) UpdateCredential(_ context.Context, c domain.SocialCredential) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	x, ok := m.creds[c.ID]
	if !ok {
		return domain.ErrNotFound
	}
	x.Secret, x.AccessExpiresAt, x.RefreshExpiresAt = c.Secret, c.AccessExpiresAt, c.RefreshExpiresAt
	delete(m.locks, c.ID)
	return nil
}

func (m *memSocial) UnlockCredential(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.locks, id)
	return nil
}

func (m *memSocial) Credential(_ context.Context, id int64) (domain.SocialCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.creds[id]
	if !ok {
		return domain.SocialCredential{}, domain.ErrNotFound
	}
	return *c, nil
}

func (m *memSocial) CredentialAccounts(_ context.Context, id int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.list(func(a *domain.SocialAccount) bool { return a.CredentialID == id })), nil
}

func (m *memSocial) DeleteCredential(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.creds, id)
	return nil
}

func (m *memSocial) CreateState(_ context.Context, h []byte, s domain.SocialState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[string(h)] = s
	return nil
}

func (m *memSocial) StateStats(_ context.Context, uid int64, since time.Time) (int, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, first := 0, time.Time{}
	for _, s := range m.states {
		if s.UserID == uid && s.CreatedAt.After(since) {
			n++
			if first.IsZero() || s.CreatedAt.Before(first) {
				first = s.CreatedAt
			}
		}
	}
	return n, first, nil
}

func (m *memSocial) PeekState(_ context.Context, h []byte, p domain.SocialPlatform, now time.Time) (domain.SocialState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.states[string(h)]
	if !ok || s.Platform != p || !s.ExpiresAt.After(now) {
		return domain.SocialState{}, domain.ErrNotFound
	}
	return s, nil
}

func (m *memSocial) TakeState(ctx context.Context, h []byte, p domain.SocialPlatform, now time.Time) (domain.SocialState, error) {
	s, err := m.PeekState(ctx, h, p, now)
	if err == nil {
		m.mu.Lock()
		delete(m.states, string(h))
		m.mu.Unlock()
	}
	return s, err
}

func (m *memSocial) DeleteStatesBefore(_ context.Context, t time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for k, s := range m.states {
		if s.ExpiresAt.Before(t) {
			delete(m.states, k)
			n++
		}
	}
	return n, nil
}

func (m *memSocial) SaveTelegramChannel(_ context.Context, ch domain.TelegramChannel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chans[ch.ChatID] = ch
	return nil
}

func (m *memSocial) TelegramChannelsAddedBy(_ context.Context, tg int64) ([]domain.TelegramChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.TelegramChannel
	for _, c := range m.chans {
		if c.AddedBy == tg && (c.BotStatus == "administrator" || c.BotStatus == "member") {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memSocial) UserTelegramID(_ context.Context, uid int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.tg[uid]; ok {
		return id, nil
	}
	return 0, domain.ErrNotFound
}

// socialUsers — пользователи и привязка Telegram (пишет в memSocial.tg).
type socialUsers struct {
	repo  *memSocial
	users map[int64]domain.User
}

func (u *socialUsers) UserByIdentity(_ context.Context, provider, subject string) (domain.User, error) {
	u.repo.mu.Lock()
	defer u.repo.mu.Unlock()
	for uid, tg := range u.repo.tg {
		if provider == domain.ProviderTelegram && strconv.FormatInt(tg, 10) == subject {
			return u.users[uid], nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (u *socialUsers) LinkIdentity(_ context.Context, uid int64, id domain.Identity) error {
	n, _ := strconv.ParseInt(id.Subject, 10, 64)
	u.repo.mu.Lock()
	defer u.repo.mu.Unlock()
	u.repo.tg[uid] = n
	return nil
}

func (u *socialUsers) UserByID(_ context.Context, id int64) (domain.User, error) {
	if x, ok := u.users[id]; ok {
		return x, nil
	}
	return domain.User{}, domain.ErrNotFound
}

// fakeVerifier — площадка: цифры и ошибки задаёт тест.
type fakeVerifier struct {
	mu        sync.Mutex
	platform  domain.SocialPlatform
	method    domain.SocialMethod
	checkErr  error
	refErr    error
	renewErr  error
	channels  []VerifiedChannel
	followers int64
	renewed   int
	revoked   []Tokens
	checks    []VerifyCheck
	refreshes []RefreshInput
}

func (f *fakeVerifier) Platform() domain.SocialPlatform { return f.platform }
func (f *fakeVerifier) Method() domain.SocialMethod     { return f.method }

func (f *fakeVerifier) Start(_ context.Context, in VerifyStart) (VerifyStep, error) {
	return VerifyStep{URL: "https://example.test/auth?state=" + in.State + "&challenge=" + in.Challenge + "&redirect=" + in.RedirectURL}, nil
}

func (f *fakeVerifier) Check(_ context.Context, in VerifyCheck) (VerifyResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, in)
	if f.checkErr != nil {
		return VerifyResult{}, f.checkErr
	}
	r := VerifyResult{Subject: "subj-1", Channels: f.channels}
	if f.method == domain.MethodOAuth {
		r.Tokens = &Tokens{Access: "access-1", Refresh: "refresh-1", AccessExpiresAt: time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)}
	}
	return r, nil
}

func (f *fakeVerifier) Refresh(_ context.Context, in RefreshInput) (VerifiedChannel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes = append(f.refreshes, in)
	if f.refErr != nil {
		return VerifiedChannel{}, f.refErr
	}
	n := f.followers
	return VerifiedChannel{ExternalID: in.Account.ExternalID, Kind: in.Account.Kind, Title: "Свежее", Stats: domain.SocialStats{Followers: &n}}, nil
}

func (f *fakeVerifier) Renew(_ context.Context, t Tokens, now time.Time) (Tokens, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.renewErr != nil {
		return t, false, f.renewErr
	}
	if t.AccessExpiresAt.After(now.Add(2 * time.Minute)) {
		return t, false, nil
	}
	f.renewed++
	t.Access = "access-" + strconv.Itoa(f.renewed+1)
	t.AccessExpiresAt = now.Add(time.Hour)
	return t, true, nil
}

func (f *fakeVerifier) Revoke(_ context.Context, t Tokens) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, t)
	return nil
}

func testBox() *secretbox.Box {
	b, err := secretbox.New(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		panic(err)
	}
	return b
}

func ptr(n int64) *int64 { return &n }

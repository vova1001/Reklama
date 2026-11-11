package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
)

type socialEnv struct {
	repo  *memSocial
	users *socialUsers
	clock *fakeClock
	yt    *fakeVerifier
	tg    *fakeVerifier
	svc   *Social
}

var creator = domain.User{ID: 8, Name: "Алина", ActiveMode: domain.ModeCreator}

func newSocialEnv(t *testing.T, mut ...func(*SocialDeps, *SocialConfig)) *socialEnv {
	t.Helper()
	e := &socialEnv{repo: newMemSocial(), clock: &fakeClock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}}
	e.users = &socialUsers{repo: e.repo, users: map[int64]domain.User{creator.ID: creator, 9: {ID: 9, Name: "Другой"}}}
	e.yt = &fakeVerifier{platform: domain.PlatformYouTube, method: domain.MethodOAuth, followers: 15000,
		channels: []VerifiedChannel{{ExternalID: "UC1", Kind: domain.SocialChannel, Title: "Алина", Handle: "alina", URL: "https://www.youtube.com/@alina",
			Stats: domain.SocialStats{Followers: ptr(12000), Views: ptr(900000), Videos: ptr(40)}}}}
	e.tg = &fakeVerifier{platform: domain.PlatformTelegram, method: domain.MethodBot, followers: 2500,
		channels: []VerifiedChannel{{ExternalID: "-1001", Kind: domain.SocialChannel, Title: "Канал Алины", Handle: "alina_ch", URL: "https://t.me/alina_ch",
			Stats: domain.SocialStats{Followers: ptr(2000)}}}}
	d := SocialDeps{Repo: e.repo, Users: e.users, Cipher: testBox(), Verifiers: []PlatformVerifier{e.tg, e.yt}, Clock: e.clock,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	cfg := SocialConfig{SiteURL: "https://ohvat.test", PlatformGap: -1}
	for _, m := range mut {
		m(&d, &cfg)
	}
	e.svc = NewSocial(d, cfg)
	return e
}

// connect — подключение YouTube целиком: Start → Callback с тем же state.
func (e *socialEnv) connect(t *testing.T) []domain.SocialAccount {
	t.Helper()
	st, err := e.svc.Start(context.Background(), creator, domain.PlatformYouTube)
	if err != nil {
		t.Fatal(err)
	}
	accs, err := e.svc.Callback(context.Background(), creator, domain.PlatformYouTube, SocialCallback{State: st.State, Code: "code-1"})
	if err != nil {
		t.Fatal(err)
	}
	return accs
}

func TestSocialOverview(t *testing.T) {
	e := newSocialEnv(t)
	vs, err := e.svc.Overview(context.Background(), creator)
	if err != nil || len(vs) != 6 {
		t.Fatalf("%v %d", err, len(vs))
	}
	want := map[domain.SocialPlatform]bool{domain.PlatformTelegram: true, domain.PlatformYouTube: true}
	for _, v := range vs {
		if v.Available != want[v.Platform] || v.Status != domain.SocialUnverified || v.Accounts == nil {
			t.Errorf("%s: %+v", v.Platform, v)
		}
	}
	if vs[0].Platform != domain.PlatformTelegram || vs[0].Method != domain.MethodBot || vs[2].Method != domain.MethodOAuth {
		t.Errorf("порядок и способ: %+v", vs)
	}
	e.connect(t)
	vs, _ = e.svc.Overview(context.Background(), creator)
	if vs[1].Status != domain.SocialVerified || len(vs[1].Accounts) != 1 {
		t.Errorf("YouTube: %+v", vs[1])
	}
	for _, c := range []struct {
		accs []domain.SocialAccount
		want domain.SocialStatus
	}{
		{nil, domain.SocialUnverified},
		{[]domain.SocialAccount{{Status: domain.SocialFailed}}, domain.SocialFailed},
		{[]domain.SocialAccount{{Status: domain.SocialFailed}, {Status: domain.SocialPending}}, domain.SocialPending},
		{[]domain.SocialAccount{{Status: domain.SocialFailed}, {Status: domain.SocialVerified}}, domain.SocialVerified},
	} {
		if got := platformStatus(c.accs); got != c.want {
			t.Errorf("%+v: %s", c.accs, got)
		}
	}
}

func TestSocialOAuthWithoutKeyDisabled(t *testing.T) {
	e := newSocialEnv(t, func(d *SocialDeps, _ *SocialConfig) { d.Cipher = nil })
	if e.svc.Available(domain.PlatformYouTube) || !e.svc.Available(domain.PlatformTelegram) {
		t.Fatal("без ключа шифрования OAuth-площадки выключены, Telegram — нет")
	}
	if _, err := e.svc.Start(context.Background(), creator, domain.PlatformYouTube); !errors.Is(err, ErrSocialDisabled) {
		t.Errorf("%v", err)
	}
	if _, err := e.svc.Start(context.Background(), creator, domain.PlatformVK); !errors.Is(err, ErrSocialDisabled) {
		t.Errorf("VK — «Скоро»: %v", err)
	}
}

func TestSocialStartAndCallback(t *testing.T) {
	e := newSocialEnv(t)
	ctx := context.Background()
	st, err := e.svc.Start(ctx, creator, domain.PlatformYouTube)
	if err != nil || st.State == "" {
		t.Fatalf("%v %+v", err, st)
	}
	u, _ := url.Parse(st.URL)
	if u.Query().Get("state") != st.State || len(u.Query().Get("challenge")) != 43 ||
		u.Query().Get("redirect") != "https://ohvat.test/api/social/youtube/callback" {
		t.Fatalf("адрес: %s", st.URL)
	}
	// verifier хранится только зашифрованно
	for _, s := range e.repo.states {
		if len(s.Verifier) == 0 || bytes.Contains(s.Verifier, []byte(u.Query().Get("challenge"))) {
			t.Fatal("verifier не зашифрован")
		}
	}
	// чужой пользователь с тем же state — нельзя (и state сгорает)
	other := domain.User{ID: 9}
	if _, err := e.svc.Callback(ctx, other, domain.PlatformYouTube, SocialCallback{State: st.State, Code: "c"}); !errors.Is(err, ErrSocialState) {
		t.Fatalf("чужой: %v", err)
	}
	if _, err := e.svc.Callback(ctx, creator, domain.PlatformYouTube, SocialCallback{State: st.State, Code: "c"}); !errors.Is(err, ErrSocialState) {
		t.Fatalf("state одноразовый: %v", err)
	}
	// отказ автора
	st, _ = e.svc.Start(ctx, creator, domain.PlatformYouTube)
	if _, err := e.svc.Callback(ctx, creator, domain.PlatformYouTube, SocialCallback{State: st.State, Error: "access_denied"}); !errors.Is(err, ErrSocialDenied) {
		t.Fatalf("отказ: %v", err)
	}
	// истёк
	st, _ = e.svc.Start(ctx, creator, domain.PlatformYouTube)
	e.clock.add(11 * time.Minute)
	if _, err := e.svc.Callback(ctx, creator, domain.PlatformYouTube, SocialCallback{State: st.State, Code: "c"}); !errors.Is(err, ErrSocialState) {
		t.Fatalf("истёк: %v", err)
	}
	// пустые и длинные
	for _, in := range []SocialCallback{{}, {State: strings.Repeat("a", 200)}} {
		if _, err := e.svc.Callback(ctx, creator, domain.PlatformYouTube, in); !errors.Is(err, ErrSocialState) {
			t.Errorf("%+v: %v", in, err)
		}
	}
	// успех: PKCE verifier дошёл до площадки, токены — только зашифрованно, цифры — от площадки
	accs := e.connect(t)
	if len(accs) != 1 || accs[0].Status != domain.SocialVerified || *accs[0].Stats.Followers != 12000 || accs[0].CredentialID == 0 ||
		accs[0].NextRefreshAt == nil || accs[0].Method != domain.MethodOAuth {
		t.Fatalf("%+v", accs)
	}
	last := e.yt.checks[len(e.yt.checks)-1]
	if len(last.Verifier) != 43 || last.Code != "code-1" || last.RedirectURL != "https://ohvat.test/api/social/youtube/callback" {
		t.Errorf("проверка: %+v", last)
	}
	next := *accs[0].NextRefreshAt
	if d := next.Sub(e.clock.Now()); d < 22*time.Hour || d > 26*time.Hour {
		t.Errorf("следующее обновление через %v", d)
	}
	cred := e.repo.creds[accs[0].CredentialID]
	if bytes.Contains(cred.Secret, []byte("access-1")) || bytes.Contains(cred.Secret, []byte("refresh-1")) {
		t.Fatal("токены в открытом виде")
	}
	tok, err := e.svc.openCredential(*cred)
	if err != nil || tok.Access != "access-1" || tok.Refresh != "refresh-1" {
		t.Fatalf("расшифровка: %v %+v", err, tok)
	}
	// чужой пользователь с этим шифротекстом — не расшифровать
	alien := *cred
	alien.UserID = 9
	if _, err := e.svc.openCredential(alien); err == nil {
		t.Fatal("шифротекст не привязан к владельцу")
	}
	// цифры в анкете — только подтверждённые
	m, err := e.svc.CardPlatforms(ctx, []int64{creator.ID, 9})
	if err != nil || len(m[creator.ID]) != 1 || m[creator.ID][0].Followers != 12000 || !m[creator.ID][0].Verified ||
		m[creator.ID][0].Kind != "youtube" || m[creator.ID][0].Type != domain.SocialChannel || len(m[9]) != 0 {
		t.Fatalf("анкета: %v %+v", err, m)
	}
	if m, _ := e.svc.CardPlatforms(ctx, nil); len(m) != 0 {
		t.Error("пусто")
	}
}

func TestSocialCallbackErrors(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]struct {
		err  error
		chs  []VerifiedChannel
		want error
	}{
		"нет канала":   {nil, []VerifiedChannel{}, ErrSocialNoChannel},
		"нет канала 2": {fmt.Errorf("%w: x", ErrSocialNoChannel), nil, ErrSocialNoChannel},
		"сбой":         {fmt.Errorf("%w: 503", ErrSocialUnavailable), nil, ErrSocialUnavailable},
		"отказ":        {fmt.Errorf("%w: scope", ErrSocialDenied), nil, ErrSocialDenied},
	} {
		e := newSocialEnv(t)
		e.yt.checkErr = c.err
		if c.chs != nil {
			e.yt.channels = c.chs
		}
		st, _ := e.svc.Start(ctx, creator, domain.PlatformYouTube)
		if _, err := e.svc.Callback(ctx, creator, domain.PlatformYouTube, SocialCallback{State: st.State, Code: "c"}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
		if len(e.repo.accs) != 0 || len(e.repo.creds) != 0 {
			t.Errorf("%s: что-то сохранилось", name)
		}
	}
	// Telegram — не через OAuth-возврат
	e := newSocialEnv(t)
	if _, err := e.svc.Callback(ctx, creator, domain.PlatformTelegram, SocialCallback{State: "x", Code: "c"}); !errors.Is(err, ErrSocialDisabled) {
		t.Errorf("telegram: %v", err)
	}
}

func TestSocialLimits(t *testing.T) {
	ctx := context.Background()
	e := newSocialEnv(t, func(_ *SocialDeps, c *SocialConfig) { c.StartsPerHour = 2; c.MaxAccounts = 1 })
	for range 2 {
		if _, err := e.svc.Start(ctx, creator, domain.PlatformTelegram); err != nil {
			t.Fatal(err)
		}
	}
	var le *LimitError
	if _, err := e.svc.Start(ctx, creator, domain.PlatformYouTube); !errors.As(err, &le) || le.Reason != "social_hourly" || le.RetryAfter <= 0 {
		t.Fatalf("лимит в час: %v", err)
	}
	e.clock.add(61 * time.Minute)
	accs := e.connect(t)
	if _, err := e.svc.Start(ctx, creator, domain.PlatformTelegram); !errors.Is(err, ErrSocialTooMany) {
		t.Errorf("аккаунтов много: %v", err)
	}
	// повторное подключение того же канала место не занимает
	e.svc.cfg.MaxAccounts = 2
	st, _ := e.svc.Start(ctx, creator, domain.PlatformYouTube)
	e.svc.cfg.MaxAccounts = 1
	again, err := e.svc.Callback(ctx, creator, domain.PlatformYouTube, SocialCallback{State: st.State, Code: "c"})
	if err != nil || len(again) != 1 || again[0].ID != accs[0].ID {
		t.Fatalf("повторно: %v %+v", err, again)
	}
	// новый канал сверх лимита — нельзя
	e.yt.channels = append(e.yt.channels, VerifiedChannel{ExternalID: "UC2", Kind: domain.SocialChannel})
	e.svc.cfg.MaxAccounts = 2
	st, _ = e.svc.Start(ctx, creator, domain.PlatformYouTube)
	e.svc.cfg.MaxAccounts = 1
	if _, err := e.svc.Callback(ctx, creator, domain.PlatformYouTube, SocialCallback{State: st.State, Code: "c"}); !errors.Is(err, ErrSocialTooMany) {
		t.Errorf("сверх лимита: %v", err)
	}
}

func TestSocialRefresh(t *testing.T) {
	ctx := context.Background()
	e := newSocialEnv(t)
	a := e.connect(t)[0]
	var le *LimitError
	if _, err := e.svc.RefreshNow(ctx, creator, a.ID); !errors.As(err, &le) || le.Reason != "social_refresh" {
		t.Fatalf("сразу после подтверждения: %v", err)
	}
	if _, err := e.svc.RefreshNow(ctx, domain.User{ID: 9}, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("чужой: %v", err)
	}
	// через 2 часа: токен истёк → обновлён (зашифрованно), цифры свежие, история пополнилась
	e.clock.add(2 * time.Hour)
	got, err := e.svc.RefreshNow(ctx, creator, a.ID)
	if err != nil || *got.Stats.Followers != 15000 || got.Title != "Свежее" || e.yt.renewed != 1 {
		t.Fatalf("%v %+v renewed=%d", err, got, e.yt.renewed)
	}
	if tok, _ := e.svc.openCredential(*e.repo.creds[a.CredentialID]); tok.Access != "access-2" || tok.Refresh != "refresh-1" {
		t.Errorf("токен после обновления: %+v", tok)
	}
	if e.yt.refreshes[0].Tokens.Access != "access-2" || e.repo.history[a.ID] != 2 || len(e.repo.locks) != 0 {
		t.Errorf("обновление: %+v history=%d locks=%v", e.yt.refreshes[0], e.repo.history[a.ID], e.repo.locks)
	}
	// временный сбой — статус прежний, повтор позже, ошибка автору
	e.clock.add(time.Hour)
	e.yt.refErr = fmt.Errorf("%w: 503", ErrSocialUnavailable)
	if _, err := e.svc.RefreshNow(ctx, creator, a.ID); !errors.Is(err, ErrSocialUnavailable) {
		t.Fatalf("сбой: %v", err)
	}
	if x := e.repo.accs[a.ID]; x.Status != domain.SocialVerified || x.Failures != 1 || !x.NextRefreshAt.After(e.clock.Now().Add(14*time.Minute)) {
		t.Errorf("после сбоя: %+v", x)
	}
	// доступ отозван — failed, причина понятная, не обновляется сам
	e.yt.refErr = fmt.Errorf("%w: 401", ErrSocialRevoked)
	got, err = e.svc.RefreshNow(ctx, creator, a.ID)
	if err != nil || got.Status != domain.SocialFailed || got.FailReason != "Доступ отозван — подключите заново" || got.NextRefreshAt != nil {
		t.Fatalf("отозван: %v %+v", err, got)
	}
	if m, _ := e.svc.CardPlatforms(ctx, []int64{creator.ID}); len(m[creator.ID]) != 0 {
		t.Error("потерявший доступ не в анкете")
	}
	// обновление токена отклонено площадкой — failed
	e.yt.refErr = nil
	e.repo.accs[a.ID].Status = domain.SocialVerified
	e.clock.add(3 * time.Hour)
	e.yt.renewErr = fmt.Errorf("%w: invalid_grant", ErrSocialRevoked)
	if got, _ = e.svc.RefreshNow(ctx, creator, a.ID); got.Status != domain.SocialFailed {
		t.Errorf("renew отклонён: %+v", got)
	}
	if len(e.repo.locks) != 0 {
		t.Error("аренда не снята")
	}
}

func TestSocialCredentialLock(t *testing.T) {
	ctx := context.Background()
	e := newSocialEnv(t)
	a := e.connect(t)[0]
	e.clock.add(2 * time.Hour)
	e.repo.locks[a.CredentialID] = e.clock.Now().Add(time.Minute) // другой процесс обновляет токен
	if err := e.svc.refresh(ctx, *e.repo.accs[a.ID]); err != nil {
		t.Fatal(err)
	}
	if e.yt.renewed != 0 || !e.repo.accs[a.ID].NextRefreshAt.Equal(e.clock.Now().Add(5*time.Minute)) {
		t.Errorf("занято — позже: %+v", e.repo.accs[a.ID])
	}
	// доступ удалён — failed
	delete(e.repo.creds, a.CredentialID)
	_ = e.svc.refresh(ctx, *e.repo.accs[a.ID])
	if e.repo.accs[a.ID].Status != domain.SocialFailed {
		t.Error("без доступа — failed")
	}
}

func TestSocialRefresher(t *testing.T) {
	e := newSocialEnv(t)
	a := e.connect(t)[0]
	e.repo.tg[creator.ID] = 777
	tgA, _ := e.repo.SaveAccount(context.Background(), domain.SocialAccount{UserID: creator.ID, Platform: domain.PlatformTelegram, ExternalID: "-1001",
		Kind: domain.SocialChannel, Status: domain.SocialPending, Method: domain.MethodBot, NextRefreshAt: ptrTime(e.clock.Now())}, e.clock.Now())
	// выключенная площадка — повтор позже, цифры не трогаем
	off, _ := e.repo.SaveAccount(context.Background(), domain.SocialAccount{UserID: creator.ID, Platform: domain.PlatformVK, ExternalID: "id1",
		Kind: domain.SocialProfile, Status: domain.SocialVerified, Method: domain.MethodOAuth, NextRefreshAt: ptrTime(e.clock.Now())}, e.clock.Now())
	e.clock.add(25 * time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.svc.RunRefresher(ctx, time.Hour); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		e.repo.mu.Lock()
		ok := e.repo.accs[a.ID].RefreshedAt.After(e.clock.Now().Add(-time.Minute)) && e.repo.accs[tgA.ID].Status == domain.SocialVerified &&
			e.repo.accs[off.ID].Failures == 1
		e.repo.mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("не обновилось: %+v %+v %+v", e.repo.accs[a.ID], e.repo.accs[tgA.ID], e.repo.accs[off.ID])
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if in := e.tg.refreshes[0]; in.TgUserID != 777 {
		t.Errorf("Telegram: владелец %+v", in)
	}
	if e.repo.accs[off.ID].Status != domain.SocialVerified {
		t.Error("выключенная площадка не теряет отметку")
	}
}

func TestSocialRefreshTelegramWithoutLink(t *testing.T) {
	e := newSocialEnv(t)
	a, _ := e.repo.SaveAccount(context.Background(), domain.SocialAccount{UserID: creator.ID, Platform: domain.PlatformTelegram, ExternalID: "-1",
		Kind: domain.SocialGroup, Status: domain.SocialVerified, Method: domain.MethodBot}, e.clock.Now())
	_ = e.svc.refresh(context.Background(), a)
	if x := e.repo.accs[a.ID]; x.Status != domain.SocialFailed || x.FailReason != "Вы больше не админ канала — подтвердите заново" {
		t.Errorf("%+v", x)
	}
}

func TestSocialUnlink(t *testing.T) {
	ctx := context.Background()
	e := newSocialEnv(t)
	e.yt.channels = append(e.yt.channels, VerifiedChannel{ExternalID: "UC2", Kind: domain.SocialChannel, Stats: domain.SocialStats{FollowersHidden: true}})
	accs := e.connect(t)
	if len(accs) != 2 || accs[0].CredentialID != accs[1].CredentialID {
		t.Fatalf("%+v", accs)
	}
	if err := e.svc.Unlink(ctx, domain.User{ID: 9}, accs[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("чужой: %v", err)
	}
	// один из двух — токен остаётся
	if err := e.svc.Unlink(ctx, creator, accs[0].ID); err != nil || len(e.yt.revoked) != 0 || len(e.repo.creds) != 1 {
		t.Fatalf("%v revoked=%d", err, len(e.yt.revoked))
	}
	// последний — токен отозван у площадки и удалён
	if err := e.svc.Unlink(ctx, creator, accs[1].ID); err != nil || len(e.yt.revoked) != 1 || e.yt.revoked[0].Refresh != "refresh-1" || len(e.repo.creds) != 0 {
		t.Fatalf("%v %+v", err, e.yt.revoked)
	}
	// Telegram — без токенов
	a, _ := e.repo.SaveAccount(ctx, domain.SocialAccount{UserID: creator.ID, Platform: domain.PlatformTelegram, ExternalID: "-1", Kind: domain.SocialChannel,
		Status: domain.SocialVerified, Method: domain.MethodBot}, e.clock.Now())
	if err := e.svc.Unlink(ctx, creator, a.ID); err != nil {
		t.Fatal(err)
	}
}

func TestSocialBackoffAndJitter(t *testing.T) {
	e := newSocialEnv(t)
	if d := e.svc.backoff(0); d < 15*time.Minute || d > 17*time.Minute {
		t.Errorf("0: %v", d)
	}
	if d := e.svc.backoff(2); d < time.Hour || d > 67*time.Minute {
		t.Errorf("2: %v", d)
	}
	if d := e.svc.backoff(30); d < 6*time.Hour || d > 7*time.Hour {
		t.Errorf("30: %v", d)
	}
	now := e.clock.Now()
	seen := map[time.Time]bool{}
	for range 20 {
		n := e.svc.nextRefresh(now)
		if d := n.Sub(now); d < 22*time.Hour || d > 26*time.Hour {
			t.Fatalf("%v", d)
		}
		seen[n] = true
	}
	if len(seen) < 10 {
		t.Error("нет разброса")
	}
}

// ===== Telegram =====

func TestTelegramLinkAndChannels(t *testing.T) {
	ctx := context.Background()
	e := newSocialEnv(t)
	tgUser := domain.TelegramProfile{ID: 777, FirstName: "Алина"}
	// бота добавили в канал до привязки — «привяжите Telegram», канал запомнен
	r, err := e.svc.ChannelChanged(ctx, ChannelEvent{ChatID: -1001, Title: "Канал Алины", Type: "channel", Status: "administrator", From: tgUser})
	if err != nil || r.Outcome != ChannelNotLinked {
		t.Fatalf("%v %+v", err, r)
	}
	st, err := e.svc.Start(ctx, creator, domain.PlatformTelegram)
	if err != nil || st.State != "" || !strings.Contains(st.URL, "state=") {
		t.Fatalf("%v %+v", err, st)
	}
	tok := strings.TrimPrefix(st.URL[strings.Index(st.URL, "state="):], "state=")
	tok = tok[:strings.Index(tok, "&")]
	if _, ok := ParseSocialStart(SocialStartPrefix + tok); !ok {
		t.Fatalf("токен: %q", tok)
	}
	if l, err := e.svc.TelegramLinkInfo(ctx, tok); err != nil || l.Name != "Алина" {
		t.Fatalf("%v %+v", err, l)
	}
	if _, err := e.svc.TelegramLinkInfo(ctx, "x"); !errors.Is(err, ErrSocialState) {
		t.Errorf("чужой токен: %v", err)
	}
	// «Привязать»: Telegram привязан, запомненный канал подтверждён сразу
	res, err := e.svc.LinkTelegram(ctx, tok, tgUser)
	if err != nil || len(res.Verified) != 1 || *res.Verified[0].Stats.Followers != 2000 || e.repo.tg[creator.ID] != 777 {
		t.Fatalf("%v %+v", err, res)
	}
	if c := e.tg.checks[0]; c.ChatID != -1001 || c.TgUserID != 777 {
		t.Errorf("проверка: %+v", c)
	}
	if _, err := e.svc.LinkTelegram(ctx, tok, tgUser); !errors.Is(err, ErrSocialState) {
		t.Errorf("токен одноразовый: %v", err)
	}
	// группа добавлена после привязки — подтверждается сразу
	e.tg.channels = []VerifiedChannel{{ExternalID: "-1002", Kind: domain.SocialGroup, Title: "Чат", Stats: domain.SocialStats{Followers: ptr(150)}}}
	r, err = e.svc.ChannelChanged(ctx, ChannelEvent{ChatID: -1002, Title: "Чат", Type: "supergroup", Status: "member", From: tgUser})
	if err != nil || r.Outcome != ChannelVerified || r.Account.Kind != domain.SocialGroup {
		t.Fatalf("группа: %v %+v", err, r)
	}
	// добавивший — не админ
	e.tg.checkErr = ErrSocialNotOwner
	if r, _ := e.svc.ChannelChanged(ctx, ChannelEvent{ChatID: -1003, Type: "channel", Status: "administrator", From: tgUser}); r.Outcome != ChannelNotAdmin {
		t.Errorf("не админ: %+v", r)
	}
	// Telegram не ответил — «ждёт проверки», фоновая задача повторит через 5 минут
	e.tg.checkErr = fmt.Errorf("%w: 502", ErrSocialUnavailable)
	r, _ = e.svc.ChannelChanged(ctx, ChannelEvent{ChatID: -1004, Title: "Чат 2", Type: "group", Status: "administrator", From: tgUser})
	if r.Outcome != ChannelPending || r.Account.Status != domain.SocialPending || r.Account.Kind != domain.SocialGroup ||
		!r.Account.NextRefreshAt.Equal(e.clock.Now().Add(5*time.Minute)) {
		t.Errorf("временно: %+v", r)
	}
	e.tg.checkErr = errors.New("база")
	if _, err := e.svc.ChannelChanged(ctx, ChannelEvent{ChatID: -1005, Type: "channel", Status: "administrator", From: tgUser}); err == nil {
		t.Error("неожиданная ошибка — наверх")
	}
	// бота убрали — отметки сняты
	r, err = e.svc.ChannelChanged(ctx, ChannelEvent{ChatID: -1001, Type: "channel", Status: "left", From: tgUser})
	if err != nil || r.Outcome != ChannelRemoved {
		t.Fatalf("%v %+v", err, r)
	}
	accs, _ := e.repo.AccountsByExternal(ctx, domain.PlatformTelegram, "-1001")
	if accs[0].Status != domain.SocialFailed || accs[0].FailReason != "Бот убран из канала или группы — добавьте его снова" {
		t.Errorf("%+v", accs)
	}
	if r, _ := e.svc.ChannelChanged(ctx, ChannelEvent{ChatID: -9, Type: "channel", Status: "kicked", From: tgUser}); r.Outcome != ChannelIgnored {
		t.Errorf("чужой канал: %+v", r)
	}
	// боты и пустые события — без действий
	if r, err := e.svc.ChannelChanged(ctx, ChannelEvent{ChatID: -1, Status: "administrator", From: domain.TelegramProfile{ID: 5, IsBot: true}}); err != nil || r.Outcome != "" {
		t.Errorf("бот: %v %+v", err, r)
	}
}

func TestTelegramLinkTakenAndCancel(t *testing.T) {
	ctx := context.Background()
	e := newSocialEnv(t)
	e.repo.tg[9] = 777 // этот Telegram уже у другого аккаунта
	st, _ := e.svc.Start(ctx, creator, domain.PlatformTelegram)
	tok := st.URL[strings.Index(st.URL, "state=")+6:]
	tok = tok[:strings.Index(tok, "&")]
	if _, err := e.svc.LinkTelegram(ctx, tok, domain.TelegramProfile{ID: 777}); !errors.Is(err, ErrTelegramTaken) {
		t.Fatalf("%v", err)
	}
	st, _ = e.svc.Start(ctx, creator, domain.PlatformTelegram)
	tok = st.URL[strings.Index(st.URL, "state=")+6:]
	tok = tok[:strings.Index(tok, "&")]
	if err := e.svc.CancelTelegramLink(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.CancelTelegramLink(ctx, tok); err != nil {
		t.Errorf("повторная отмена: %v", err)
	}
	if _, err := e.svc.LinkTelegram(ctx, tok, domain.TelegramProfile{ID: 778}); !errors.Is(err, ErrSocialState) {
		t.Errorf("после отмены: %v", err)
	}
	if _, err := e.svc.LinkTelegram(ctx, tok, domain.TelegramProfile{ID: 5, IsBot: true}); !errors.Is(err, ErrSocialState) {
		t.Errorf("бот: %v", err)
	}
	// уже привязан к этому же аккаунту — ок
	e.repo.tg[creator.ID] = 779
	st, _ = e.svc.Start(ctx, creator, domain.PlatformTelegram)
	tok = st.URL[strings.Index(st.URL, "state=")+6:]
	tok = tok[:strings.Index(tok, "&")]
	if _, err := e.svc.LinkTelegram(ctx, tok, domain.TelegramProfile{ID: 779}); err != nil {
		t.Errorf("свой: %v", err)
	}
}

func TestParseSocialStart(t *testing.T) {
	good := SocialStartPrefix + strings.Repeat("a", 43)
	if tok, ok := ParseSocialStart(good); !ok || len(tok) != 43 {
		t.Error(good)
	}
	for _, s := range []string{"", "ch_", "login_" + strings.Repeat("a", 43), SocialStartPrefix + strings.Repeat("a", 42), SocialStartPrefix + strings.Repeat("a", 42) + "!"} {
		if _, ok := ParseSocialStart(s); ok {
			t.Errorf("%q", s)
		}
	}
}

func TestFailReason(t *testing.T) {
	if r := failReason(domain.PlatformYouTube, ErrSocialNoChannel); r != "Аккаунт больше не найден — подключите заново" {
		t.Error(r)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

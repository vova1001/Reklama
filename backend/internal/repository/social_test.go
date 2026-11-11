package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ohvat/backend/internal/domain"
	"ohvat/backend/internal/repository"
)

func TestSocialRepository(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	auth := repository.NewAuth(pool)
	r := repository.NewSocial(pool)
	u := newUser(t, auth, "social@x.ru")
	other := newUser(t, auth, "social2@x.ru")
	now := time.Now().UTC().Truncate(time.Second)
	n := int64(1200)

	// доступ: шифротекст как есть, повторное подключение того же человека — та же строка
	cred, err := r.SaveCredential(ctx, domain.SocialCredential{UserID: u.ID, Platform: domain.PlatformYouTube, Subject: "UC1", Secret: []byte{1, 2, 3},
		AccessExpiresAt: &now})
	if err != nil || cred.ID == 0 {
		t.Fatal(err)
	}
	again, err := r.SaveCredential(ctx, domain.SocialCredential{UserID: u.ID, Platform: domain.PlatformYouTube, Subject: "UC1", Secret: []byte{4}})
	if err != nil || again.ID != cred.ID || string(again.Secret) != "\x04" || again.AccessExpiresAt != nil {
		t.Fatalf("%v %+v", err, again)
	}
	// аренда: второй раз — занято; снята — снова можно; нет — not found
	if _, err := r.LockCredential(ctx, cred.ID, now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LockCredential(ctx, cred.ID, now, now.Add(time.Minute)); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("занято: %v", err)
	}
	if _, err := r.LockCredential(ctx, cred.ID, now.Add(2*time.Minute), now.Add(3*time.Minute)); err != nil {
		t.Fatalf("аренда истекла: %v", err)
	}
	if err := r.UpdateCredential(ctx, domain.SocialCredential{ID: cred.ID, Secret: []byte{9}, RefreshExpiresAt: &now}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LockCredential(ctx, cred.ID, now, now.Add(time.Minute)); err != nil {
		t.Fatalf("после сохранения аренда снята: %v", err)
	}
	if err := r.UnlockCredential(ctx, cred.ID); err != nil {
		t.Fatal(err)
	}
	if c, err := r.Credential(ctx, cred.ID); err != nil || c.Secret[0] != 9 || c.RefreshExpiresAt == nil {
		t.Fatalf("%v %+v", err, c)
	}
	if _, err := r.LockCredential(ctx, 999999, now, now); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("нет: %v", err)
	}

	// аккаунт: подтверждение + история; повтор — та же строка
	next := now.Add(24 * time.Hour)
	a, err := r.SaveAccount(ctx, domain.SocialAccount{UserID: u.ID, Platform: domain.PlatformYouTube, ExternalID: "UC1", Kind: domain.SocialChannel,
		Title: "Канал", URL: "https://www.youtube.com/@a", Status: domain.SocialVerified, Method: domain.MethodOAuth, CredentialID: cred.ID,
		Stats: domain.SocialStats{Followers: &n, Views: &n}, VerifiedAt: &now, RefreshedAt: &now, NextRefreshAt: &next}, now)
	if err != nil || a.ID == 0 || *a.Stats.Followers != 1200 || a.Stats.Videos != nil || a.CredentialID != cred.ID {
		t.Fatalf("%v %+v", err, a)
	}
	a2, err := r.SaveAccount(ctx, domain.SocialAccount{UserID: u.ID, Platform: domain.PlatformYouTube, ExternalID: "UC1", Kind: domain.SocialChannel,
		Status: domain.SocialVerified, Method: domain.MethodOAuth, CredentialID: cred.ID, Stats: domain.SocialStats{FollowersHidden: true}}, now.Add(time.Second))
	if err != nil || a2.ID != a.ID || a2.Stats.Followers != nil || !a2.Stats.FollowersHidden || a2.VerifiedAt == nil {
		t.Fatalf("повтор: %v %+v", err, a2)
	}
	// тот же канал у другого автора — своя строка
	tg, err := r.SaveAccount(ctx, domain.SocialAccount{UserID: other.ID, Platform: domain.PlatformTelegram, ExternalID: "-1001", Kind: domain.SocialGroup,
		Status: domain.SocialPending, Method: domain.MethodBot, NextRefreshAt: &now}, now)
	if err != nil || tg.CredentialID != 0 {
		t.Fatal(err)
	}
	if _, err := r.Account(ctx, u.ID, tg.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("чужой: %v", err)
	}
	if c, _ := r.CountAccounts(ctx, u.ID); c != 1 {
		t.Errorf("число: %d", c)
	}
	// обновление цифр
	m := int64(1500)
	upd := a2
	upd.Title, upd.Stats = "Новое", domain.SocialStats{Followers: &m}
	got, err := r.SaveStats(ctx, upd, now.Add(time.Hour), next)
	if err != nil || got.Title != "Новое" || *got.Stats.Followers != 1500 || got.Status != domain.SocialVerified {
		t.Fatalf("%v %+v", err, got)
	}
	st, at, err := r.History(ctx, a.ID, 10)
	if err != nil || len(st) != 3 || *st[0].Followers != 1500 || !at[0].Equal(now.Add(time.Hour)) {
		t.Fatalf("история: %v %d", err, len(st))
	}
	// подтверждённые — для анкет
	vs, err := r.VerifiedAccounts(ctx, []int64{u.ID, other.ID})
	if err != nil || len(vs) != 1 || vs[0].ID != a.ID {
		t.Fatalf("%v %+v", err, vs)
	}
	// аренда на обновление: pending Telegram — пора; второй раз — уже арендован
	due, err := r.ClaimDue(ctx, now.Add(time.Minute), now.Add(time.Hour), 10)
	if err != nil || len(due) != 1 || due[0].ID != tg.ID {
		t.Fatalf("due: %v %+v", err, due)
	}
	if due, _ := r.ClaimDue(ctx, now.Add(time.Minute), now.Add(time.Hour), 10); len(due) != 0 {
		t.Errorf("повторно: %+v", due)
	}
	if err := r.SetRetry(ctx, tg.ID, now); err != nil {
		t.Fatal(err)
	}
	if x, _ := r.AccountsByExternal(ctx, domain.PlatformTelegram, "-1001"); len(x) != 1 || x[0].Failures != 1 {
		t.Errorf("%+v", x)
	}
	if err := r.SetFailed(ctx, tg.ID, "Бот удалён"); err != nil {
		t.Fatal(err)
	}
	if x, _ := r.Account(ctx, other.ID, tg.ID); x.Status != domain.SocialFailed || x.FailReason != "Бот удалён" || x.NextRefreshAt != nil {
		t.Errorf("%+v", x)
	}
	// отвязка: доступ ещё нужен? нет
	if c, _ := r.CredentialAccounts(ctx, cred.ID); c != 1 {
		t.Errorf("пользуются: %d", c)
	}
	if _, err := r.DeleteAccount(ctx, other.ID, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("чужой: %v", err)
	}
	if d, err := r.DeleteAccount(ctx, u.ID, a.ID); err != nil || d.CredentialID != cred.ID {
		t.Fatal(err)
	}
	if c, _ := r.CredentialAccounts(ctx, cred.ID); c != 0 {
		t.Errorf("после отвязки: %d", c)
	}
	if err := r.DeleteCredential(ctx, cred.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Credential(ctx, cred.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("%v", err)
	}

	// начатые подключения: одноразовые, со сроком
	h := []byte("hash-1")
	if err := r.CreateState(ctx, h, domain.SocialState{UserID: u.ID, Platform: domain.PlatformYouTube, Verifier: []byte{7}, CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if c, first, err := r.StateStats(ctx, u.ID, now.Add(-time.Hour)); err != nil || c != 1 || !first.Equal(now) {
		t.Fatalf("%v %d %v", err, c, first)
	}
	if _, err := r.PeekState(ctx, h, domain.PlatformVK, now); !errors.Is(err, domain.ErrNotFound) {
		t.Error("другая площадка")
	}
	if s, err := r.PeekState(ctx, h, domain.PlatformYouTube, now); err != nil || s.UserID != u.ID || s.Verifier[0] != 7 {
		t.Fatal(err)
	}
	if _, err := r.TakeState(ctx, h, domain.PlatformYouTube, now.Add(11*time.Minute)); !errors.Is(err, domain.ErrNotFound) {
		t.Error("истёк")
	}
	if _, err := r.TakeState(ctx, h, domain.PlatformYouTube, now); err != nil {
		t.Fatal(err)
	}
	if _, err := r.TakeState(ctx, h, domain.PlatformYouTube, now); !errors.Is(err, domain.ErrNotFound) {
		t.Error("одноразовый")
	}
	_ = r.CreateState(ctx, []byte("old"), domain.SocialState{UserID: u.ID, Platform: domain.PlatformTelegram, CreatedAt: now, ExpiresAt: now})
	if n, err := r.DeleteStatesBefore(ctx, now.Add(time.Minute)); err != nil || n != 1 {
		t.Errorf("очистка: %v %d", err, n)
	}

	// Telegram: каналы и привязка
	if _, err := r.UserTelegramID(ctx, u.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("без Telegram: %v", err)
	}
	if err := auth.LinkIdentity(ctx, u.ID, domain.Identity{Provider: domain.ProviderTelegram, Subject: "777"}); err != nil {
		t.Fatal(err)
	}
	if id, err := r.UserTelegramID(ctx, u.ID); err != nil || id != 777 {
		t.Fatalf("%v %d", err, id)
	}
	for _, ch := range []domain.TelegramChannel{
		{ChatID: -1001, Title: "Канал", Username: "chan", Type: "channel", AddedBy: 777, BotStatus: "administrator"},
		{ChatID: -1002, Title: "Чат", Type: "supergroup", AddedBy: 777, BotStatus: "member"},
		{ChatID: -1003, Title: "Убран", Type: "channel", AddedBy: 777, BotStatus: "left"},
	} {
		if err := r.SaveTelegramChannel(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}
	chs, err := r.TelegramChannelsAddedBy(ctx, 777)
	if err != nil || len(chs) != 2 {
		t.Fatalf("%v %+v", err, chs)
	}
	for _, c := range chs {
		if c.ChatID == -1002 && (c.Type != "supergroup" || c.Kind() != domain.SocialGroup) {
			t.Errorf("%+v", c)
		}
	}
}

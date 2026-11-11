package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"strconv"
	"time"

	"crypto/rand"

	"ohvat/backend/internal/domain"
)

// ===== подтверждение соцсетей автора =====
// Автор подтверждает, что аккаунт его, через официальный API площадки; цифры в анкете берутся только отсюда.
// Площадки — за PlatformVerifier (gateway/platforms/*); токены OAuth — только зашифрованно (TokenCipher);
// фоновая задача раз в сутки (с разбросом) обновляет цифры, токены обновляются по правилам площадки.

// SocialRepository — аккаунты, доступы, начатые подключения, каналы Telegram.
type SocialRepository interface {
	Accounts(ctx context.Context, userID int64) ([]domain.SocialAccount, error)
	VerifiedAccounts(ctx context.Context, userIDs []int64) ([]domain.SocialAccount, error)
	// Account — аккаунт пользователя; чужой или нет — domain.ErrNotFound.
	Account(ctx context.Context, userID, id int64) (domain.SocialAccount, error)
	CountAccounts(ctx context.Context, userID int64) (int, error)
	// SaveAccount — создать или обновить (по user_id, площадке, external_id) и записать цифры в историю.
	SaveAccount(ctx context.Context, a domain.SocialAccount, at time.Time) (domain.SocialAccount, error)
	// SaveStats — свежие цифры, имя и ссылка из upd (по upd.ID; статус verified) и запись в историю — одной транзакцией.
	SaveStats(ctx context.Context, upd domain.SocialAccount, at, next time.Time) (domain.SocialAccount, error)
	SetRetry(ctx context.Context, id int64, next time.Time) error
	SetFailed(ctx context.Context, id int64, reason string) error
	// ClaimDue — аккаунты, которым пора обновиться; их следующая попытка сдвигается на leaseUntil.
	ClaimDue(ctx context.Context, now, leaseUntil time.Time, limit int) ([]domain.SocialAccount, error)
	// DeleteAccount — отвязать; чужой или нет — domain.ErrNotFound.
	DeleteAccount(ctx context.Context, userID, id int64) (domain.SocialAccount, error)
	AccountsByExternal(ctx context.Context, p domain.SocialPlatform, externalID string) ([]domain.SocialAccount, error)

	SaveCredential(ctx context.Context, c domain.SocialCredential) (domain.SocialCredential, error)
	// LockCredential — аренда доступа на обновление токена; занят — domain.ErrConflict.
	LockCredential(ctx context.Context, id int64, now, until time.Time) (domain.SocialCredential, error)
	// UpdateCredential — новые токены и снятие аренды.
	UpdateCredential(ctx context.Context, c domain.SocialCredential) error
	UnlockCredential(ctx context.Context, id int64) error
	Credential(ctx context.Context, id int64) (domain.SocialCredential, error)
	CredentialAccounts(ctx context.Context, id int64) (int, error)
	DeleteCredential(ctx context.Context, id int64) error

	CreateState(ctx context.Context, hash []byte, s domain.SocialState) error
	StateStats(ctx context.Context, userID int64, since time.Time) (int, time.Time, error)
	// PeekState / TakeState — неистёкшее подключение (Take — гасит, один раз); нет — domain.ErrNotFound.
	PeekState(ctx context.Context, hash []byte, p domain.SocialPlatform, now time.Time) (domain.SocialState, error)
	TakeState(ctx context.Context, hash []byte, p domain.SocialPlatform, now time.Time) (domain.SocialState, error)
	DeleteStatesBefore(ctx context.Context, t time.Time) (int64, error)

	SaveTelegramChannel(ctx context.Context, ch domain.TelegramChannel) error
	TelegramChannelsAddedBy(ctx context.Context, tgUserID int64) ([]domain.TelegramChannel, error)
	// UserTelegramID — Telegram пользователя (вход через Telegram или привязка); нет — domain.ErrNotFound.
	UserTelegramID(ctx context.Context, userID int64) (int64, error)
}

// SocialUsers — пользователи и способы входа (привязка Telegram).
type SocialUsers interface {
	UserByIdentity(ctx context.Context, provider, subject string) (domain.User, error)
	LinkIdentity(ctx context.Context, userID int64, id domain.Identity) error
	UserByID(ctx context.Context, id int64) (domain.User, error)
}

// TokenCipher — шифрование токенов (AES-GCM, secretbox).
type TokenCipher interface {
	Seal(plain, aad []byte) ([]byte, error)
	Open(sealed, aad []byte) ([]byte, error)
}

// SocialConfig — настройки подтверждения.
type SocialConfig struct {
	SiteURL        string        // PUBLIC_URL: адреса возврата OAuth
	RefreshEvery   time.Duration // как часто обновлять цифры, 24 ч (разброс ±1/12)
	StartsPerHour  int           // начатых подключений на пользователя в час, 20
	ManualEvery    time.Duration // ручное «Обновить» не чаще, 10 мин
	MaxAccounts    int           // аккаунтов на пользователя, 20
	MaxPerConnect  int           // аккаунтов за одно подключение (сообщества VK), 10
	StateTTL       time.Duration // OAuth, 10 мин
	TelegramTTL    time.Duration // ссылка на бота, 30 мин
	PlatformGap    time.Duration // пауза между запросами к одной площадке в фоне, 1 с
	Batch          int           // аккаунтов за один заход фоновой задачи, 50
	RequestTimeout time.Duration // одна проверка у площадки, 20 с
}

func (c *SocialConfig) defaults() {
	if c.RefreshEvery <= 0 {
		c.RefreshEvery = 24 * time.Hour
	}
	if c.StartsPerHour <= 0 {
		c.StartsPerHour = 20
	}
	if c.ManualEvery <= 0 {
		c.ManualEvery = 10 * time.Minute
	}
	if c.MaxAccounts <= 0 {
		c.MaxAccounts = 20
	}
	if c.MaxPerConnect <= 0 {
		c.MaxPerConnect = 10
	}
	if c.StateTTL <= 0 {
		c.StateTTL = 10 * time.Minute
	}
	if c.TelegramTTL <= 0 {
		c.TelegramTTL = 30 * time.Minute
	}
	if c.PlatformGap < 0 {
		c.PlatformGap = 0
	} else if c.PlatformGap == 0 {
		c.PlatformGap = time.Second
	}
	if c.Batch <= 0 {
		c.Batch = 50
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 20 * time.Second
	}
}

// SocialDeps — зависимости.
type SocialDeps struct {
	Repo      SocialRepository
	Users     SocialUsers
	Cipher    TokenCipher        // nil — площадки с OAuth выключены (токены негде хранить)
	Verifiers []PlatformVerifier // подключённые площадки (по ключам)
	Clock     Clock
	Rand      io.Reader
	Log       *slog.Logger
}

// Social — подключение, проверка, обновление и отвязка соцсетей.
type Social struct {
	repo   SocialRepository
	users  SocialUsers
	cipher TokenCipher
	vs     map[domain.SocialPlatform]PlatformVerifier
	clock  Clock
	rnd    io.Reader
	log    *slog.Logger
	cfg    SocialConfig
}

// NewSocial создаёт сервис. Площадка с OAuth без шифрования токенов не включается.
func NewSocial(d SocialDeps, cfg SocialConfig) *Social {
	cfg.defaults()
	if d.Clock == nil {
		d.Clock = SystemClock{}
	}
	if d.Rand == nil {
		d.Rand = cryptoRand
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	s := &Social{repo: d.Repo, users: d.Users, cipher: d.Cipher, vs: map[domain.SocialPlatform]PlatformVerifier{},
		clock: d.Clock, rnd: d.Rand, log: d.Log, cfg: cfg}
	for _, v := range d.Verifiers {
		if v == nil {
			continue
		}
		if v.Method() == domain.MethodOAuth && d.Cipher == nil {
			d.Log.Warn("соцсети: площадка выключена — нет TOKEN_ENC_KEY для токенов", "platform", v.Platform())
			continue
		}
		s.vs[v.Platform()] = v
	}
	return s
}

// Available — площадки, которые можно подключить сейчас.
func (s *Social) Available(p domain.SocialPlatform) bool { _, ok := s.vs[p]; return ok }

// ===== обзор =====

// PlatformView — площадка в анкете: можно ли подключить, статус, аккаунты.
type PlatformView struct {
	Platform  domain.SocialPlatform
	Available bool
	Method    domain.SocialMethod
	Status    domain.SocialStatus
	Accounts  []domain.SocialAccount
}

var platformMethod = map[domain.SocialPlatform]domain.SocialMethod{
	domain.PlatformTelegram: domain.MethodBot, domain.PlatformYouTube: domain.MethodOAuth, domain.PlatformVK: domain.MethodOAuth,
	domain.PlatformTikTok: domain.MethodOAuth, domain.PlatformInstagram: domain.MethodOAuth, domain.PlatformThreads: domain.MethodOAuth,
}

// Overview — все площадки по порядку со статусом: verified — есть подтверждённый; failed — доступ потерян;
// pending — проверка не завершена; unverified — не подключено.
func (s *Social) Overview(ctx context.Context, u domain.User) ([]PlatformView, error) {
	accs, err := s.repo.Accounts(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	by := map[domain.SocialPlatform][]domain.SocialAccount{}
	for _, a := range accs {
		by[a.Platform] = append(by[a.Platform], a)
	}
	out := make([]PlatformView, 0, len(domain.SocialPlatforms))
	for _, p := range domain.SocialPlatforms {
		v := PlatformView{Platform: p, Available: s.Available(p), Method: platformMethod[p], Status: domain.SocialUnverified, Accounts: by[p]}
		if v.Accounts == nil {
			v.Accounts = []domain.SocialAccount{}
		}
		v.Status = platformStatus(v.Accounts)
		out = append(out, v)
	}
	return out, nil
}

func platformStatus(accs []domain.SocialAccount) domain.SocialStatus {
	has := map[domain.SocialStatus]bool{}
	for _, a := range accs {
		has[a.Status] = true
	}
	for _, st := range []domain.SocialStatus{domain.SocialVerified, domain.SocialPending, domain.SocialFailed} {
		if has[st] {
			return st
		}
	}
	return domain.SocialUnverified
}

// CardPlatforms — подтверждённые соцсети авторов для анкет (только цифры от площадок).
func (s *Social) CardPlatforms(ctx context.Context, userIDs []int64) (map[int64][]domain.Platform, error) {
	out := map[int64][]domain.Platform{}
	if len(userIDs) == 0 {
		return out, nil
	}
	accs, err := s.repo.VerifiedAccounts(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	for _, a := range accs {
		p := domain.Platform{Kind: string(a.Platform), Type: a.Kind, Handle: a.Handle, Title: a.Title, URL: a.URL,
			FollowersHidden: a.Stats.FollowersHidden, Verified: true, RefreshedAt: a.RefreshedAt}
		if a.Stats.Followers != nil {
			p.Followers = *a.Stats.Followers
		}
		out[a.UserID] = append(out[a.UserID], p)
	}
	return out, nil
}

// ===== подключение =====

// SocialStart — куда отправить автора; State — для привязки к браузеру (cookie), только у OAuth.
type SocialStart struct {
	URL   string
	State string
}

// Start — начать подключение площадки: state (одноразовый, с PKCE) и адрес разрешения площадки
// (Telegram — ссылка на бота с одноразовым токеном привязки).
func (s *Social) Start(ctx context.Context, u domain.User, p domain.SocialPlatform) (SocialStart, error) {
	v, ok := s.vs[p]
	if !ok {
		return SocialStart{}, ErrSocialDisabled
	}
	now := s.clock.Now()
	n, first, err := s.repo.StateStats(ctx, u.ID, now.Add(-time.Hour))
	if err != nil {
		return SocialStart{}, err
	}
	if n >= s.cfg.StartsPerHour {
		return SocialStart{}, &LimitError{Reason: "social_hourly", RetryAfter: first.Add(time.Hour).Sub(now)}
	}
	cnt, err := s.repo.CountAccounts(ctx, u.ID)
	if err != nil {
		return SocialStart{}, err
	}
	if cnt >= s.cfg.MaxAccounts {
		return SocialStart{}, ErrSocialTooMany
	}
	state, err := randomToken(s.rnd)
	if err != nil {
		return SocialStart{}, err
	}
	st := domain.SocialState{UserID: u.ID, Platform: p, CreatedAt: now, ExpiresAt: now.Add(s.cfg.StateTTL)}
	in := VerifyStart{State: state, RedirectURL: s.redirectURL(p)}
	if v.Method() == domain.MethodBot {
		st.ExpiresAt = now.Add(s.cfg.TelegramTTL)
	} else {
		verifier, err := randomToken(s.rnd)
		if err != nil {
			return SocialStart{}, err
		}
		if st.Verifier, err = s.cipher.Seal([]byte(verifier), stateAAD(u.ID, p)); err != nil {
			return SocialStart{}, err
		}
		in.Challenge = pkceChallenge(verifier)
	}
	step, err := v.Start(ctx, in)
	if err != nil {
		return SocialStart{}, err
	}
	if err := s.repo.CreateState(ctx, hashToken(state), st); err != nil {
		return SocialStart{}, err
	}
	out := SocialStart{URL: step.URL}
	if v.Method() == domain.MethodOAuth {
		out.State = state
	}
	return out, nil
}

func (s *Social) redirectURL(p domain.SocialPlatform) string {
	return s.cfg.SiteURL + "/api/social/" + string(p) + "/callback"
}

// SocialCallback — возврат с площадки.
type SocialCallback struct {
	State    string
	Code     string
	Error    string // площадка вернула ошибку (автор отказал)
	DeviceID string // VK ID
}

// Callback — завершить подключение OAuth: state (свой, неистёкший, одноразовый) → обмен кода → аккаунты
// с цифрами → доступ (зашифрованно) и аккаунты в базу.
func (s *Social) Callback(ctx context.Context, u domain.User, p domain.SocialPlatform, in SocialCallback) ([]domain.SocialAccount, error) {
	v, ok := s.vs[p]
	if !ok || v.Method() != domain.MethodOAuth {
		return nil, ErrSocialDisabled
	}
	if in.State == "" || len(in.State) > 128 {
		return nil, ErrSocialState
	}
	now := s.clock.Now()
	st, err := s.repo.TakeState(ctx, hashToken(in.State), p, now)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && st.UserID != u.ID) {
		return nil, ErrSocialState
	}
	if err != nil {
		return nil, err
	}
	if in.Error != "" {
		return nil, ErrSocialDenied
	}
	if in.Code == "" || len(in.Code) > 4096 || len(in.DeviceID) > 256 {
		return nil, ErrSocialState
	}
	verifier, err := s.cipher.Open(st.Verifier, stateAAD(u.ID, p))
	if err != nil {
		return nil, fmt.Errorf("соцсети: verifier: %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	res, err := v.Check(cctx, VerifyCheck{Code: in.Code, Verifier: string(verifier), RedirectURL: s.redirectURL(p), State: in.State, DeviceID: in.DeviceID})
	if err != nil {
		return nil, err
	}
	if len(res.Channels) == 0 {
		return nil, ErrSocialNoChannel
	}
	if res.Tokens == nil || res.Subject == "" {
		return nil, fmt.Errorf("соцсети %s: площадка не вернула токены", p)
	}
	cnt, err := s.repo.CountAccounts(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	room := s.cfg.MaxAccounts - cnt
	chans := res.Channels
	if len(chans) > s.cfg.MaxPerConnect {
		chans = chans[:s.cfg.MaxPerConnect]
	}
	if room < len(chans) {
		// повторное подключение тех же аккаунтов место не занимает — проверит лимит база при вставке; здесь грубо
		have, err := s.repo.Accounts(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		known := map[string]bool{}
		for _, a := range have {
			if a.Platform == p {
				known[a.ExternalID] = true
			}
		}
		fresh := 0
		for _, c := range chans {
			if !known[c.ExternalID] {
				fresh++
			}
		}
		if fresh > room {
			return nil, ErrSocialTooMany
		}
	}
	res.Tokens.IssuedAt = now
	cred, err := s.sealCredential(u.ID, p, res.Subject, *res.Tokens)
	if err != nil {
		return nil, err
	}
	if cred, err = s.repo.SaveCredential(ctx, cred); err != nil {
		return nil, err
	}
	out := make([]domain.SocialAccount, 0, len(chans))
	for _, c := range chans {
		a, err := s.repo.SaveAccount(ctx, s.verifiedAccount(u.ID, p, v.Method(), cred.ID, c, now), now)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	s.log.InfoContext(ctx, "соцсети: подтверждено", "user", u.ID, "platform", p, "accounts", len(out))
	return out, nil
}

func (s *Social) verifiedAccount(userID int64, p domain.SocialPlatform, m domain.SocialMethod, credID int64, c VerifiedChannel, now time.Time) domain.SocialAccount {
	next := s.nextRefresh(now)
	return domain.SocialAccount{
		UserID: userID, Platform: p, ExternalID: c.ExternalID, Kind: c.Kind, Handle: c.Handle, Title: c.Title, URL: c.URL,
		Status: domain.SocialVerified, Method: m, CredentialID: credID, Stats: c.Stats,
		VerifiedAt: &now, RefreshedAt: &now, NextRefreshAt: &next,
	}
}

// ===== обновление =====

// RefreshNow — «Обновить» у своего аккаунта (не чаще ManualEvery).
func (s *Social) RefreshNow(ctx context.Context, u domain.User, id int64) (domain.SocialAccount, error) {
	a, err := s.repo.Account(ctx, u.ID, id)
	if err != nil {
		return domain.SocialAccount{}, err
	}
	now := s.clock.Now()
	if a.RefreshedAt != nil && now.Sub(*a.RefreshedAt) < s.cfg.ManualEvery && a.Status == domain.SocialVerified {
		return domain.SocialAccount{}, &LimitError{Reason: "social_refresh", RetryAfter: a.RefreshedAt.Add(s.cfg.ManualEvery).Sub(now)}
	}
	if _, ok := s.vs[a.Platform]; !ok {
		return domain.SocialAccount{}, ErrSocialDisabled
	}
	if err := s.refresh(ctx, a); err != nil {
		if errors.Is(err, ErrSocialUnavailable) {
			return domain.SocialAccount{}, err
		}
		s.log.WarnContext(ctx, "соцсети: обновление", "account", a.ID, "platform", a.Platform, "err", err)
	}
	return s.repo.Account(ctx, u.ID, id)
}

// refresh — свежие цифры одного аккаунта. Итог записан в базу: verified (цифры), failed (доступ потерян)
// или повтор позже (временный сбой — ErrSocialUnavailable).
func (s *Social) refresh(ctx context.Context, a domain.SocialAccount) error {
	v, ok := s.vs[a.Platform]
	now := s.clock.Now()
	if !ok { // площадку выключили (ключи убраны) — цифры остаются, проверим позже
		return s.repo.SetRetry(ctx, a.ID, s.nextRefresh(now))
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	in := RefreshInput{Account: a}
	switch v.Method() {
	case domain.MethodOAuth:
		if a.CredentialID == 0 {
			return s.fail(ctx, a, ErrSocialRevoked)
		}
		tok, err := s.freshTokens(ctx, v, a)
		if errors.Is(err, domain.ErrConflict) { // доступ обновляет другой процесс — позже
			return s.repo.SetRetry(ctx, a.ID, now.Add(5*time.Minute))
		}
		if err != nil {
			return s.afterError(ctx, a, err)
		}
		in.Tokens = &tok
	case domain.MethodBot:
		tg, err := s.repo.UserTelegramID(ctx, a.UserID)
		if errors.Is(err, domain.ErrNotFound) {
			return s.fail(ctx, a, ErrSocialNotOwner)
		}
		if err != nil {
			return err
		}
		in.TgUserID = tg
	}
	ch, err := v.Refresh(ctx, in)
	if err != nil {
		return s.afterError(ctx, a, err)
	}
	a.Handle, a.Title, a.URL, a.Stats = ch.Handle, ch.Title, ch.URL, ch.Stats
	_, err = s.repo.SaveStats(ctx, a, now, s.nextRefresh(now))
	return err
}

// freshTokens — токены доступа (под арендой: одноразовый refresh-токен обновляет только один процесс).
func (s *Social) freshTokens(ctx context.Context, v PlatformVerifier, a domain.SocialAccount) (Tokens, error) {
	now := s.clock.Now()
	cred, err := s.repo.LockCredential(ctx, a.CredentialID, now, now.Add(2*time.Minute))
	if errors.Is(err, domain.ErrNotFound) {
		return Tokens{}, ErrSocialRevoked
	}
	if err != nil {
		return Tokens{}, err
	}
	saved := false
	defer func() {
		if !saved {
			if err := s.repo.UnlockCredential(context.WithoutCancel(ctx), cred.ID); err != nil {
				s.log.WarnContext(ctx, "соцсети: снять аренду доступа", "err", err)
			}
		}
	}()
	tok, err := s.openCredential(cred)
	if err != nil {
		s.log.ErrorContext(ctx, "соцсети: токены не расшифрованы (сменился TOKEN_ENC_KEY?)", "credential", cred.ID, "err", err)
		return Tokens{}, ErrSocialRevoked
	}
	keeper, ok := v.(TokenKeeper)
	if !ok {
		return tok, nil
	}
	fresh, changed, err := keeper.Renew(ctx, tok, now)
	if err != nil {
		return Tokens{}, err
	}
	if !changed {
		return tok, nil
	}
	if fresh.IssuedAt.IsZero() {
		fresh.IssuedAt = now
	}
	upd, err := s.sealCredential(cred.UserID, cred.Platform, cred.Subject, fresh)
	if err != nil {
		return Tokens{}, err
	}
	upd.ID = cred.ID
	if err := s.repo.UpdateCredential(ctx, upd); err != nil {
		return Tokens{}, err
	}
	saved = true
	return fresh, nil
}

// afterError — доступ потерян → failed; временный сбой → повтор с растущей паузой.
func (s *Social) afterError(ctx context.Context, a domain.SocialAccount, err error) error {
	switch {
	case errors.Is(err, ErrSocialRevoked), errors.Is(err, ErrSocialNotOwner), errors.Is(err, ErrSocialNoChannel):
		return s.fail(ctx, a, err)
	}
	if !errors.Is(err, ErrSocialUnavailable) {
		s.log.WarnContext(ctx, "соцсети: сбой обновления", "account", a.ID, "platform", a.Platform, "err", err)
	}
	if rerr := s.repo.SetRetry(ctx, a.ID, s.clock.Now().Add(s.backoff(a.Failures))); rerr != nil {
		return rerr
	}
	return fmt.Errorf("%w: %w", ErrSocialUnavailable, err)
}

func (s *Social) fail(ctx context.Context, a domain.SocialAccount, err error) error {
	s.log.InfoContext(ctx, "соцсети: доступ потерян", "account", a.ID, "platform", a.Platform, "err", err)
	return s.repo.SetFailed(ctx, a.ID, failReason(a.Platform, err))
}

// failReason — понятная причина для автора.
func failReason(p domain.SocialPlatform, err error) string {
	switch {
	case p == domain.PlatformTelegram && errors.Is(err, ErrSocialNotOwner):
		return "Вы больше не админ канала — подтвердите заново"
	case p == domain.PlatformTelegram:
		return "Бот убран из канала или группы — добавьте его снова"
	case errors.Is(err, ErrSocialNoChannel):
		return "Аккаунт больше не найден — подключите заново"
	default:
		return "Доступ отозван — подключите заново"
	}
}

// RunRefresher — раз в every забирает аккаунты, которым пора обновиться (аренда — другой сервер их не возьмёт),
// и обновляет по одному с паузой между запросами к одной площадке. Заодно чистит старые подключения.
func (s *Social) RunRefresher(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	last := map[domain.SocialPlatform]time.Time{}
	for {
		s.refreshDue(ctx, last)
		if n, err := s.repo.DeleteStatesBefore(ctx, s.clock.Now().Add(-time.Hour)); err != nil && ctx.Err() == nil {
			s.log.WarnContext(ctx, "соцсети: очистка подключений", "err", err)
		} else if n > 0 {
			s.log.InfoContext(ctx, "соцсети: старые подключения удалены", "n", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// refreshDue — один заход: до 10 пачек по Batch.
func (s *Social) refreshDue(ctx context.Context, last map[domain.SocialPlatform]time.Time) {
	for range 10 {
		now := s.clock.Now()
		lease := now.Add(time.Duration(s.cfg.Batch)*(s.cfg.PlatformGap+s.cfg.RequestTimeout) + time.Minute)
		accs, err := s.repo.ClaimDue(ctx, now, lease, s.cfg.Batch)
		if err != nil {
			if ctx.Err() == nil {
				s.log.WarnContext(ctx, "соцсети: выбор аккаунтов на обновление", "err", err)
			}
			return
		}
		for _, a := range accs {
			if wait := time.Until(last[a.Platform].Add(s.cfg.PlatformGap)); wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
			last[a.Platform] = time.Now()
			if err := s.refresh(ctx, a); err != nil && ctx.Err() == nil && !errors.Is(err, ErrSocialUnavailable) {
				s.log.WarnContext(ctx, "соцсети: обновление", "account", a.ID, "platform", a.Platform, "err", err)
			}
		}
		if len(accs) < s.cfg.Batch || ctx.Err() != nil {
			return
		}
	}
}

// nextRefresh — через RefreshEvery ± 1/12 (разброс: обновления не собираются в одну минуту).
func (s *Social) nextRefresh(now time.Time) time.Time {
	span := int64(s.cfg.RefreshEvery / 6)
	return now.Add(s.cfg.RefreshEvery - s.cfg.RefreshEvery/12 + time.Duration(s.randInt(span)))
}

// backoff — пауза после временного сбоя: 15 мин × 2^n, не больше 6 ч, +10% разброса.
func (s *Social) backoff(failures int) time.Duration {
	d := 15 * time.Minute
	for i := 0; i < failures && d < 6*time.Hour; i++ {
		d *= 2
	}
	d = min(d, 6*time.Hour)
	return d + time.Duration(s.randInt(int64(d/10)))
}

func (s *Social) randInt(n int64) int64 {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(s.rnd, big.NewInt(n))
	if err != nil {
		return n / 2
	}
	return v.Int64()
}

// ===== отвязка =====

// Unlink — убрать аккаунт из анкеты. Последний аккаунт доступа — токен отзывается у площадки и удаляется.
func (s *Social) Unlink(ctx context.Context, u domain.User, id int64) error {
	a, err := s.repo.DeleteAccount(ctx, u.ID, id)
	if err != nil {
		return err
	}
	if a.CredentialID == 0 {
		return nil
	}
	n, err := s.repo.CredentialAccounts(ctx, a.CredentialID)
	if err != nil || n > 0 {
		return err
	}
	cred, err := s.repo.Credential(ctx, a.CredentialID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if k, ok := s.vs[a.Platform].(TokenKeeper); ok {
		if tok, err := s.openCredential(cred); err == nil {
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			if err := k.Revoke(rctx, tok); err != nil {
				s.log.WarnContext(ctx, "соцсети: отзыв токена у площадки", "platform", a.Platform, "err", err)
			}
			cancel()
		}
	}
	return s.repo.DeleteCredential(ctx, cred.ID)
}

// ===== токены =====

func (s *Social) sealCredential(userID int64, p domain.SocialPlatform, subject string, t Tokens) (domain.SocialCredential, error) {
	raw, err := json.Marshal(t)
	if err != nil {
		return domain.SocialCredential{}, err
	}
	sealed, err := s.cipher.Seal(raw, credAAD(userID, p, subject))
	if err != nil {
		return domain.SocialCredential{}, err
	}
	c := domain.SocialCredential{UserID: userID, Platform: p, Subject: subject, Secret: sealed}
	if !t.AccessExpiresAt.IsZero() {
		at := t.AccessExpiresAt
		c.AccessExpiresAt = &at
	}
	if !t.RefreshExpiresAt.IsZero() {
		rt := t.RefreshExpiresAt
		c.RefreshExpiresAt = &rt
	}
	return c, nil
}

func (s *Social) openCredential(c domain.SocialCredential) (Tokens, error) {
	if s.cipher == nil {
		return Tokens{}, errors.New("нет ключа шифрования")
	}
	raw, err := s.cipher.Open(c.Secret, credAAD(c.UserID, c.Platform, c.Subject))
	if err != nil {
		return Tokens{}, err
	}
	var t Tokens
	if err := json.Unmarshal(raw, &t); err != nil {
		return Tokens{}, err
	}
	return t, nil
}

// credAAD / stateAAD — с чем связан шифротекст: подменить строку в базе на чужую нельзя.
func credAAD(userID int64, p domain.SocialPlatform, subject string) []byte {
	return []byte("cred:" + strconv.FormatInt(userID, 10) + ":" + string(p) + ":" + subject)
}

func stateAAD(userID int64, p domain.SocialPlatform) []byte {
	return []byte("state:" + strconv.FormatInt(userID, 10) + ":" + string(p))
}

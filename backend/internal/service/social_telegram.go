package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ohvat/backend/internal/domain"
)

// ===== Telegram: канал подтверждается ботом-админом =====
// 1) «Подключить Telegram» → ссылка t.me/<бот>?start=ch_<токен> → в боте «Привязать» (Telegram привязывается к
//    аккаунту как способ входа). 2) Автор добавляет бота админом канала (права не нужны) → Telegram присылает
//    my_chat_member → бот проверяет getChatAdministrators (автор — создатель или админ) и getChatMemberCount.

// SocialStartPrefix — начало параметра /start для привязки Telegram (до 64 символов: ch_ + 43).
const SocialStartPrefix = "ch_"

// ParseSocialStart — токен из «ch_<токен>».
func ParseSocialStart(arg string) (string, bool) {
	if len(arg) != len(SocialStartPrefix)+43 || arg[:len(SocialStartPrefix)] != SocialStartPrefix {
		return "", false
	}
	tok := arg[len(SocialStartPrefix):]
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return "", false
		}
	}
	return tok, true
}

// ChannelEvent — бота добавили в канал, повысили, понизили или удалили.
type ChannelEvent struct {
	ChatID   int64
	Title    string
	Username string
	Type     string // channel | group | supergroup
	Status   string // administrator | member | left | kicked | restricted
	From     domain.TelegramProfile
}

// Итоги события канала для бота.
const (
	ChannelVerified  = "verified"   // канал подтверждён
	ChannelPending   = "pending"    // проверка не прошла временно — повторим сами
	ChannelNotLinked = "not_linked" // Telegram не привязан к аккаунту на сайте
	ChannelNotAdmin  = "not_admin"  // добавивший — не админ канала
	ChannelRemoved   = "removed"    // бота убрали — отметки сняты
	ChannelIgnored   = ""
)

// ChannelResult — что ответить в личный чат добавившему.
type ChannelResult struct {
	Outcome string
	Account *domain.SocialAccount
}

// TelegramLink — к какому аккаунту сайта привязывается Telegram (бот спрашивает подтверждение).
type TelegramLink struct {
	Name string
}

// TelegramLinkResult — привязка готова; Verified — каналы, куда бота добавили раньше (подтверждены сразу).
type TelegramLinkResult struct {
	Verified []domain.SocialAccount
}

// TelegramLinkInfo — неистёкшая ссылка привязки: имя аккаунта на сайте (для вопроса «Привязать?»).
func (s *Social) TelegramLinkInfo(ctx context.Context, token string) (TelegramLink, error) {
	st, err := s.repo.PeekState(ctx, hashToken(token), domain.PlatformTelegram, s.clock.Now())
	if errors.Is(err, domain.ErrNotFound) {
		return TelegramLink{}, ErrSocialState
	}
	if err != nil {
		return TelegramLink{}, err
	}
	u, err := s.users.UserByID(ctx, st.UserID)
	if err != nil {
		return TelegramLink{}, err
	}
	return TelegramLink{Name: u.Name}, nil
}

// CancelTelegramLink — «Это не я»: ссылка гасится.
func (s *Social) CancelTelegramLink(ctx context.Context, token string) error {
	_, err := s.repo.TakeState(ctx, hashToken(token), domain.PlatformTelegram, s.clock.Now())
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	return err
}

// LinkTelegram — «Привязать» в боте: Telegram становится способом входа аккаунта (если ещё не привязан к другому);
// каналы, куда этот человек уже добавил бота, проверяются сразу.
func (s *Social) LinkTelegram(ctx context.Context, token string, p domain.TelegramProfile) (TelegramLinkResult, error) {
	if p.ID <= 0 || p.IsBot {
		return TelegramLinkResult{}, ErrSocialState
	}
	st, err := s.repo.TakeState(ctx, hashToken(token), domain.PlatformTelegram, s.clock.Now())
	if errors.Is(err, domain.ErrNotFound) {
		return TelegramLinkResult{}, ErrSocialState
	}
	if err != nil {
		return TelegramLinkResult{}, err
	}
	subject := strconv.FormatInt(p.ID, 10)
	owner, err := s.users.UserByIdentity(ctx, domain.ProviderTelegram, subject)
	switch {
	case err == nil && owner.ID != st.UserID:
		return TelegramLinkResult{}, ErrTelegramTaken
	case errors.Is(err, domain.ErrNotFound):
		if err := s.users.LinkIdentity(ctx, st.UserID, domain.Identity{Provider: domain.ProviderTelegram, Subject: subject}); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				return TelegramLinkResult{}, ErrTelegramTaken
			}
			return TelegramLinkResult{}, err
		}
		s.log.InfoContext(ctx, "соцсети: Telegram привязан", "user", st.UserID)
	case err != nil:
		return TelegramLinkResult{}, err
	}
	out := TelegramLinkResult{}
	chans, err := s.repo.TelegramChannelsAddedBy(ctx, p.ID)
	if err != nil {
		return out, err
	}
	for _, ch := range chans {
		r, err := s.verifyChannel(ctx, st.UserID, p.ID, ch)
		if err != nil {
			s.log.WarnContext(ctx, "соцсети: канал после привязки", "chat", ch.ChatID, "err", err)
			continue
		}
		if r.Outcome == ChannelVerified {
			out.Verified = append(out.Verified, *r.Account)
		}
	}
	return out, nil
}

// ChannelChanged — событие my_chat_member канала или группы: бота добавили (админом; в группе можно и участником) →
// проверка, что добавивший — создатель или админ, и число подписчиков; бота убрали → отметки «Подтверждено» снимаются.
func (s *Social) ChannelChanged(ctx context.Context, ev ChannelEvent) (ChannelResult, error) {
	if ev.ChatID == 0 || ev.From.ID <= 0 || ev.From.IsBot {
		return ChannelResult{}, nil
	}
	ch := domain.TelegramChannel{ChatID: ev.ChatID, Title: ev.Title, Username: ev.Username, Type: ev.Type, AddedBy: ev.From.ID, BotStatus: ev.Status}
	if err := s.repo.SaveTelegramChannel(ctx, ch); err != nil {
		return ChannelResult{}, err
	}
	if ev.Status != "administrator" && ev.Status != "member" { // убрали или ограничили (в канале бот — только админ)
		accs, err := s.repo.AccountsByExternal(ctx, domain.PlatformTelegram, strconv.FormatInt(ev.ChatID, 10))
		if err != nil {
			return ChannelResult{}, err
		}
		for _, a := range accs {
			if a.Status == domain.SocialFailed {
				continue
			}
			if err := s.repo.SetFailed(ctx, a.ID, failReason(domain.PlatformTelegram, ErrSocialRevoked)); err != nil {
				return ChannelResult{}, err
			}
		}
		if len(accs) == 0 {
			return ChannelResult{}, nil
		}
		return ChannelResult{Outcome: ChannelRemoved}, nil
	}
	if _, ok := s.vs[domain.PlatformTelegram]; !ok {
		return ChannelResult{}, nil
	}
	u, err := s.users.UserByIdentity(ctx, domain.ProviderTelegram, strconv.FormatInt(ev.From.ID, 10))
	if errors.Is(err, domain.ErrNotFound) {
		return ChannelResult{Outcome: ChannelNotLinked}, nil
	}
	if err != nil {
		return ChannelResult{}, err
	}
	return s.verifyChannel(ctx, u.ID, ev.From.ID, ch)
}

// verifyChannel — getChatAdministrators + getChatMemberCount; временный сбой — аккаунт «ждёт проверки»,
// фоновая задача повторит.
func (s *Social) verifyChannel(ctx context.Context, userID, tgUserID int64, ch domain.TelegramChannel) (ChannelResult, error) {
	v, ok := s.vs[domain.PlatformTelegram]
	if !ok {
		return ChannelResult{}, ErrSocialDisabled
	}
	now := s.clock.Now()
	cctx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	res, err := v.Check(cctx, VerifyCheck{ChatID: ch.ChatID, TgUserID: tgUserID})
	switch {
	case errors.Is(err, ErrSocialNotOwner):
		return ChannelResult{Outcome: ChannelNotAdmin}, nil
	case errors.Is(err, ErrSocialUnavailable):
		next := now.Add(5 * time.Minute)
		a, serr := s.repo.SaveAccount(ctx, domain.SocialAccount{
			UserID: userID, Platform: domain.PlatformTelegram, ExternalID: strconv.FormatInt(ch.ChatID, 10), Kind: ch.Kind(),
			Handle: ch.Username, Title: ch.Title, URL: telegramURL(ch.Username), Status: domain.SocialPending, Method: domain.MethodBot,
			NextRefreshAt: &next,
		}, now)
		if serr != nil {
			return ChannelResult{}, serr
		}
		return ChannelResult{Outcome: ChannelPending, Account: &a}, nil
	case err != nil:
		return ChannelResult{}, err
	case len(res.Channels) == 0:
		return ChannelResult{Outcome: ChannelNotAdmin}, nil
	}
	a, err := s.repo.SaveAccount(ctx, s.verifiedAccount(userID, domain.PlatformTelegram, domain.MethodBot, 0, res.Channels[0], now), now)
	if err != nil {
		return ChannelResult{}, err
	}
	s.log.InfoContext(ctx, "соцсети: канал Telegram подтверждён", "user", userID, "chat", ch.ChatID)
	return ChannelResult{Outcome: ChannelVerified, Account: &a}, nil
}

func telegramURL(username string) string {
	if username == "" {
		return ""
	}
	return "https://t.me/" + username
}

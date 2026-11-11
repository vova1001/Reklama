package telegram

import (
	"context"
	"errors"
	"testing"

	"ohvat/backend/internal/domain"
	tggw "ohvat/backend/internal/gateway/telegram"
	"ohvat/backend/internal/service"
)

type fakeAPI struct {
	chat    tggw.Chat
	admins  []tggw.ChatMember
	count   int64
	chatErr error
	admErr  error
	cntErr  error
}

func (f *fakeAPI) GetChat(context.Context, int64) (tggw.Chat, error) { return f.chat, f.chatErr }
func (f *fakeAPI) GetChatAdministrators(context.Context, int64) ([]tggw.ChatMember, error) {
	return f.admins, f.admErr
}
func (f *fakeAPI) GetChatMemberCount(context.Context, int64) (int64, error) { return f.count, f.cntErr }

func owner(id int64, status string) tggw.ChatMember {
	return tggw.ChatMember{Status: status, User: tggw.User{ID: id}}
}

func TestCheckChannelAndGroup(t *testing.T) {
	ctx := context.Background()
	api := &fakeAPI{chat: tggw.Chat{ID: -1001, Type: "channel", Title: "Канал", Username: "chan"},
		admins: []tggw.ChatMember{owner(1, "administrator"), owner(777, "creator")}, count: 5000}
	v := New(api, "reklama_auth_bot")
	if v.Platform() != domain.PlatformTelegram || v.Method() != domain.MethodBot {
		t.Fatal("площадка")
	}
	st, _ := v.Start(ctx, service.VerifyStart{State: "TOKEN"})
	if st.URL != "https://t.me/reklama_auth_bot?start=ch_TOKEN" {
		t.Error(st.URL)
	}
	r, err := v.Check(ctx, service.VerifyCheck{ChatID: -1001, TgUserID: 777})
	if err != nil || r.Subject != "777" || len(r.Channels) != 1 {
		t.Fatalf("%v %+v", err, r)
	}
	c := r.Channels[0]
	if c.ExternalID != "-1001" || c.Kind != domain.SocialChannel || c.URL != "https://t.me/chan" || *c.Stats.Followers != 5000 || c.Title != "Канал" {
		t.Errorf("%+v", c)
	}
	// закрытая группа без @имени: участники, без ссылки
	api.chat = tggw.Chat{ID: -1002, Type: "supergroup", Title: "Чат"}
	api.admins = []tggw.ChatMember{owner(777, "administrator")}
	api.count = 42
	ch, err := v.Refresh(ctx, service.RefreshInput{Account: domain.SocialAccount{ExternalID: "-1002"}, TgUserID: 777})
	if err != nil || ch.Kind != domain.SocialGroup || ch.URL != "" || *ch.Stats.Followers != 42 {
		t.Fatalf("%v %+v", err, ch)
	}
}

func TestCheckErrors(t *testing.T) {
	ctx := context.Background()
	base := func() *fakeAPI {
		return &fakeAPI{chat: tggw.Chat{ID: -1, Type: "channel"}, admins: []tggw.ChatMember{owner(777, "creator")}, count: 1}
	}
	cases := map[string]struct {
		mut  func(*fakeAPI)
		in   service.VerifyCheck
		want error
	}{
		"не админ":        {func(f *fakeAPI) { f.admins = []tggw.ChatMember{owner(5, "creator")} }, service.VerifyCheck{ChatID: -1, TgUserID: 777}, service.ErrSocialNotOwner},
		"просто участник": {func(f *fakeAPI) { f.admins = []tggw.ChatMember{owner(777, "member")} }, service.VerifyCheck{ChatID: -1, TgUserID: 777}, service.ErrSocialNotOwner},
		"без владельца":   {func(*fakeAPI) {}, service.VerifyCheck{ChatID: -1}, service.ErrSocialNotOwner},
		"личный чат":      {func(f *fakeAPI) { f.chat.Type = "private" }, service.VerifyCheck{ChatID: -1, TgUserID: 777}, service.ErrSocialNoChannel},
		"бот удалён": {func(f *fakeAPI) {
			f.chatErr = &tggw.APIError{Method: "getChat", Code: 400, Description: "Bad Request: chat not found"}
		}, service.VerifyCheck{ChatID: -1, TgUserID: 777}, service.ErrSocialRevoked},
		"бот выгнан": {func(f *fakeAPI) {
			f.admErr = &tggw.APIError{Code: 403, Description: "Forbidden: bot was kicked"}
		}, service.VerifyCheck{ChatID: -1, TgUserID: 777}, service.ErrSocialRevoked},
		"лимит": {func(f *fakeAPI) { f.cntErr = &tggw.APIError{Code: 429, Description: "Too Many Requests"} },
			service.VerifyCheck{ChatID: -1, TgUserID: 777}, service.ErrSocialUnavailable},
		"сеть": {func(f *fakeAPI) { f.chatErr = &tggw.NetError{Method: "getChat", Err: context.DeadlineExceeded} },
			service.VerifyCheck{ChatID: -1, TgUserID: 777}, service.ErrSocialUnavailable},
	}
	for name, c := range cases {
		api := base()
		c.mut(api)
		if _, err := New(api, "b").Check(ctx, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	api := base()
	api.chatErr = &tggw.APIError{Code: 409, Description: "x"}
	if _, err := New(api, "b").Check(ctx, service.VerifyCheck{ChatID: -1, TgUserID: 777}); err == nil ||
		errors.Is(err, service.ErrSocialRevoked) || errors.Is(err, service.ErrSocialUnavailable) {
		t.Errorf("прочее: %v", err)
	}
	if _, err := New(base(), "b").Refresh(ctx, service.RefreshInput{Account: domain.SocialAccount{ExternalID: "x"}, TgUserID: 777}); !errors.Is(err, service.ErrSocialNoChannel) {
		t.Errorf("id: %v", err)
	}
}

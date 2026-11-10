package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ohvat/backend/internal/domain"
)

func newProfilesEnv(t *testing.T) (*Profiles, *memRepo, domain.User) {
	t.Helper()
	repo := newMem()
	u, err := repo.CreateUserWithIdentity(context.Background(), domain.NewUser{Name: "Анна"}, domain.Identity{Provider: "email", Subject: "a@x.ru"})
	if err != nil {
		t.Fatal(err)
	}
	return NewProfiles(repo, repo), repo, u
}

func wantFields(t *testing.T, err error, fields ...string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("ждали ошибки полей %v, получили %v", fields, err)
	}
	for _, f := range fields {
		if ve.Fields[f] == "" {
			t.Errorf("нет ошибки поля %s: %v", f, ve.Fields)
		}
	}
}

func TestOverviewChecklist(t *testing.T) {
	ctx := context.Background()
	s, repo, u := newProfilesEnv(t)
	o, err := s.Overview(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	if o.Completion != 0 || len(o.Checklist) != 3 || o.Profiles.Business != nil || o.Profiles.Creator != nil {
		t.Fatalf("новый аккаунт: %+v", o)
	}
	if strings.Join(o.Providers, ",") != "email" {
		t.Errorf("способы входа: %v", o.Providers)
	}

	u, _ = s.SaveBasics(ctx, u, domain.Basics{Name: "Анна", City: "Казань"})
	if _, err := s.SaveBusiness(ctx, u, domain.BusinessProfile{Name: "Кофейня", City: "Казань", Spheres: []string{"food"}}); err != nil {
		t.Fatal(err)
	}
	o, _ = s.Overview(ctx, u)
	if o.Completion != 66 || !o.Checklist[0].Done || !o.Checklist[1].Done || o.Checklist[2].Done || o.Profiles.Business == nil {
		t.Fatalf("общие + профиль бизнеса: %+v", o)
	}
	// в режиме автора профиль режима не заполнен — прогресс считается для активного режима
	o, err = s.SwitchMode(ctx, u, " creator ")
	if err != nil || o.User.ActiveMode != domain.ModeCreator || o.Checklist[1].Done || o.Completion != 33 {
		t.Fatalf("режим автора: %v %+v", err, o)
	}
	repo.provs[u.ID] = append(repo.provs[u.ID], domain.ProviderTelegram)
	if o, _ = s.Overview(ctx, o.User); !o.Checklist[2].Done {
		t.Errorf("Telegram привязан: %+v", o.Checklist)
	}

	for _, name := range []string{"Providers", "BusinessProfile", "CreatorProfile"} {
		repo.fail[name] = errBoom
		if _, err := s.Overview(ctx, u); !errors.Is(err, errBoom) {
			t.Errorf("сбой %s: %v", name, err)
		}
		delete(repo.fail, name)
	}
}

func TestSwitchMode(t *testing.T) {
	ctx := context.Background()
	s, repo, u := newProfilesEnv(t)
	for _, bad := range []string{"", "admin", "Business"} {
		_, err := s.SwitchMode(ctx, u, bad)
		wantFields(t, err, "mode")
	}
	// тот же режим — без записи в базу
	repo.fail["SetMode"] = errBoom
	if o, err := s.SwitchMode(ctx, u, domain.ModeBusiness); err != nil || o.User.ActiveMode != domain.ModeBusiness {
		t.Fatalf("тот же режим: %v", err)
	}
	if _, err := s.SwitchMode(ctx, u, domain.ModeCreator); !errors.Is(err, errBoom) {
		t.Fatalf("сбой базы: %v", err)
	}
	delete(repo.fail, "SetMode")
	o, err := s.SwitchMode(ctx, u, domain.ModeCreator)
	if err != nil || o.User.ActiveMode != domain.ModeCreator || repo.users[u.ID].ActiveMode != domain.ModeCreator {
		t.Fatalf("%v %+v", err, o)
	}
}

func TestSaveBasics(t *testing.T) {
	ctx := context.Background()
	s, repo, u := newProfilesEnv(t)
	got, err := s.SaveBasics(ctx, u, domain.Basics{Name: "  Анна   К ", City: " Казань "})
	if err != nil || got.Name != "Анна К" || got.City != "Казань" || !got.Onboarded() {
		t.Fatalf("%v %+v", err, got)
	}
	_, err = s.SaveBasics(ctx, u, domain.Basics{Name: "А", City: strings.Repeat("я", 81)})
	wantFields(t, err, "name", "city")
	repo.fail["UpdateBasics"] = errBoom
	if _, err := s.SaveBasics(ctx, u, domain.Basics{Name: "Анна", City: "Казань"}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestSaveBusiness(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		in     domain.BusinessProfile
		fields []string
	}{
		{name: "пусто", in: domain.BusinessProfile{}, fields: []string{"name", "city", "spheres"}},
		{name: "6 сфер", in: domain.BusinessProfile{Name: "Ок", City: "Ок", Spheres: []string{"a", "b", "c", "d", "e", "f"}}, fields: []string{"spheres"}},
		{name: "сфера с мусором", in: domain.BusinessProfile{Name: "Ок", City: "Ок", Spheres: []string{"DROP TABLE"}}, fields: []string{"spheres"}},
		{name: "сфера не из справочника", in: domain.BusinessProfile{Name: "Ок", City: "Ок", Spheres: []string{"food", "nope"}}, fields: []string{"spheres"}},
		{name: "длинное название", in: domain.BusinessProfile{Name: strings.Repeat("я", 81), City: "Ок", Spheres: []string{"food"}}, fields: []string{"name"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _, u := newProfilesEnv(t)
			_, err := s.SaveBusiness(ctx, u, c.in)
			wantFields(t, err, c.fields...)
		})
	}
	t.Run("успех и повторное сохранение", func(t *testing.T) {
		s, _, u := newProfilesEnv(t)
		if p, err := s.Business(ctx, u); err != nil || p != nil {
			t.Fatalf("до заполнения: %v %+v", err, p)
		}
		got, err := s.SaveBusiness(ctx, u, domain.BusinessProfile{Name: "  Кофейня   «Зерно» ", City: "Казань", Spheres: []string{"food", "food", " beauty "}})
		if err != nil || got.Name != "Кофейня «Зерно»" || strings.Join(got.Spheres, ",") != "food,beauty" {
			t.Fatalf("%v %+v", err, got)
		}
		if _, err := s.SaveBusiness(ctx, u, domain.BusinessProfile{Name: "Зерно", City: "Казань", Spheres: []string{"sport"}}); err != nil {
			t.Fatal(err)
		}
		p, err := s.Business(ctx, u)
		if err != nil || p == nil || p.Name != "Зерно" || p.Spheres[0] != "sport" {
			t.Fatalf("%v %+v", err, p)
		}
	})
	t.Run("сбои базы", func(t *testing.T) {
		s, repo, u := newProfilesEnv(t)
		ok := domain.BusinessProfile{Name: "Ок", City: "Ок", Spheres: []string{"food"}}
		for _, name := range []string{"CountCategories", "SaveBusinessProfile"} {
			repo.fail[name] = errBoom
			if _, err := s.SaveBusiness(ctx, u, ok); !errors.Is(err, errBoom) {
				t.Errorf("%s: %v", name, err)
			}
			delete(repo.fail, name)
		}
		repo.fail["BusinessProfile"] = errBoom
		if _, err := s.Business(ctx, u); !errors.Is(err, errBoom) {
			t.Error(err)
		}
	})
}

func TestSaveCreator(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		in     domain.CreatorProfile
		fields []string
	}{
		{name: "пусто", in: domain.CreatorProfile{}, fields: []string{"name", "city", "spheres", "channels"}},
		{name: "плохая ссылка", in: domain.CreatorProfile{Name: "Ок", City: "Ок", Spheres: []string{"food"}, Channels: []string{"javascript:alert(1)"}}, fields: []string{"channels"}},
		{name: "ссылка с логином", in: domain.CreatorProfile{Name: "Ок", City: "Ок", Spheres: []string{"food"}, Channels: []string{"https://u:p@vk.com/x"}}, fields: []string{"channels"}},
		{name: "6 каналов", in: domain.CreatorProfile{Name: "Ок", City: "Ок", Spheres: []string{"food"}, Channels: []string{"a.ru", "b.ru", "c.ru", "d.ru", "e.ru", "f.ru"}}, fields: []string{"channels"}},
		{name: "тема не из справочника", in: domain.CreatorProfile{Name: "Ок", City: "Ок", Spheres: []string{"nope"}, Channels: []string{"@alina_tg"}}, fields: []string{"spheres"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _, u := newProfilesEnv(t)
			_, err := s.SaveCreator(ctx, u, c.in)
			wantFields(t, err, c.fields...)
		})
	}
	t.Run("успех: ссылки нормализуются", func(t *testing.T) {
		s, _, u := newProfilesEnv(t)
		got, err := s.SaveCreator(ctx, u, domain.CreatorProfile{Name: "Алина", City: "Москва", Spheres: []string{"beauty"},
			Channels: []string{"youtube.com/@alina", "@alina_tg", "http://vk.com/alina", "https://vk.com/alina"}})
		want := "https://youtube.com/@alina,@alina_tg,https://vk.com/alina"
		if err != nil || strings.Join(got.Channels, ",") != want {
			t.Fatalf("%v %+v", err, got)
		}
		p, err := s.Creator(ctx, u)
		if err != nil || p == nil || p.Name != "Алина" {
			t.Fatalf("%v %+v", err, p)
		}
	})
	t.Run("сбои базы", func(t *testing.T) {
		s, repo, u := newProfilesEnv(t)
		if p, err := s.Creator(ctx, u); err != nil || p != nil {
			t.Fatalf("до заполнения: %v %+v", err, p)
		}
		ok := domain.CreatorProfile{Name: "Ок", City: "Ок", Spheres: []string{"food"}, Channels: []string{"@alina_tg"}}
		for _, name := range []string{"CountCategories", "SaveCreatorProfile"} {
			repo.fail[name] = errBoom
			if _, err := s.SaveCreator(ctx, u, ok); !errors.Is(err, errBoom) {
				t.Errorf("%s: %v", name, err)
			}
			delete(repo.fail, name)
		}
		repo.fail["CreatorProfile"] = errBoom
		if _, err := s.Creator(ctx, u); !errors.Is(err, errBoom) {
			t.Error(err)
		}
	})
}

func TestProfilesFilled(t *testing.T) {
	p := domain.Profiles{Business: &domain.BusinessProfile{}}
	if !p.Filled(domain.ModeBusiness) || p.Filled(domain.ModeCreator) || p.Filled("x") {
		t.Errorf("%+v", p)
	}
}

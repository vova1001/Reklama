package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"ohvat/backend/internal/domain"
)

func TestNormCategory(t *testing.T) {
	for in, want := range map[string]string{
		"  Ёлки-Палки  ": "елки палки", "КОФЕ  и   чай": "кофе и чай", "Еда & кафе!": "еда кафе", "": "",
	} {
		if got := NormCategory(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestCategoryTitleCheck(t *testing.T) {
	for in, ok := range map[string]bool{
		"Йога": true, "Кофе с собой": true, "3D-печать": true, "я": false, "1234": false,
		"Очень длинное название категории больше сорока": false,
		"site.ru": false, "https://x": false, "www.кофе": false, "@кофе": false,
		"Хуйня": false, "блядь": false, "Пиздец": false, "сука": false,
		"Хлебать": true, "Сукно": true, "Мудрость": true, "Психология": true,
	} {
		if got := checkCategoryTitle(in) == ""; got != ok {
			t.Errorf("%q: %v", in, got)
		}
	}
}

func TestCategoryCreate(t *testing.T) {
	cats := newMemCats()
	s := NewCategories(cats)
	s.rnd = bytes.NewReader(bytes.Repeat([]byte{1}, 1024))
	ctx := context.Background()
	u := domain.User{ID: 5}

	// совпадает с общей без учёта регистра и пробелов — вернуть общую
	c, created, err := s.Create(ctx, u, "  еда И  КАФЕ ")
	if err != nil || created || c.Slug != "food" {
		t.Fatalf("общая: %v %v %+v", err, created, c)
	}
	// новая — на проверке, с заглавной буквы, slug u-…
	c, created, err = s.Create(ctx, u, "йога  для беременных")
	if err != nil || !created || c.Title != "Йога для беременных" || c.Status != domain.CategoryPending || c.Slug != "u-010101010101" {
		t.Fatalf("новая: %v %v %+v", err, created, c)
	}
	// повтор у того же автора — та же
	again, created, err := s.Create(ctx, u, "ЙОГА ДЛЯ БЕРЕМЕННЫХ")
	if err != nil || created || again.ID != c.ID {
		t.Fatalf("повтор: %v %v %+v", err, created, again)
	}
	// видна автору в задании, но не другому
	if n, custom, _ := cats.Usable(ctx, 5, []string{c.Slug, "food"}); n != 2 || custom != 1 {
		t.Errorf("автору: %d %d", n, custom)
	}
	if n, _, _ := cats.Usable(ctx, 6, []string{c.Slug}); n != 0 {
		t.Error("чужая на проверке не годится")
	}
	// плохое название
	if _, _, err := s.Create(ctx, u, "vk.com/x"); fields(t, err)["title"] == "" {
		t.Error("ссылка")
	}
	// лимит своих на проверке
	for i := 0; i < maxCustomPending-1; i++ {
		if _, _, err := s.Create(ctx, u, fmt.Sprintf("Тема %c", 'а'+rune(i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Create(ctx, u, "Ещё одна"); fields(t, err)["title"] == "" {
		t.Error("лимит своих")
	}
	// список: общие + свои
	l, _ := s.List(ctx, u)
	if len(l.Common) != 3 || len(l.Own) != maxCustomPending {
		t.Errorf("%d %d", len(l.Common), len(l.Own))
	}
}

func TestCategorySimilar(t *testing.T) {
	cats := newMemCats()
	s := NewCategories(cats)
	ctx := context.Background()
	u := domain.User{ID: 5}
	cats.list = append(cats.list, domain.CategoryInfo{ID: 9, Slug: "u-1", Title: "Кофейни", Status: domain.CategoryPending, AuthorID: 5},
		domain.CategoryInfo{ID: 10, Slug: "u-2", Title: "Кофе чужой", Status: domain.CategoryPending, AuthorID: 6})
	check := func(q string, want ...string) {
		t.Helper()
		got, err := s.Similar(ctx, u, q)
		if err != nil {
			t.Fatal(err)
		}
		var slugs []string
		for _, c := range got {
			slugs = append(slugs, c.Slug)
		}
		if fmt.Sprint(slugs) != fmt.Sprint(want) {
			t.Errorf("%q: %v, ждали %v", q, slugs, want)
		}
	}
	check("еда", "food")
	check("кафе", "food", "u-1") // «кофейни» — опечатка в одну букву
	check("коф", "u-1")          // своя на проверке видна, чужая — нет
	check("спрот", "sport")      // опечатка
	check("красотаа", "beauty")  // лишняя буква
	check("  ")
	check("авто")
}

func TestCategoryModeration(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	ctx := context.Background()
	s := NewCategories(e.cats)
	c, _, _ := s.Create(ctx, author, "Настольные игры")
	// своя в анкете: до 3
	for i := 0; i < 3; i++ {
		_, _, _ = s.Create(ctx, author, fmt.Sprintf("Своя %c", 'а'+rune(i)))
	}
	own, _ := s.List(ctx, author)
	in := fullCard()
	in.Topics = nil
	for _, o := range own.Own {
		in.Topics = append(in.Topics, o.Slug)
	}
	if f := fields(t, func() error { _, err := e.cards.Save(ctx, author, in); return err }()); f["topics"] == "" {
		t.Error("своих больше 3")
	}
	in.Topics = in.Topics[:3]
	if _, err := e.cards.Save(ctx, author, in); err != nil {
		t.Errorf("3 своих: %v", err)
	}
	// одобрить — станет общей; повторно — нельзя
	st, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectCategory, ID: c.ID, ModeratorID: 1, Approve: true})
	if err != nil || st != domain.CategoryApproved {
		t.Fatalf("%v %v", err, st)
	}
	if _, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectCategory, ID: c.ID, ModeratorID: 1, Approve: true}); !errors.Is(err, ErrWrongStatus) {
		t.Errorf("дважды: %v", err)
	}
	if n, _, _ := e.cats.Usable(ctx, 999, []string{c.Slug}); n != 1 {
		t.Error("общая — всем")
	}
	if _, err := e.mod.Decide(ctx, Decision{Subject: domain.SubjectCategory, ID: 404, ModeratorID: 1, Approve: true}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("нет: %v", err)
	}
}

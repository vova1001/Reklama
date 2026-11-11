package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestNormFormat(t *testing.T) {
	for in, want := range map[string]string{
		"reels": "reels", " Reels ": "reels", "VK  клипы": "vk_clips", "vk_clips": "vk_clips", "ТИКТОК": "ТИКТОК", "TIKTOK": "tiktok",
		"stories": "Stories", "  прямой   эфир ": "Прямой эфир", "обзор кофе": "Обзор кофе", "": "", "  ": "",
	} {
		got, msg := normFormat(in)
		if got != want || msg != "" {
			t.Errorf("%q → %q %q, ждали %q", in, got, msg, want)
		}
	}
	for _, bad := range []string{"я", "https://x.ru", "12345", "длинное название формата больше тридцати"} {
		if got, msg := normFormat(bad); msg == "" || got != "" {
			t.Errorf("%q: ждали ошибку, а %q", bad, got)
		}
	}
}

func TestCheckFormats(t *testing.T) {
	ve := &ValidationError{}
	got := checkFormats(ve, []string{"Reels", "reels", "stories", "STORIES", " ", "Обзор кофе"})
	if ve.orNil() != nil || !reflect.DeepEqual(got, []string{"reels", "Stories", "Обзор кофе"}) {
		t.Errorf("без повторов, в порядке выбора: %v %v", got, ve.Fields)
	}
	ve = &ValidationError{}
	checkFormats(ve, []string{"reels", "shorts", "tiktok", "Обзор"})
	if ve.Fields["formats"] != "Не больше 3 форматов" {
		t.Errorf("предел: %v", ve.Fields)
	}
}

// Несколько сфер (до 5, своих до 3) и свои форматы — сохраняются в порядке выбора и проходят проверку.
func TestTaskCategoriesAndFormats(t *testing.T) {
	e := newListingEnv(ListingLimits{})
	ctx := context.Background()
	in := fullTask()
	in.Categories = []string{"beauty", "food", "beauty"}
	in.Formats = []string{"reels", "  прямой эфир", "Распаковка"}
	d, err := e.tasks.Create(ctx, biz, in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Categories, []string{"beauty", "food"}) || !reflect.DeepEqual(d.Formats, []string{"reels", "Прямой эфир", "Распаковка"}) {
		t.Fatalf("сохранено: %+v %+v", d.Categories, d.Formats)
	}
	if _, err := e.tasks.Submit(ctx, biz, d.ID); err != nil {
		t.Fatalf("на проверку: %v", err)
	}

	// своих сфер в задании — не больше 3
	var own []string
	for _, title := range []string{"Настолки", "Квесты", "Йога", "Кальян"} {
		c, _, err := NewCategories(e.cats).Create(ctx, biz, title)
		if err != nil {
			t.Fatal(err)
		}
		own = append(own, c.Slug)
	}
	in.Categories = own[:3]
	if _, err := e.tasks.Create(ctx, biz, in); err != nil {
		t.Fatalf("3 своих: %v", err)
	}
	in.Categories = own
	if f := fields(t, func() error { _, err := e.tasks.Create(ctx, biz, in); return err }()); f["categories"] != "Своих сфер — не больше 3" {
		t.Errorf("4 своих: %v", f)
	}
}

type formatRepoFake struct {
	own []string
	err error
}

func (f formatRepoFake) OwnFormats(context.Context, int64) ([]string, error) { return f.own, f.err }

func TestFormatsSimilar(t *testing.T) {
	ctx := context.Background()
	s := NewFormats(formatRepoFake{own: []string{"Обзор кофе", "Reels"}})
	titles := func(q string) []string {
		fs, err := s.Similar(ctx, biz, q)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, f := range fs {
			out = append(out, f.Title)
		}
		return out
	}
	if got := titles("обз"); !reflect.DeepEqual(got, []string{"Обзор", "Обзор кофе"}) {
		t.Errorf("обз: %v", got)
	}
	if got := titles("reel"); len(got) != 1 || got[0] != "Reels" {
		t.Errorf("без повторов со списком: %v", got)
	}
	if got := titles("распоковка"); len(got) != 1 || got[0] != "Распаковка" {
		t.Errorf("опечатка: %v", got)
	}
	if got := titles(" !! "); len(got) != 0 {
		t.Errorf("пусто: %v", got)
	}
	fs, _ := s.Similar(ctx, biz, "кофе")
	if len(fs) != 1 || !fs[0].Own || fs[0].ID != "Обзор кофе" {
		t.Errorf("свой: %+v", fs)
	}
	if fs, _ := s.Similar(ctx, biz, "tik"); len(fs) != 1 || !fs[0].Builtin || fs[0].ID != "tiktok" {
		t.Errorf("из списка — slug: %+v", fs)
	}
	boom := errors.New("бд")
	if _, err := NewFormats(formatRepoFake{err: boom}).Similar(ctx, biz, "обзор"); !errors.Is(err, boom) {
		t.Errorf("ошибка хранилища: %v", err)
	}
}

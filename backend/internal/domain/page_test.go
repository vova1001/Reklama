package domain

import "testing"

func TestParseVideoLink(t *testing.T) {
	cases := []struct {
		in    string
		ok    bool
		embed string
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=10", true, "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"},
		{"youtu.be/dQw4w9WgXcQ", true, "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"},
		{"https://m.youtube.com/shorts/dQw4w9WgXcQ", true, "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"},
		{"https://www.youtube.com/embed/dQw4w9WgXcQ", true, "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ"},
		{"https://vk.com/video-123_456", true, "https://vk.com/video_ext.php?oid=-123&id=456"},
		{"https://vkvideo.ru/video123_456", true, "https://vk.com/video_ext.php?oid=123&id=456"},
		{"https://vk.com/clip-1_2", true, "https://vk.com/video_ext.php?oid=-1&id=2"},
		{"https://vk.com/club1?z=video-1_2%2Fabc", true, "https://vk.com/video_ext.php?oid=-1&id=2"},
		{"https://vk.com/video_ext.php?oid=-1&id=2&hash=abc123", true, "https://vk.com/video_ext.php?oid=-1&id=2&hash=abc123"},
		{"https://vk.com/video_ext.php?oid=-1&id=2&hash=<x>", false, ""},
		{"https://youtube.com.evil.com/watch?v=dQw4w9WgXcQ", false, ""},
		{"https://evil.com/youtu.be/dQw4w9WgXcQ", false, ""},
		{"javascript:alert(1)", false, ""},
		{"https://www.youtube.com/watch?v=short", false, ""},
		{"https://user@youtube.com/watch?v=dQw4w9WgXcQ", false, ""},
		{"https://youtube.com:8443/watch?v=dQw4w9WgXcQ", false, ""},
		{"ftp://youtu.be/dQw4w9WgXcQ", false, ""},
		{"", false, ""},
	}
	for _, c := range cases {
		v, ok := ParseVideoLink(c.in)
		if ok != c.ok || v.EmbedURL() != c.embed {
			t.Errorf("%q: ok=%v embed=%q, want %v %q", c.in, ok, v.EmbedURL(), c.ok, c.embed)
		}
	}
	if (PageVideo{Source: VideoYouTube, ID: "dQw4w9WgXcQ"}).WatchURL() != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Error("youtube watch")
	}
	if (PageVideo{Source: VideoVK, OID: "-1", ID: "2"}).WatchURL() != "https://vk.com/video-1_2" {
		t.Error("vk watch")
	}
	// подменённые части (не из ParseVideoLink) не встраиваются
	for _, v := range []PageVideo{{Source: VideoYouTube, ID: "x\"><script>"}, {Source: VideoVK, OID: "1&x=y", ID: "2"}, {Source: VideoFile, Media: "m"}} {
		if v.EmbedURL() != "" || v.WatchURL() != "" {
			t.Errorf("must not embed: %+v", v)
		}
	}
}

func TestSlugs(t *testing.T) {
	cases := map[string]string{
		"Кофейня «Зерно»":    "kofeynya-zerno",
		"  Анна  Смирнова  ": "anna-smirnova",
		"Щука & Ёж 24/7":     "shchuka-ezh-24-7",
		"Coffee-Shop!!":      "coffee-shop",
		"ООО «Подъезд»":      "ooo-podezd",
		"---":                "",
		"Очень длинное название кофейни на набережной реки": "ochen-dlinnoe-nazvanie-kofeyni-na-nabere",
	}
	for in, want := range cases {
		if got := SlugFrom(in); got != want {
			t.Errorf("SlugFrom(%q) = %q, want %q", in, got, want)
		}
	}
	for s, want := range map[string]bool{"abc": true, "a-b-c": true, "ab": false, "admin": false, "a--b": false, "-ab": false,
		"Abc": false, "кофе": false, "a_b": false, "abcdefghijabcdefghijabcdefghijabcdefghij": true, "abcdefghijabcdefghijabcdefghijabcdefghijk": false} {
		if ValidPageSlug(s) != want {
			t.Errorf("ValidPageSlug(%q) != %v", s, want)
		}
	}
}

func TestPageMediaAndBlocks(t *testing.T) {
	p := Page{Logo: "l", Cover: "c", Blocks: []PageBlock{
		{Type: PBPhotos, Photos: []string{"p1", "p2", "l"}},
		{Type: PBVideo, Videos: []PageVideo{{Source: VideoFile, Media: "v", Poster: "vp"}, {Source: VideoYouTube, ID: "x"}}},
	}}
	got := PageMedia(p)
	want := []string{"l", "c", "p1", "p2", "v", "vp"}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%v", got)
		}
	}
	if !PageBlockSingle(PBAbout) || PageBlockSingle(PBText) || !PageBlockAuto(PBCard) || PageBlockAuto(PBAbout) {
		t.Error("block flags")
	}
	if len(PageBlockTypes(PageBusiness)) != 6 || len(PageBlockTypes(PageCreator)) != 10 {
		t.Error("block types")
	}
	if !ValidPageKind(PageCreator) || ValidPageKind("shop") {
		t.Error("kinds")
	}
}

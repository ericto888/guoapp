package core

import (
	"encoding/base64"
	"strings"
	"testing"
)

func huangguaCardSample(alias, title, cover string) string {
	return `<article data-xpch="card-drama" class="group"><div class="ph-cover"><img src="/_img/` +
		base64.RawStdEncoding.EncodeToString([]byte(cover)) +
		`.jpeg" alt="` + title + `"><span>更新至12集</span></div><div class="truncate">` + title +
		`</div><p>69457热度</p><a href="/drama/` + alias + `">详情</a></article>`
}

func TestParseHuangguaCards(t *testing.T) {
	host := "https://abc.example.test"
	body := huangguaCardSample("dj-028472f1", "琼明神女录", "https://pic.example.test/cover/1.jpeg") +
		huangguaCardSample("18-1955-9f8f", "18岁太奶驾到", "https://pic.example.test/cover/2.jpeg")
	items := parseHuangguaCards(body, host)
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	first := items[0]
	if first.ID != "huanggua:dj-028472f1" || first.Source != sourceHuanggua || first.Title != "琼明神女录" {
		t.Fatalf("first item = %+v", first)
	}
	// 封面必须保留站内 /_img/ 解密代理路径，不能解码成原始 CDN 地址。
	wantCover := host + "/_img/" +
		base64.RawStdEncoding.EncodeToString([]byte("https://pic.example.test/cover/1.jpeg")) +
		".jpeg"
	if first.Cover != wantCover {
		t.Fatalf("cover = %q, want %q", first.Cover, wantCover)
	}
	if first.EpisodeCount != "12" || first.Views != "69457热度" {
		t.Fatalf("metadata missing: %+v", first)
	}
	if items[1].ID != "huanggua:18-1955-9f8f" {
		t.Fatalf("second id = %s", items[1].ID)
	}
}

func TestParseHuangguaCardsPrefersLazyCoverAndKeepsItPairedWithTitle(t *testing.T) {
	host := "https://abc.example.test"
	coverPath := "/_img/" + base64.RawStdEncoding.EncodeToString([]byte("https://pic.example.test/cover/lazy.jpeg")) + ".jpeg"
	body := `<article data-xpch="card-drama"><div class="ph-cover">` +
		`<img src="/static/loading.gif" alt="占位">` +
		`<img loading="lazy" src="/static/loading.gif" data-src="` + coverPath + `" alt="懒加载样本">` +
		`</div><a href="/drama/dj-lazy">详情</a></article>`
	items := parseHuangguaCards(body, host)
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	if items[0].Title != "懒加载样本" || items[0].Name != "懒加载样本" {
		t.Fatalf("title = %+v", items[0])
	}
	if items[0].Cover != host+coverPath {
		t.Fatalf("cover = %q, want %q", items[0].Cover, host+coverPath)
	}
}

func TestHuangguaMetaImageResolvesRelativeAddress(t *testing.T) {
	host := "https://abc.example.test"
	if got := huangguaMetaImage(`<meta property="og:image" content="/_img/a.jpeg">`, host); got != host+"/_img/a.jpeg" {
		t.Fatalf("og:image relative = %q", got)
	}
	if got := huangguaMetaImage(`<meta content="//pic.example.test/a.jpg" property="og:image">`, host); got != "https://pic.example.test/a.jpg" {
		t.Fatalf("og:image protocol relative = %q", got)
	}
	if got := huangguaMetaImage("<html></html>", host); got != "" {
		t.Fatalf("missing og:image = %q", got)
	}
}

func TestParseHuangguaCardsFillsLazyCoverFromNuxtPayload(t *testing.T) {
	host := "https://abc.example.test"
	coverURL := "https://pic.example.test/upload_01/cover/20261021/c1.jpeg"
	payload := `["unused",{"slug":2,"title":3,"cover":4,"total_episode_count":5,"heat_7d":6},` +
		`"dj-lazy","懒加载样本",{"url":7,"fallback_url":8},"12",69457,"` + coverURL + `","/static/poster.jpg"]`
	body := `<script id="__NUXT_DATA__" type="application/json">` + payload + `</script>` +
		`<article data-xpch="card-drama"><img src="data:image/gif;base64,R0lGOD" alt="" data-priority>` +
		`<a href="/drama/dj-lazy">详情</a></article>`
	items := parseHuangguaCards(body, host)
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	item := items[0]
	if item.Title != "懒加载样本" || item.Name != "懒加载样本" {
		t.Fatalf("title = %+v", item)
	}
	wantCover := host + "/_img/" + base64.RawStdEncoding.EncodeToString([]byte(coverURL)) + ".jpeg"
	if item.Cover != wantCover || item.CoverURL != wantCover {
		t.Fatalf("cover = %q, want %q", item.Cover, wantCover)
	}
	if item.EpisodeCount != "12" || item.Views != "69457热度" {
		t.Fatalf("metadata = %+v", item)
	}
}

func TestParseHuangguaPayloadFollowsIndexReferences(t *testing.T) {
	body := `<script id="__NUXT_DATA__" type="application/json">` +
		`["unused",{"slug":2,"title":3,"cover":4,"total_episode_count":5,"heat_7d":6},` +
		`"dj-ref","引用样本",{"url":7,"fallback_url":8,"encrypted":true},8,1024,` +
		`"https://pic.example.test/a.webp","/static/poster.jpg"]</script>`
	host := "https://abc.example.test"
	items := parseHuangguaPayload(body)
	entry, found := items["dj-ref"]
	if !found {
		t.Fatalf("payload item missing: %+v", items)
	}
	if entry.Title != "引用样本" || entry.Episodes != 8 || entry.Heat != "1024" {
		t.Fatalf("payload item = %+v", entry)
	}
	if want := host + "/_img/" + base64.RawStdEncoding.EncodeToString([]byte("https://pic.example.test/a.webp")) + ".webp"; huangguaCoverProxy(host, entry.Cover) != want {
		t.Fatalf("proxy cover = %q", huangguaCoverProxy(host, entry.Cover))
	}
	if got := huangguaCoverAddress(host, "", entry.Fallback); got != host+"/static/poster.jpg" {
		t.Fatalf("fallback cover = %q", got)
	}
}

func TestHuangguaMetaImageSkipsDefaultShareImage(t *testing.T) {
	body := `<meta property="og:image" content="https://hgdju.com/images/social-default.png?v=1">`
	if got := huangguaMetaImage(body, "https://abc.example.test"); got != "" {
		t.Fatalf("default share image = %q", got)
	}
}

func TestHuangguaCoversReadyRejectsLegacyCovers(t *testing.T) {
	ready := []nativeDrama{
		{ID: "huanggua:a", Cover: "https://porncz.eyrkjyudz.cc/_img/aHR0cHM.jpeg"},
		{ID: "huanggua:b", Cover: "https://porncz.eyrkjyudz.cc/_img/YnBkZg.webp"},
	}
	if !huangguaCoversReady(ready) {
		t.Fatal("代理封面应视为可用")
	}
	for _, cover := range []string{"", "https://hgdju.com/images/social-default.png?v=1", "https://pic.gyezbp.cn/new/xiao/20201117/a.png"} {
		if huangguaCoversReady([]nativeDrama{{ID: "huanggua:a", Cover: cover}}) {
			t.Fatalf("旧封面 %q 不应视为可用", cover)
		}
	}
}

func TestHuangguaChaptersDeduplicatesAndSorts(t *testing.T) {
	body := `<a href="/play/dj-1/3">3</a><a href="/play/dj-1/1">1</a><a href="/play/dj-1/2">2</a><a href="/play/dj-1/2">2</a>`
	chapters := huangguaChapters(body, "https://abc.example.test", "dj-1")
	if len(chapters) != 3 {
		t.Fatalf("chapters = %+v", chapters)
	}
	for index, want := range []string{"第1集", "第2集", "第3集"} {
		if chapters[index].Title != want {
			t.Fatalf("chapter %d title = %s", index, chapters[index].Title)
		}
	}
	if chapters[1].PageURL != "https://abc.example.test/play/dj-1/2" {
		t.Fatalf("page url = %s", chapters[1].PageURL)
	}
	if chapters[0].Source != sourceHuanggua || !strings.HasPrefix(chapters[0].ID, "huanggua:dj-1:") {
		t.Fatalf("chapter identity = %+v", chapters[0])
	}
	if len(huangguaChapters("<html></html>", "https://abc.example.test", "dj-1")) != 1 {
		t.Fatal("missing episode links should fall back to a single episode")
	}
}

func TestParseHuangguaPlayURL(t *testing.T) {
	escaped := `{"source_url":"https:\u002F\u002Fhls.example.test\u002Fv\u002Fa.m3u8?auth_key=1\u0026v=3","h265_url":""}`
	if got := parseHuangguaPlayURL(escaped); got != "https://hls.example.test/v/a.m3u8?auth_key=1&v=3" {
		t.Fatalf("escaped url = %q", got)
	}
	plain := `<script>var x = {"source_url":"https://hls.example.test/b.m3u8?x=1"}</script>`
	if got := parseHuangguaPlayURL(plain); got != "https://hls.example.test/b.m3u8?x=1" {
		t.Fatalf("plain url = %q", got)
	}
	if got := parseHuangguaPlayURL("nothing here"); got != "" {
		t.Fatalf("unexpected url %q", got)
	}
}

func TestHuangguaAndDsdDomainRecognition(t *testing.T) {
	for _, address := range []string{
		"https://abc.ywwegfkd.cc/drama/dj-1",
		"https://porncz.eyrkjyudz.cc/play/dj-1/1",
		"https://hgdja1.com/",
		"https://hgdju.com/",
	} {
		if source := providerSourceForURL(address); source != sourceHuanggua {
			t.Fatalf("providerSourceForURL(%q) = %q", address, source)
		}
	}
	for _, address := range []string{"https://dsd901.lol/index.php/vod/type/id/1.html", "https://www.dsd.com.se/"} {
		if source := providerSourceForURL(address); source != sourceDSD {
			t.Fatalf("providerSourceForURL(%q) = %q", address, source)
		}
	}
	if got := canonicalProviderSource("dsd901.lol"); got != sourceDSD {
		t.Fatalf("canonicalProviderSource(dsd901.lol) = %s", got)
	}
	if got := canonicalProviderSource("hgdja1.com"); got != sourceHuanggua {
		t.Fatalf("canonicalProviderSource(hgdja1.com) = %s", got)
	}
	if !validHuangguaCategory("") || !validHuangguaCategory("aiduanju") || validHuangguaCategory("../x") {
		t.Fatal("huanggua category validation is wrong")
	}
}

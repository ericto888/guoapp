package core

import (
	"strings"
	"testing"
)

func TestCustomMaccmsIDStableAndNamespaced(t *testing.T) {
	first := customMaccmsID("https://www.example.com/")
	second := customMaccmsID("https://www.example.com")
	if first != second {
		t.Fatalf("id should ignore trailing slash: %s vs %s", first, second)
	}
	if !strings.HasPrefix(first, customSourcePrefix) || len(first) != len(customSourcePrefix)+16 {
		t.Fatalf("unexpected id: %s", first)
	}
	if first == customMaccmsID("https://other.example.com") {
		t.Fatal("different sites must not share an id")
	}
}

func TestCustomSourceIDsRoundTripThroughProviderParsing(t *testing.T) {
	id := customMaccmsID("https://www.example.com")
	dramaID := providerDramaID(id, "12345")
	source, sourceID, ok := splitProviderDramaID(dramaID)
	if !ok || source != id || sourceID != "12345" {
		t.Fatalf("splitProviderDramaID(%q) = %q/%q/%v", dramaID, source, sourceID, ok)
	}
	if canonicalProviderSource(dramaID) != id {
		t.Fatalf("canonicalProviderSource(%q) = %q", dramaID, canonicalProviderSource(dramaID))
	}
	if !isHuangguoProviderSource(id) {
		t.Fatal("custom source should be a valid provider source")
	}
}

func TestNormalizeCustomSourceAddress(t *testing.T) {
	for _, sample := range []struct {
		raw  string
		want string
	}{
		{"example.com", "https://example.com"},
		{"https://example.com/", "https://example.com"},
		{" //example.com/api.php/provide/vod/ ", "https://example.com/api.php/provide/vod"},
	} {
		got, err := normalizeCustomSourceAddress(sample.raw)
		if err != nil || got != sample.want {
			t.Fatalf("normalize(%q) = %q, %v", sample.raw, got, err)
		}
	}
	for _, raw := range []string{"", "   ", "ftp://example.com", "https://"} {
		if _, err := normalizeCustomSourceAddress(raw); err == nil {
			t.Fatalf("normalize(%q) should fail", raw)
		}
	}
}

func TestCustomMaccmsCandidates(t *testing.T) {
	candidates := customMaccmsCandidates("https://example.com")
	if len(candidates) < 2 || candidates[0] != "https://example.com/api.php/provide/vod" {
		t.Fatalf("candidates = %v", candidates)
	}
	direct := customMaccmsCandidates("https://example.com/api.php/provide/vod/")
	if len(direct) != 1 || direct[0] != "https://example.com/api.php/provide/vod" {
		t.Fatalf("endpoint candidates = %v", direct)
	}
}

func TestCustomMaccmsRegistryPersists(t *testing.T) {
	directory := t.TempDir()
	registry := newCustomMaccmsRegistry(directory)
	record := customMaccmsSource{ID: customMaccmsID("https://example.com"), Name: "示例站", Base: "https://example.com"}
	if err := registry.put(record); err != nil {
		t.Fatal(err)
	}
	reloaded := newCustomMaccmsRegistry(directory)
	reloaded.load()
	got, found := reloaded.get(record.ID)
	if !found || got.Name != record.Name || got.Base != record.Base {
		t.Fatalf("reloaded record = %+v, found = %v", got, found)
	}
	if err := reloaded.remove(record.ID); err != nil {
		t.Fatal(err)
	}
	if _, found := reloaded.get(record.ID); found {
		t.Fatal("record should be removed")
	}
}

func TestCustomSourceDraftsParsesSupportedFormats(t *testing.T) {
	exported := `{"format":"duanju-custom-sources","version":1,"sites":[{"name":"示例","url":"https://example.com","api":"https://example.com/api.php/provide/vod"}]}`
	drafts, _ := customSourceDrafts(exported)
	if len(drafts) != 1 || drafts[0].Name != "示例" || drafts[0].Base != "https://example.com" {
		t.Fatalf("export drafts = %+v", drafts)
	}

	tvbox := `{"sites":[{"name":"甲站","api":"https://a.example.com/api.php/provide/vod","type":1},{"name":"乙站","api":"https://b.example.com"}]}`
	drafts, _ = customSourceDrafts(tvbox)
	if len(drafts) != 2 || drafts[0].Name != "甲站" || drafts[1].Base != "https://b.example.com" {
		t.Fatalf("tvbox drafts = %+v", drafts)
	}

	lines := "丙站,https://c.example.com\nhttps://d.example.com\n# 注释"
	drafts, _ = customSourceDrafts(lines)
	if len(drafts) != 2 || drafts[0].Name != "丙站" || drafts[1].Base != "https://d.example.com" {
		t.Fatalf("line drafts = %+v", drafts)
	}
}

func TestMaccmsCategoriesForCustomSourceDropsParents(t *testing.T) {
	rows := []map[string]any{
		{"type_id": "1", "type_pid": "0", "type_name": "电影"},
		{"type_id": "6", "type_pid": "1", "type_name": "动作片"},
		{"type_id": "2", "type_pid": "0", "type_name": "连续剧"},
		{"type_id": "13", "type_pid": "2", "type_name": "国产剧"},
		{"type_id": "30", "type_pid": "0", "type_name": "演员表"},
	}
	items := maccmsCategories(rows, true)
	names := map[string]bool{}
	for _, item := range items {
		names[item.Name] = true
	}
	if !names["动作片"] || !names["国产剧"] {
		t.Fatalf("leaf categories missing: %+v", items)
	}
	if names["电影"] || names["连续剧"] || names["演员表"] {
		t.Fatalf("parent or blocked categories kept: %+v", items)
	}
}

func TestMaccmsCategoriesDetectsParentsWithoutPid(t *testing.T) {
	// 实测 api.zuidapi.com 的 ac=list 不返回 type_pid，只能按名称识别父分类。
	rows := []map[string]any{
		{"type_id": 1, "type_name": "电影"},
		{"type_id": 6, "type_name": "动作片"},
		{"type_id": 2, "type_name": "电视剧"},
		{"type_id": 13, "type_name": "国产剧"},
		{"type_id": 3, "type_name": "综艺"},
		{"type_id": 25, "type_name": "大陆综艺"},
	}
	items := maccmsCategories(rows, true)
	for _, item := range items {
		if item.Name == "电影" || item.Name == "电视剧" || item.Name == "综艺" {
			t.Fatalf("parent category kept: %+v", items)
		}
	}
	if len(items) != 3 {
		t.Fatalf("categories = %+v", items)
	}
}

func TestMaccmsSiteOfResolvesCustomSource(t *testing.T) {
	engine, err := newNativeEngine(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	record := customMaccmsSource{
		ID: customMaccmsID("https://example.com"), Name: "示例站",
		Base: "https://example.com", API: "https://example.com/api.php/provide/vod",
	}
	if err := engine.customSources.put(record); err != nil {
		t.Fatal(err)
	}
	nativeState.Lock()
	previous := nativeState.engine
	nativeState.engine = engine
	nativeState.Unlock()
	defer func() {
		nativeState.Lock()
		nativeState.engine = previous
		nativeState.Unlock()
	}()
	site, found := maccmsSiteOf(record.ID)
	if !found || site.Name != record.Name || site.APIBase != record.API || site.SiteBase != "https://example.com" {
		t.Fatalf("maccmsSiteOf = %+v, found = %v", site, found)
	}
}

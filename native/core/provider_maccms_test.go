package core

import "testing"

func TestMaccmsCategoriesFromClass(t *testing.T) {
	class := []map[string]any{
		{"type_id": "1", "type_pid": "0", "type_name": "国产区"},
		{"type_id": "6", "type_pid": "1", "type_name": "网红主播"},
		{"type_id": "6", "type_pid": "1", "type_name": "网红主播"},
		{"type_id": "13", "type_pid": "2", "type_name": "制服诱惑"},
		{"type_id": "x", "type_pid": "2", "type_name": "无效分类"},
		{"type_id": "9", "type_pid": "2", "type_name": ""},
	}

	film := maccmsCategoriesFromClass(maccmsSites[sourceZuid], class)
	if len(film) != 0 {
		t.Fatalf("影视站不应保留非影视分类：%+v", film)
	}

	all := maccmsCategoriesFromClass(maccmsSites[sourceHsck], class)
	if len(all) != 2 {
		t.Fatalf("应只保留 2 个叶子分类，实际 %d 个：%+v", len(all), all)
	}
	if all[0].ID != "6" || all[0].Name != "网红主播" || all[1].ID != "13" {
		t.Fatalf("叶子分类顺序或去重不正确：%+v", all)
	}
}

func TestHsckMaccmsSite(t *testing.T) {
	site, ok := maccmsSiteOf("HSck")
	if !ok {
		t.Fatal("hsck 未登记采集接口")
	}
	if site.APIBase != "https://hsckzy001.com" || !site.AllCategories {
		t.Fatalf("hsck 站点配置不正确：%+v", site)
	}
	if bases := site.apiBases(); len(bases) != 3 || bases[0] != "https://hsckzy001.com" {
		t.Fatalf("hsck 备用线路顺序不正确：%v", bases)
	}
	if !isHuangguoProviderSource(sourceHsck) {
		t.Fatal("hsck 未纳入 native 站源白名单")
	}
	if source, sourceID, valid := splitProviderDramaID("hsck:89540"); !valid || source != sourceHsck || sourceID != "89540" {
		t.Fatalf("hsck 剧集 ID 解析不正确：%s %s %v", source, sourceID, valid)
	}
}

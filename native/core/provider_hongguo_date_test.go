package core

import "testing"

func TestHongguoDramaDoesNotTreatCreateTimeAsOnlineDate(t *testing.T) {
	drama := hongguoDramaFromAny(map[string]any{
		"video_data": map[string]any{
			"series_id":   "123456789",
			"series_name": "测试短剧",
			"create_time": "1794700800",
		},
	}, "短剧")

	if drama.ID == "" {
		t.Fatal("expected Hongguo drama to be parsed")
	}
	if drama.OnlineDate != "" {
		t.Fatalf("create_time must not be treated as online date, got %q", drama.OnlineDate)
	}
}

func TestHongguoDramaUsesFirstVisibleTimeAsOnlineDate(t *testing.T) {
	drama := hongguoDramaFromAny(map[string]any{
		"video_data": map[string]any{
			"series_id":         "123456790",
			"series_name":       "测试短剧",
			"first_visible_time": "1794700800",
			"create_time":       "1794000000",
		},
	}, "短剧")

	if drama.OnlineDate != "2026-11-15" {
		t.Fatalf("expected first_visible_time date 2026-11-15, got %q", drama.OnlineDate)
	}
}

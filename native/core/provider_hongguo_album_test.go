package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func decodeHongguoTestJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var out map[string]any
	if err := decoder.Decode(&out); err != nil {
		t.Fatalf("解析测试 JSON 失败: %v", err)
	}
	return out
}

func TestParseHongguoAlbumSeriesIDs(t *testing.T) {
	result := decodeHongguoTestJSON(t, `{
		"data": {
			"7667544091206355992": {
				"album_info": {
					"album_title": "京师第一纨绔系列剧",
					"album_serial_count": 15,
					"album_series_ids": [7667544091206355992, "7704629770473966617", 7667544091206355992, 0]
				}
			}
		}
	}`)
	ids := parseHongguoAlbumSeriesIDs(result, "7667544091206355992")
	want := []string{"7667544091206355992", "7704629770473966617"}
	if len(ids) != len(want) {
		t.Fatalf("季 ID 数量不符: %v", ids)
	}
	for index := range want {
		if ids[index] != want[index] {
			t.Fatalf("第 %d 个季 ID 不符: %s != %s", index+1, ids[index], want[index])
		}
	}
}

func TestParseHongguoAlbumSeasonRowsUsesPublishTime(t *testing.T) {
	publish := int64(1794697681)
	result := decodeHongguoTestJSON(t, `{
		"data": {
			"7667544091206355992": {
				"base_info": {
					"series_title": "京师第一纨绔第一季",
					"episode_cnt": 114,
					"publish_time": 1785454080,
					"series_cover": "https://example.com/1.jpg"
				}
			},
			"7704629770473966617": {
				"base_info": {
					"series_title": "京师第一纨绔第十四季",
					"episode_cnt": 276,
					"publish_time": 1794697681
				}
			},
			"7708298945843186713": {}
		}
	}`)
	seasons := parseHongguoAlbumSeasonRows(result,
		[]string{"7667544091206355992", "7704629770473966617", "7708298945843186713"})
	if len(seasons) != 2 {
		t.Fatalf("应跳过缺失 base_info 的季，实际 %d 项", len(seasons))
	}
	if seasons[0].SeasonIndex != 1 || seasons[0].Episodes != 114 {
		t.Fatalf("第一季解析异常: %+v", seasons[0])
	}
	if want := time.Unix(1785454080, 0).In(providerChinaTime).Format("2006-01-02"); seasons[0].OnlineDate != want {
		t.Fatalf("第一季上线日期应为 %s，实际 %s", want, seasons[0].OnlineDate)
	}
	if seasons[1].SeasonIndex != 14 {
		t.Fatalf("第十四季季号应为 14，实际 %d", seasons[1].SeasonIndex)
	}
	if want := time.Unix(publish, 0).In(providerChinaTime).Format("2006-01-02"); seasons[1].OnlineDate != want {
		t.Fatalf("第十四季上线日期应为 %s，实际 %s", want, seasons[1].OnlineDate)
	}
	// 1794697681 = 2026-11-14 23:08 UTC，中国时区是 11-15；不能按 UTC 算成 11-14。
	if seasons[1].OnlineDate != "2026-11-15" {
		t.Fatalf("第十四季上线日期应为 2026-11-15，实际 %s", seasons[1].OnlineDate)
	}
}

func TestHongguoAlbumPublishDateIgnoresUpcomingSeason(t *testing.T) {
	base := map[string]any{"publish_time": "1794728328"}
	if date := hongguoAlbumPublishDate(base, 0); date != "" {
		t.Fatalf("待上线季不应使用 publish_time，实际 %s", date)
	}
	if date := hongguoAlbumPublishDate(map[string]any{"publish_time": "0"}, 10); date != "" {
		t.Fatalf("无效时间戳应返回空，实际 %s", date)
	}
}

func TestMergeHongguoUpcomingSeasonsAppendsMissingSeason(t *testing.T) {
	album := []hongguoPlanSeason{
		{SeriesID: "1", SeasonIndex: 1, Episodes: 114, OnlineDate: "2026-07-31"},
		{SeriesID: "14", SeasonIndex: 14, Episodes: 276, OnlineDate: "2026-11-15"},
	}
	plan := []hongguoPlanSeason{
		{SeriesID: "14", SeasonIndex: 14, Episodes: 63, OnlineDate: ""},
		{SeriesID: "15", SeasonIndex: 15, Episodes: 0, OnlineDate: "2026-11-22"},
	}
	merged := mergeHongguoUpcomingSeasons(album, plan)
	if len(merged) != 3 {
		t.Fatalf("合并后应有 3 季，实际 %d", len(merged))
	}
	if merged[0].SeasonIndex != 1 || merged[1].SeasonIndex != 14 || merged[2].SeasonIndex != 15 {
		t.Fatalf("季顺序异常: %+v", merged)
	}
	if merged[1].Episodes != 276 || merged[1].OnlineDate != "2026-11-15" {
		t.Fatalf("已上线季应保留批量接口数据，实际 %+v", merged[1])
	}
	if merged[2].OnlineDate != "2026-11-22" || merged[2].Episodes != 0 {
		t.Fatalf("待上线季日期应来自预计文案，实际 %+v", merged[2])
	}
}

func TestHongguoSeasonCacheHitsEverySeasonID(t *testing.T) {
	downloader := &Downloader{}
	seasons := []hongguoPlanSeason{
		{SeriesID: "11", SeasonIndex: 1, OnlineDate: "2026-07-31"},
		{SeriesID: "12", SeasonIndex: 2, OnlineDate: "2026-08-01"},
	}
	downloader.storeHongguoSeasons(seasons)
	for _, id := range []string{"11", "12"} {
		cached, ok := downloader.cachedHongguoSeasons(id)
		if !ok || len(cached) != 2 || cached[0].SeriesID != "11" {
			t.Fatalf("从第 %s 季未命中缓存: ok=%v %+v", id, ok, cached)
		}
	}
	// 调用方 append 不应污染缓存。
	cached, _ := downloader.cachedHongguoSeasons("11")
	_ = append(cached[:0], hongguoPlanSeason{SeriesID: "99"})
	again, _ := downloader.cachedHongguoSeasons("11")
	if again[0].SeriesID != "11" {
		t.Fatalf("缓存被调用方修改: %+v", again)
	}
}

func TestHongguoSeasonCacheExpires(t *testing.T) {
	downloader := &Downloader{}
	client := downloader.hongguoClient()
	client.mu.Lock()
	client.seasons["21"] = hongguoSeasonCacheEntry{
		seasons: []hongguoPlanSeason{{SeriesID: "21", SeasonIndex: 1}},
		fetched: time.Now().Add(-hongguoSeasonCacheTTL - time.Minute),
	}
	client.mu.Unlock()
	if _, ok := downloader.cachedHongguoSeasons("21"); ok {
		t.Fatal("超过 TTL 的缓存不应命中")
	}
}

package core

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 红果系列批量接口：/reading/distribution/book_pack_fields/inner_feed_detail/v1/
//
// 实测（2026-11）：
//   - 请求体必须是整数 ID 数组：{"book_pack_channel":"inner_feed_detail","req_ids":[int64,...],"req_fields":[...]}，
//     传字符串 ID 会直接 HTTP 400（expected int）。
//   - album_series_ids 只包含已经上线、且被红果归入该系列的季；待上线季（episode_cnt=0）
//     可能还不在列表里，需要 plan/v 发现。
//   - base_info.publish_time 是各季真实上线时间（秒级时间戳），换算后与网页详情
//     first_visible_time 完全一致；但待上线季的 publish_time 是当前时刻，不可当作上线日期。
const hongguoAlbumFieldsPath = "/reading/distribution/book_pack_fields/inner_feed_detail/v1/"

var hongguoAlbumIDFields = []string{
	"album_id", "album_title", "album_serial_count", "series_in_album_order", "album_series_ids",
}

var hongguoAlbumSeasonFields = []string{
	"series_id", "series_title", "series_cover", "series_intro", "episode_cnt",
	"series_status", "category_schema", "series_play_cnt", "publish_time",
}

// hongguoSeasonCacheTTL 与上游 hongguo.js 一致：整组季缓存 30 分钟。
const hongguoSeasonCacheTTL = 30 * time.Minute

// hongguoSeasonCacheEntry 缓存某个系列（按季 ID 索引）的完整季列表。
type hongguoSeasonCacheEntry struct {
	seasons []hongguoPlanSeason
	fetched time.Time
}

// hongguoSeriesSeasons 汇总红果官方系列的全部季。
//
// 已上线季来自系列批量接口（带真实上线日期），待上线季来自 plan/v
// （episode_cnt=0，日期用「预计X月X日上线」文案）。两者都失败才 ok=false。
// 两路请求走不同 lane 并行：限流器按"站源#lane"加锁，同 lane 串行，
// 不同 lane 可以同时在途，因此并行能把关键路径从 3 次往返降到 2 次。
//
// 成功且拿到批量数据后按每个季 ID 缓存 30 分钟，从同系列任意一季进入
// 都能直接命中，重复打开不再发请求。
func (downloader *Downloader) hongguoSeriesSeasons(ctx context.Context, seriesID string) ([]hongguoPlanSeason, bool) {
	if cached, ok := downloader.cachedHongguoSeasons(seriesID); ok {
		return cached, true
	}
	var (
		album   []hongguoPlanSeason
		albumOK bool
		plan    []hongguoPlanSeason
		planOK  bool
	)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		album, albumOK = downloader.hongguoAlbumSeasons(ctx, seriesID)
	}()
	go func() {
		defer wait.Done()
		planCtx := context.WithValue(ctx, requestLaneKey{}, "hongguo-plan")
		plan, planOK = downloader.hongguoPlanSeasons(planCtx, seriesID)
	}()
	wait.Wait()
	if !albumOK && !planOK {
		return nil, false
	}
	if !albumOK {
		// 只有 plan/v 的结果时不上线日期不可靠，不写缓存。
		return plan, true
	}
	if !planOK || len(plan) == 0 {
		downloader.storeHongguoSeasons(album)
		return album, true
	}
	merged := mergeHongguoUpcomingSeasons(album, plan)
	downloader.storeHongguoSeasons(merged)
	return merged, true
}

// cachedHongguoSeasons 读取缓存（返回副本，避免调用方 append 改到缓存底层数组）。
func (downloader *Downloader) cachedHongguoSeasons(seriesID string) ([]hongguoPlanSeason, bool) {
	client := downloader.hongguoClient()
	client.mu.Lock()
	defer client.mu.Unlock()
	entry, ok := client.seasons[seriesID]
	if !ok || time.Since(entry.fetched) >= hongguoSeasonCacheTTL {
		return nil, false
	}
	return append([]hongguoPlanSeason{}, entry.seasons...), true
}

// storeHongguoSeasons 按列表中每个季 ID 各写一份，任一季入口都能命中。
func (downloader *Downloader) storeHongguoSeasons(seasons []hongguoPlanSeason) {
	if len(seasons) == 0 {
		return
	}
	shared := append([]hongguoPlanSeason{}, seasons...)
	entry := hongguoSeasonCacheEntry{seasons: shared, fetched: time.Now()}
	client := downloader.hongguoClient()
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.seasons == nil {
		client.seasons = map[string]hongguoSeasonCacheEntry{}
	}
	for _, season := range shared {
		if season.SeriesID != "" {
			client.seasons[season.SeriesID] = entry
		}
	}
}

// hongguoAlbumSeasons 通过系列批量接口取「已上线季」列表。
func (downloader *Downloader) hongguoAlbumSeasons(ctx context.Context, seriesID string) ([]hongguoPlanSeason, bool) {
	if !hongguoNumericID.MatchString(seriesID) {
		return nil, false
	}
	ids, ok := downloader.hongguoAlbumSeriesIDs(ctx, seriesID)
	if !ok || len(ids) == 0 {
		return nil, false
	}
	result, err := downloader.hongguoAppRequest(ctx, http.MethodPost, hongguoAlbumFieldsPath, nil, map[string]any{
		"book_pack_channel": "inner_feed_detail",
		"req_ids":           hongguoIDList(ids),
		"req_fields":        hongguoAlbumSeasonFields,
	})
	if err != nil {
		return nil, false
	}
	seasons := parseHongguoAlbumSeasonRows(result, ids)
	if len(seasons) == 0 {
		return nil, false
	}
	return seasons, true
}

// hongguoAlbumSeriesIDs 取系列内的季 ID（按官方顺序）。
func (downloader *Downloader) hongguoAlbumSeriesIDs(ctx context.Context, seriesID string) ([]string, bool) {
	result, err := downloader.hongguoAppRequest(ctx, http.MethodPost, hongguoAlbumFieldsPath, nil, map[string]any{
		"book_pack_channel": "inner_feed_detail",
		"req_ids":           hongguoIDList([]string{seriesID}),
		"req_fields":        hongguoAlbumIDFields,
	})
	if err != nil {
		return nil, false
	}
	ids := parseHongguoAlbumSeriesIDs(result, seriesID)
	return ids, len(ids) > 0
}

// parseHongguoAlbumSeriesIDs 解析 data.<seriesID>.album_info.album_series_ids，保持官方顺序。
func parseHongguoAlbumSeriesIDs(result map[string]any, seriesID string) []string {
	info := nestedMap(result, "data", seriesID, "album_info")
	var ids []string
	seen := map[string]bool{}
	for _, item := range anyList(info["album_series_ids"]) {
		id := hongguoJSONID(item)
		if id == "" || seen[id] || !hongguoNumericID.MatchString(id) {
			continue
		}
		if value, err := strconv.ParseInt(id, 10, 64); err != nil || value <= 0 {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// parseHongguoAlbumSeasonRows 按 ids 顺序解析 data.<id>.base_info。
// 缺 base_info 的季（通常未上线）直接跳过。
func parseHongguoAlbumSeasonRows(result map[string]any, ids []string) []hongguoPlanSeason {
	var seasons []hongguoPlanSeason
	for index, id := range ids {
		base := nestedMap(result, "data", id, "base_info")
		if len(base) == 0 {
			continue
		}
		episodes, _ := strconv.Atoi(mapString(base, "episode_cnt"))
		if episodes < 0 {
			episodes = 0
		}
		title := strings.TrimSpace(mapString(base, "series_title"))
		seasonIndex := index + 1
		if _, parsed, _, _ := hongguoSearchSeason(title); parsed > 0 {
			seasonIndex = parsed
		}
		if seasonIndex < 1 || seasonIndex > hongguoSearchSeasonLimit {
			continue
		}
		seasons = append(seasons, hongguoPlanSeason{
			SeriesID:    id,
			Title:       title,
			SeasonIndex: seasonIndex,
			Episodes:    episodes,
			Cover:       mapString(base, "series_cover"),
			Description: mapString(base, "series_intro"),
			OnlineDate:  hongguoAlbumPublishDate(base, episodes),
		})
	}
	return seasons
}

// hongguoAlbumPublishDate 取 base_info.publish_time 的真实上线日期。
// 待上线季（episodes=0）的时间戳是查询时刻，不能当作上线日期，直接留空。
// 固定按中国时区换算：iOS 上 Go 的 time.Local 可能是 UTC，直接用 Local
// 会把 11-15 07:08 算成 11-14。
func hongguoAlbumPublishDate(base map[string]any, episodes int) string {
	if episodes <= 0 {
		return ""
	}
	seconds, err := strconv.ParseInt(mapString(base, "publish_time"), 10, 64)
	if err != nil || seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).In(providerChinaTime).Format("2006-01-02")
}

// mergeHongguoUpcomingSeasons 把 plan/v 里尚未进入系列包的季（待上线季）追加到批量结果。
// 已在批量结果中的季完全以批量数据为准，避免两侧日期/集数互相覆盖。
func mergeHongguoUpcomingSeasons(album, plan []hongguoPlanSeason) []hongguoPlanSeason {
	merged := append([]hongguoPlanSeason{}, album...)
	known := map[string]bool{}
	for _, season := range merged {
		known[season.SeriesID] = true
	}
	for _, season := range plan {
		if season.SeriesID == "" || known[season.SeriesID] {
			continue
		}
		known[season.SeriesID] = true
		merged = append(merged, season)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].SeasonIndex < merged[j].SeasonIndex
	})
	return merged
}

// hongguoIDList 把字符串 ID 转成接口要求的整数数组。
func hongguoIDList(ids []string) []int64 {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		value, err := strconv.ParseInt(id, 10, 64)
		if err != nil || value <= 0 {
			continue
		}
		out = append(out, value)
	}
	return out
}

// hongguoJSONID 把 any 形态的 ID 转成字符串，避免大整数精度丢失。
func hongguoJSONID(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return ""
	}
}

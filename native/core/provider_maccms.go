package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// 几个标准苹果CMS 采集站，接口一致：
// ac=list 取分类，ac=detail 取列表/详情，vod_play_url 里是 m3u8 或播放页。
type maccmsSite struct {
	Source   string
	Name     string
	APIBase  string
	SiteBase string
}

var maccmsSites = map[string]maccmsSite{
	sourceZuid: {Source: sourceZuid, Name: "最大影视", APIBase: "https://api.zuidapi.com", SiteBase: "https://www.zuidazy.net"},
	sourceFfzy: {Source: sourceFfzy, Name: "非凡影视", APIBase: "https://api.ffzyapi.com", SiteBase: "https://www.ffzyapi.com"},
	sourceRycj: {Source: sourceRycj, Name: "如意影视", APIBase: "https://cj.rycjapi.com", SiteBase: "https://www.rycjapi.com"},
}

// maccmsCategoryNames 只保留影视相关的叶子分类，父分类（电影片/连续剧/综艺/动漫）接口返回空。
var maccmsCategoryNames = map[string]bool{
	"动作片": true, "喜剧片": true, "爱情片": true, "科幻片": true, "恐怖片": true,
	"剧情片": true, "战争片": true, "4K电影": true,
	"国产剧": true, "欧美剧": true, "香港剧": true, "韩国剧": true, "日本剧": true,
	"台湾剧": true, "海外剧": true, "泰国剧": true, "韩剧": true, "日剧": true,
	"港剧": true, "台剧": true, "泰剧": true,
	"大陆综艺": true, "日韩综艺": true, "港台综艺": true, "欧美综艺": true,
	"国产动漫": true, "日韩动漫": true, "欧美动漫": true, "动画片": true,
}

var (
	maccmsIDPattern      = regexp.MustCompile(`^\d+$`)
	maccmsTagPattern     = regexp.MustCompile(`(?s)<[^>]*>`)
	maccmsEpisodePattern = regexp.MustCompile(`(?:全|更新至?第?)\s*(\d+)\s*集`)
)

type maccmsPage struct {
	PageCount any              `json:"pagecount"`
	List      []map[string]any `json:"list"`
	Class     []map[string]any `json:"class"`
}

func maccmsSiteOf(source string) (maccmsSite, bool) {
	source = canonicalProviderSource(source)
	if site, found := maccmsSites[source]; found {
		return site, true
	}
	// 自定义源与内置采集站共用同一套解析，记录在运行时注册表里。
	if record, found := customSourceRecordOf(source); found {
		return customSourceSite(record), true
	}
	return maccmsSite{}, false
}

func maccmsNumber(value any) int {
	switch typed := value.(type) {
	case json.Number:
		number, _ := typed.Int64()
		return int(number)
	case float64:
		return int(typed)
	case string:
		number, _ := strconv.Atoi(strings.TrimSpace(typed))
		return number
	}
	return 0
}

func validMaccmsCategory(category string) bool {
	if category == "" {
		return true
	}
	return maccmsIDPattern.MatchString(category)
}

// maccmsRequest 走采集接口，返回当页数据。
func (d *Downloader) maccmsRequest(ctx context.Context, source string, params url.Values) (maccmsPage, error) {
	site, ok := maccmsSiteOf(source)
	if !ok {
		return maccmsPage{}, errors.New("站源未接入采集接口")
	}
	return d.maccmsRequestAt(ctx, site, params)
}

// maccmsRequestAt 对给定站点发起采集请求。自定义源只填了站点根地址时，
// 依次尝试标准接口候选，命中的端点会记进注册表，后续请求直接复用。
func (d *Downloader) maccmsRequestAt(ctx context.Context, site maccmsSite, params url.Values) (maccmsPage, error) {
	candidates := []string{site.APIBase}
	if isCustomMaccmsSource(site.Source) {
		candidates = customMaccmsCandidates(site.APIBase)
	}
	var lastErr error
	for _, endpoint := range candidates {
		page, err := d.maccmsRequestEndpoint(ctx, site, endpoint, params)
		if err != nil {
			lastErr = err
			continue
		}
		if isCustomMaccmsSource(site.Source) {
			if engine := nativeEngineSnapshot(); engine != nil && engine.customSources != nil {
				engine.customSources.rememberAPI(site.Source, endpoint)
			}
		}
		return page, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%s接口返回格式异常", site.Name)
	}
	return maccmsPage{}, lastErr
}

func (d *Downloader) maccmsRequestEndpoint(ctx context.Context, site maccmsSite, endpoint string, params url.Values) (maccmsPage, error) {
	address := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if !isMaccmsEndpointURL(address) {
		address += "/api.php/provide/vod"
	}
	address += "/?" + params.Encode()
	body, err := d.fetchProviderText(ctx, address, strings.TrimRight(site.SiteBase, "/")+"/")
	if err != nil {
		return maccmsPage{}, err
	}
	var page maccmsPage
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&page); err != nil {
		return maccmsPage{}, fmt.Errorf("%s接口返回格式异常", site.Name)
	}
	return page, nil
}

// fetchMaccmsCategories 读取站点分类，仅保留影视类目并保持站点顺序。
func (d *Downloader) fetchMaccmsCategories(ctx context.Context, source string) ([]nativeCategory, error) {
	site, ok := maccmsSiteOf(source)
	if !ok {
		return nil, errors.New("站源未接入采集接口")
	}
	page, err := d.maccmsRequest(ctx, source, url.Values{"ac": {"list"}})
	if err != nil {
		return nil, err
	}
	items := maccmsCategories(page.Class, isCustomMaccmsSource(source))
	if len(items) == 0 {
		return nil, fmt.Errorf("%s暂未返回影视分类", site.Name)
	}
	return items, nil
}

// maccmsCategories 从 ac=list 的 class 里挑出可用于浏览的分类。
// 内置采集站用白名单；自定义源各站分类命名不一，用「非父分类 + 影视关键词」筛选。
func maccmsCategories(rows []map[string]any, custom bool) []nativeCategory {
	items := make([]nativeCategory, 0, len(rows))
	seen := map[string]bool{}
	if !custom {
		for _, class := range rows {
			id := mapString(class, "type_id")
			name := strings.TrimSpace(mapString(class, "type_name"))
			if !maccmsIDPattern.MatchString(id) || seen[id] || !maccmsCategoryNames[name] {
				continue
			}
			seen[id] = true
			items = append(items, nativeCategory{ID: id, Name: name})
		}
		return items
	}
	parents := map[string]bool{}
	entries := make([]nativeCategory, 0, len(rows))
	for _, class := range rows {
		id := mapString(class, "type_id")
		name := strings.TrimSpace(mapString(class, "type_name"))
		if !maccmsIDPattern.MatchString(id) || name == "" || seen[id] {
			continue
		}
		if parent := mapString(class, "type_pid"); parent != "" && parent != "0" {
			parents[parent] = true
		}
		seen[id] = true
		entries = append(entries, nativeCategory{ID: id, Name: name})
	}
	for _, entry := range entries {
		if parents[entry.ID] || maccmsParentNames[entry.Name] {
			continue
		}
		if maccmsCustomCategoryBlocked(entry.Name) {
			continue
		}
		if maccmsCustomCategoryHinted(entry.Name) {
			items = append(items, entry)
		}
	}
	if len(items) > 0 {
		return items
	}
	// 分类命名完全不沾影视关键词时，退回全部叶子分类。
	for _, entry := range entries {
		if !parents[entry.ID] && !maccmsParentNames[entry.Name] && !maccmsCustomCategoryBlocked(entry.Name) {
			items = append(items, entry)
		}
	}
	return items
}

func maccmsCustomCategoryHinted(name string) bool {
	for _, hint := range maccmsCategoryHints {
		if strings.Contains(name, hint) {
			return true
		}
	}
	return false
}

func maccmsCustomCategoryBlocked(name string) bool {
	for _, blocked := range maccmsCategoryBlocklist {
		if strings.Contains(name, blocked) {
			return true
		}
	}
	return false
}

var maccmsCategoryHints = []string{
	"电影", "片", "剧", "综艺", "动漫", "动画", "纪录", "短剧", "体育", "少儿", "喜剧", "脱口秀", "番剧",
}

var maccmsCategoryBlocklist = []string{
	"演员", "明星", "专题", "福利", "资讯", "新闻", "排行", "榜单", "解说", "花絮", "预告",
}

// maccmsParentNames 没有 type_pid 的站点只能靠名称识别父分类：
// 父分类点进去通常是空列表，留在标签里会让人以为源坏了。
var maccmsParentNames = map[string]bool{
	"电影": true, "电视剧": true, "连续剧": true, "综艺": true, "动漫": true, "动画": true,
	"片库": true, "电影片": true, "连续剧片": true, "综艺片": true, "动漫片": true, "高清": true,
}

func (d *Downloader) fetchMaccmsCatalogPage(ctx context.Context, source string, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 {
		page = 1
	}
	if !validMaccmsCategory(category) {
		return nil, false, errors.New("影视分类无效")
	}
	// 不传 t 时按更新时间倒序返回全站影视，正好当作「全部」。
	params := url.Values{"ac": {"detail"}, "pg": {strconv.Itoa(page)}}
	if query = strings.TrimSpace(query); query != "" {
		params.Set("wd", query)
	} else if category != "" {
		params.Set("t", category)
	}
	result, err := d.maccmsRequest(ctx, source, params)
	if err != nil {
		return nil, false, err
	}
	items := make([]Drama, 0, len(result.List))
	for _, row := range result.List {
		if drama := maccmsDramaFromRow(source, row); drama.ID != "" {
			items = append(items, drama)
		}
	}
	return items, page < maccmsNumber(result.PageCount), nil
}

func (d *Downloader) fetchMaccmsDetail(ctx context.Context, source, sourceID string) (Drama, []Chapter, error) {
	site, ok := maccmsSiteOf(source)
	if !ok {
		return Drama{}, nil, errors.New("站源未接入采集接口")
	}
	sourceID = strings.TrimSpace(sourceID)
	if !maccmsIDPattern.MatchString(sourceID) {
		return Drama{}, nil, fmt.Errorf("%s剧集 ID 无效", site.Name)
	}
	result, err := d.maccmsRequest(ctx, source, url.Values{"ac": {"detail"}, "ids": {sourceID}})
	if err != nil {
		return Drama{}, nil, err
	}
	var row map[string]any
	for _, item := range result.List {
		if mapString(item, "vod_id") == sourceID {
			row = item
			break
		}
	}
	if row == nil {
		return Drama{}, nil, fmt.Errorf("该剧在%s已下线，请刷新剧库", site.Name)
	}
	drama := maccmsDramaFromRow(source, row)
	chapters := maccmsChapters(source, row, sourceID)
	if len(chapters) == 0 {
		return drama, nil, fmt.Errorf("%s没有返回可播放的分集", site.Name)
	}
	drama.TotalEpisode, drama.EpisodeCount = strconv.Itoa(len(chapters)), strconv.Itoa(len(chapters))
	return drama, chapters, nil
}

func maccmsDramaFromRow(source string, row map[string]any) Drama {
	sourceID := mapString(row, "vod_id")
	if !maccmsIDPattern.MatchString(sourceID) {
		return Drama{}
	}
	title := mapString(row, "vod_name")
	if title == "" {
		return Drama{}
	}
	content := maccmsPlainText(mapString(row, "vod_content"))
	remark := strings.TrimSpace(mapString(row, "vod_remarks"))
	var tags []string
	for _, tag := range strings.FieldsFunc(maccmsPlainText(mapString(row, "vod_class")), func(r rune) bool {
		return r == ',' || r == '，' || r == '/' || r == ' '
	}) {
		if tag = strings.TrimSpace(tag); tag != "" && len([]rune(tag)) <= 12 {
			tags = append(tags, tag)
		}
	}
	episodes := ""
	if match := maccmsEpisodePattern.FindStringSubmatch(remark); len(match) == 2 {
		episodes = match[1]
	}
	cover := mapString(row, "vod_pic")
	return Drama{
		ID: providerDramaID(source, sourceID), Source: source, SourceID: sourceID,
		Title: title, Name: title, Desc: content, Intro: content,
		Cover: cover, CoverURL: cover, CategoryName: mapString(row, "type_name"),
		ChannelName: "", Remark: remark, TotalEpisode: episodes, EpisodeCount: episodes,
		Tags: tags, Score: mapString(row, "vod_score"), Views: mapString(row, "vod_hits"),
		OnlineDate: mapString(row, "vod_pubdate"),
	}
}

// maccmsChapters 把 vod_play_url 拆成分集。采集站可能给多条线路，
// 优先选能直接播的 m3u8 线路，再比集数，避免选中播放页线路。
func maccmsChapters(source string, row map[string]any, sourceID string) []Chapter {
	playURL := mapString(row, "vod_play_url")
	var best []Chapter
	bestHLS := -1
	for _, group := range strings.Split(playURL, "$$$") {
		var chapters []Chapter
		hls := 0
		for _, line := range strings.Split(group, "#") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			name, address := line, ""
			if index := strings.Index(line, "$"); index >= 0 {
				name, address = line[:index], line[index+1:]
			}
			address = strings.TrimSpace(address)
			if !isProviderHTTPMediaURL(address) {
				continue
			}
			if strings.Contains(strings.ToLower(address), ".m3u8") {
				hls++
			}
			index := len(chapters) + 1
			chapters = append(chapters, Chapter{
				ID: providerChapterID(source, sourceID, strconv.Itoa(index)), Source: source,
				Title:    firstNonEmpty(strings.TrimSpace(name), fmt.Sprintf("第%d集", index)),
				VideoURL: address, CurrentEpisode: rawEpisode(index),
			})
		}
		if len(chapters) == 0 {
			continue
		}
		// m3u8 集数优先；没有 m3u8 时退回集数最多的线路。
		if hls > bestHLS || hls == bestHLS && len(chapters) > len(best) {
			best, bestHLS = chapters, hls
		}
	}
	return best
}

// maccmsPlainText 去掉简介里的 HTML 标签和多余空白。
func maccmsPlainText(value string) string {
	value = maccmsTagPattern.ReplaceAllString(value, " ")
	value = strings.ReplaceAll(value, "&nbsp;", " ")
	value = html.UnescapeString(value)
	return strings.Join(strings.Fields(value), " ")
}

package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 黄瓜短剧：站点是 Nuxt SSR，没有可用的公开 JSON 接口；
// 列表/详情/取流直接解析服务端渲染出来的 HTML。
const (
	huangguaNavURL    = "https://hgdja1.com/"
	huangguaHostTTL   = 30 * time.Minute
	huangguaUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"
)

// 入口页无法解析时的兜底主机；这些域名会随入口页轮换。
var huangguaSeedHosts = []string{
	"https://abc.ywwegfkd.cc",
	"https://qwer.ywwegfkd.cc",
	"https://xyz.ywwegfkd.cc",
	"https://mkqjue.eyrkjyudz.cc",
	"https://porncz.eyrkjyudz.cc",
	"https://rixgqa.eyrkjyudz.cc",
}

var huangguaCategories = []nativeCategory{
	{ID: "", Name: "推荐"},
	{ID: "aiduanju", Name: "AI 短剧"},
	{ID: "manju", Name: "AI 漫剧"},
	{ID: "mogai", Name: "AI 魔改"},
	{ID: "zhenren", Name: "真人短剧"},
	{ID: "yuanchuang", Name: "原创短剧"},
	{ID: "browse", Name: "全部"},
}

var (
	huangguaCardSplit   = regexp.MustCompile(`<article data-xpch="card-drama"`)
	huangguaAlias       = regexp.MustCompile(`href="/drama/([a-zA-Z0-9\-]+)"`)
	huangguaImgTag      = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	huangguaImgAttr     = regexp.MustCompile(`(?is)([a-zA-Z:_-]+)\s*=\s*"([^"]*)"`)
	huangguaCoverPath   = regexp.MustCompile(`(/_img/[A-Za-z0-9+/=]+\.(?:jpeg|jpg|png|webp))`)
	huangguaOGImage     = regexp.MustCompile(`(?is)<meta\b[^>]*(?:property|name)=["']og:image["'][^>]*content=["']([^"']+)["']`)
	huangguaOGImageAlt  = regexp.MustCompile(`(?is)<meta\b[^>]*content=["']([^"']+)["'][^>]*(?:property|name)=["']og:image["']`)
	huangguaAlt         = regexp.MustCompile(`<img[^>]+alt="([^"]+)"`)
	huangguaEpisodeText = regexp.MustCompile(`(?:更新至|全)\s*(\d+)\s*集`)
	huangguaHeatText    = regexp.MustCompile(`([0-9.]+万?)\s*热度`)
	huangguaH1          = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	huangguaHTMLTag     = regexp.MustCompile(`(?s)<[^>]*>`)
	huangguaMetaDesc    = regexp.MustCompile(`(?is)<meta\s+name="description"\s+content="([^"]*)"`)
	huangguaHostLink    = regexp.MustCompile(`https?://[a-zA-Z0-9][a-zA-Z0-9\-]*\.(?:ywwegfkd\.cc|eyrkjyudz\.cc)`)
	huangguaPlayURL     = regexp.MustCompile(`"source_url"\s*:\s*"([^"]+)"`)
	huangguaEscapedM3U8 = regexp.MustCompile(`https?:\\u002F\\u002F[^"]+?\.m3u8[^"]*`)
	huangguaPayloadTag  = regexp.MustCompile(`(?is)<script[^>]*id="__NUXT_DATA__"[^>]*>(.*?)</script>`)
)

type huangguaPayloadItem struct {
	Cover    string
	Fallback string
	Title    string
	Episodes int
	Heat     string
}

type huangguaHostState struct {
	mu        sync.Mutex
	host      string
	updatedAt time.Time
}

func validHuangguaCategory(category string) bool {
	if category == "" {
		return true
	}
	for _, entry := range huangguaCategories {
		if entry.ID == category {
			return true
		}
	}
	return false
}

func huangguaContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, providerTextUserAgentKey{}, huangguaUserAgent)
}

// huangguaHostForRequest 返回当前可用的站点主机：
// 优先用 30 分钟内缓存，其次读入口页 hgdja1.com 列出的新域名，最后试静态兜底。
func (d *Downloader) huangguaHostForRequest(ctx context.Context) (string, error) {
	state := &d.huangguaHost
	state.mu.Lock()
	cached, fresh := state.host, time.Since(state.updatedAt) < huangguaHostTTL
	state.mu.Unlock()
	if cached != "" && fresh {
		return cached, nil
	}
	candidates := d.huangguaHostCandidates(ctx)
	if cached != "" {
		candidates = append([]string{cached}, candidates...)
	}
	for _, host := range candidates {
		if d.huangguaHostWorks(ctx, host) {
			state.mu.Lock()
			state.host, state.updatedAt = host, time.Now()
			state.mu.Unlock()
			return host, nil
		}
	}
	return "", errors.New("黄瓜短剧当前没有可用线路")
}

func (d *Downloader) huangguaHostCandidates(ctx context.Context) []string {
	var out []string
	seen := map[string]bool{}
	add := func(host string) {
		host = strings.TrimRight(strings.TrimSpace(host), "/")
		if host == "" || seen[host] || strings.Contains(host, "hgdja1.com") {
			return
		}
		seen[host] = true
		out = append(out, host)
	}
	if body, err := d.fetchProviderText(huangguaContext(ctx), huangguaNavURL, huangguaNavURL); err == nil {
		for _, host := range huangguaHostLink.FindAllString(body, 12) {
			add(host)
		}
	}
	for _, host := range huangguaSeedHosts {
		add(host)
	}
	return out
}

func (d *Downloader) huangguaHostWorks(ctx context.Context, host string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	body, err := d.fetchProviderText(huangguaContext(probeCtx), host+"/aiduanju", host+"/")
	return err == nil && strings.Contains(body, "黄瓜短剧") && strings.Contains(body, "/drama/")
}

func (d *Downloader) fetchHuangguaCategories() []nativeCategory {
	all := []nativeCategory{{Name: "全部"}}
	return append(all, huangguaCategories...)
}

func (d *Downloader) fetchHuangguaCatalogPage(ctx context.Context, page int, category string) ([]Drama, bool, error) {
	if page > 1 {
		// 站点 SSR 每类只渲染首屏，翻页交给前端接口，这里不重复请求。
		return nil, false, nil
	}
	if !validHuangguaCategory(category) {
		return nil, false, errors.New("黄瓜短剧分类无效")
	}
	host, err := d.huangguaHostForRequest(ctx)
	if err != nil {
		return nil, false, err
	}
	path := "/"
	if category != "" {
		path = "/" + category
	}
	body, err := d.fetchProviderText(huangguaContext(ctx), host+path, host+"/")
	if err != nil {
		return nil, false, err
	}
	items := parseHuangguaCards(body, host)
	if len(items) == 0 {
		// 站点限流时会返回没有卡片的精简页，按失败处理，避免把旧缓存标记成已刷新。
		return nil, false, errors.New("黄瓜短剧没有返回剧集，请稍后重试")
	}
	return items, false, nil
}

// huangguaCoversReady 判断缓存封面是否都已经是站内解密代理地址。
// 旧版本写入过空封面、全站默认分享图和未解密的 CDN 直链，这些都不是真实海报。
func huangguaCoversReady(items []nativeDrama) bool {
	for _, item := range items {
		cover := strings.TrimSpace(nativeText(item.Cover))
		if cover == "" || strings.Contains(cover, "social-default") || strings.Contains(cover, "pic.gyezbp.cn") {
			return false
		}
	}
	return true
}

// huangguaCardImage 从图片标签里取封面的站内代理路径和同一张图上的标题。
// 站点对封面做了懒加载，src 常常只是全站通用的占位图，真实地址在 data-src 一类属性里。
func huangguaCardImage(body string) (string, string) {
	cover, title := "", ""
	for _, tag := range huangguaImgTag.FindAllString(body, 16) {
		attributes := map[string]string{}
		for _, match := range huangguaImgAttr.FindAllStringSubmatch(tag, -1) {
			attributes[strings.ToLower(match[1])] = strings.TrimSpace(match[2])
		}
		path := ""
		for _, key := range []string{"data-src", "data-original", "data-lazy-src", "src", "srcset"} {
			if match := huangguaCoverPath.FindStringSubmatch(attributes[key]); len(match) == 2 {
				path = match[1]
				break
			}
		}
		if path == "" {
			continue
		}
		if cover == "" {
			cover, title = path, attributes["alt"]
		}
		if title != "" {
			break
		}
	}
	return cover, title
}

func huangguaMetaImage(body, host string) string {
	for _, pattern := range []*regexp.Regexp{huangguaOGImage, huangguaOGImageAlt} {
		match := pattern.FindStringSubmatch(body)
		if len(match) != 2 {
			continue
		}
		value := strings.TrimSpace(match[1])
		if strings.Contains(value, "social-default") {
			continue
		}
		switch {
		case strings.HasPrefix(value, "//"):
			return "https:" + value
		case strings.HasPrefix(value, "/"):
			return host + value
		case strings.HasPrefix(value, "http"):
			return value
		}
	}
	return ""
}

// parseHuangguaPayload 读取 Nuxt SSR 的内联数据。列表页里只有首屏第一张卡直出封面，
// 其余卡片的图片是懒加载占位图，真实封面、集数和热度都只在这里。
func parseHuangguaPayload(body string) map[string]huangguaPayloadItem {
	match := huangguaPayloadTag.FindStringSubmatch(body)
	if len(match) != 2 {
		return nil
	}
	var values []any
	if err := json.Unmarshal([]byte(match[1]), &values); err != nil {
		return nil
	}
	items := make(map[string]huangguaPayloadItem)
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := row["cover"]; !ok {
			continue
		}
		slug := huangguaPayloadString(values, row["slug"])
		cover, fallback := huangguaPayloadCover(values, row["cover"])
		if slug == "" || (cover == "" && fallback == "") {
			continue
		}
		if _, exists := items[slug]; exists {
			continue
		}
		items[slug] = huangguaPayloadItem{
			Cover:    cover,
			Fallback: fallback,
			Title:    huangguaPayloadString(values, row["title"]),
			Episodes: huangguaPayloadInt(values, row["total_episode_count"]),
			Heat:     huangguaPayloadString(values, row["heat_7d"]),
		}
	}
	return items
}

// huangguaPayloadValue 按 devalue 的约定解引用：字段里的数字是扁平数组的下标，
// 字符串和数字本身只出现一次，所以只解一层。
func huangguaPayloadValue(values []any, value any) any {
	index, ok := value.(float64)
	if !ok {
		return value
	}
	if index < 0 || int(index) >= len(values) {
		return nil
	}
	return values[int(index)]
}

func huangguaPayloadString(values []any, value any) string {
	switch item := huangguaPayloadValue(values, value).(type) {
	case string:
		return strings.TrimSpace(item)
	case float64:
		return strconv.FormatFloat(item, 'f', -1, 64)
	}
	return ""
}

func huangguaPayloadInt(values []any, value any) int {
	number, err := strconv.Atoi(huangguaPayloadString(values, value))
	if err != nil || number < 0 || number > 99999 {
		return 0
	}
	return number
}

// huangguaPayloadCover 取封面对象里的加密地址和站点自带的备用图。
func huangguaPayloadCover(values []any, value any) (string, string) {
	row, ok := huangguaPayloadValue(values, value).(map[string]any)
	if !ok {
		return huangguaPayloadString(values, value), ""
	}
	return huangguaPayloadString(values, row["url"]), huangguaPayloadString(values, row["fallback_url"])
}

func huangguaCoverAddress(host, url, fallback string) string {
	if proxy := huangguaCoverProxy(host, url); proxy != "" {
		return proxy
	}
	fallback = strings.TrimSpace(fallback)
	if strings.HasPrefix(fallback, "/") && !strings.HasPrefix(fallback, "//") {
		return host + fallback
	}
	if strings.HasPrefix(fallback, "http") {
		return fallback
	}
	return ""
}

// huangguaCoverProxy 把 CDN 封面换成站点的解密代理地址，直连拿到的都是密文。
func huangguaCoverProxy(host, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/_img/") {
		return host + raw
	}
	if !strings.HasPrefix(raw, "http") || strings.ContainsAny(raw, " \t\r\n") {
		return ""
	}
	extension := ""
	if dot := strings.LastIndex(raw, "."); dot > 0 && len(raw)-dot <= 6 && !strings.ContainsAny(raw[dot:], "/?") {
		extension = raw[dot:]
	}
	if extension == "" {
		extension = ".jpeg"
	}
	return host + "/_img/" + base64.RawStdEncoding.EncodeToString([]byte(raw)) + extension
}

func parseHuangguaCards(body, host string) []Drama {
	segments := huangguaCardSplit.Split(body, -1)
	items := make([]Drama, 0, len(segments))
	if len(segments) < 2 {
		return items
	}
	payload := parseHuangguaPayload(body)
	seen := map[string]bool{}
	for _, segment := range segments[1:] {
		alias := ""
		if match := huangguaAlias.FindStringSubmatch(segment); len(match) == 2 {
			alias = match[1]
		}
		if alias == "" || seen[alias] {
			continue
		}
		seen[alias] = true
		coverPath, imageAlt := huangguaCardImage(segment)
		title := imageAlt
		if title == "" {
			if match := huangguaAlt.FindStringSubmatch(segment); len(match) == 2 {
				title = strings.TrimSpace(match[1])
			}
		}
		entry, inPayload := payload[alias]
		if title == "" && inPayload {
			title = entry.Title
		}
		if title == "" {
			continue
		}
		cover := ""
		if coverPath != "" {
			// 站点 CDN 上的封面是加密存储的，/_img/ 是站点的解密代理端点，
			// 必须保留站内路径作为封面地址，不能解码出原始 CDN 地址直连。
			cover = host + coverPath
		} else if inPayload {
			cover = huangguaCoverAddress(host, entry.Cover, entry.Fallback)
		}
		episodes := 0
		if match := huangguaEpisodeText.FindStringSubmatch(segment); len(match) == 2 {
			episodes, _ = strconv.Atoi(match[1])
		}
		if episodes == 0 && inPayload {
			episodes = entry.Episodes
		}
		heat := ""
		if match := huangguaHeatText.FindStringSubmatch(segment); len(match) == 2 {
			heat = match[1] + "热度"
		}
		if heat == "" && inPayload && entry.Heat != "" {
			heat = entry.Heat + "热度"
		}
		items = append(items, Drama{
			ID: providerDramaID(sourceHuanggua, alias), Source: sourceHuanggua, SourceID: alias,
			Title: title, Name: title, Cover: cover, CoverURL: cover,
			TotalEpisode: strconv.Itoa(episodes), EpisodeCount: strconv.Itoa(episodes),
			Views: heat,
		})
	}
	return items
}

func (d *Downloader) fetchHuangguaDetail(ctx context.Context, alias string) (Drama, []Chapter, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" || strings.ContainsAny(alias, "/?#") {
		return Drama{}, nil, errors.New("黄瓜短剧剧集 ID 无效")
	}
	host, err := d.huangguaHostForRequest(ctx)
	if err != nil {
		return Drama{}, nil, err
	}
	pageURL := host + "/drama/" + alias
	body, err := d.fetchProviderText(huangguaContext(ctx), pageURL, host+"/")
	if err != nil {
		return Drama{}, nil, err
	}
	title := ""
	if match := huangguaH1.FindStringSubmatch(body); len(match) == 2 {
		title = strings.TrimSpace(huangguaHTMLTag.ReplaceAllString(match[1], ""))
	}
	if title == "" {
		return Drama{}, nil, errors.New("黄瓜短剧未返回剧集信息")
	}
	desc := ""
	if match := huangguaMetaDesc.FindStringSubmatch(body); len(match) == 2 {
		desc = strings.TrimSpace(match[1])
	}
	cover := ""
	if entry, found := parseHuangguaPayload(body)[alias]; found {
		cover = huangguaCoverAddress(host, entry.Cover, entry.Fallback)
	}
	if cover == "" {
		if coverPath, _ := huangguaCardImage(body); coverPath != "" {
			cover = host + coverPath
		}
	}
	if cover == "" {
		cover = huangguaMetaImage(body, host)
	}
	chapters := huangguaChapters(body, host, alias)
	drama := Drama{
		ID: providerDramaID(sourceHuanggua, alias), Source: sourceHuanggua, SourceID: alias,
		Title: title, Name: title, Desc: desc, Intro: desc, Cover: cover, CoverURL: cover,
		ChannelName: "短剧", TotalEpisode: strconv.Itoa(len(chapters)), EpisodeCount: strconv.Itoa(len(chapters)),
	}
	return drama, chapters, nil
}

func huangguaChapters(body, host, alias string) []Chapter {
	pattern := regexp.MustCompile(`/play/` + regexp.QuoteMeta(alias) + `/(\d+)`)
	numbers := map[int]bool{}
	for _, match := range pattern.FindAllStringSubmatch(body, -1) {
		if number, err := strconv.Atoi(match[1]); err == nil && number > 0 && number <= 9999 {
			numbers[number] = true
		}
	}
	if len(numbers) == 0 {
		numbers[1] = true
	}
	ordered := make([]int, 0, len(numbers))
	for number := range numbers {
		ordered = append(ordered, number)
	}
	sort.Ints(ordered)
	chapters := make([]Chapter, 0, len(ordered))
	for _, number := range ordered {
		chapters = append(chapters, Chapter{
			ID:             providerChapterID(sourceHuanggua, alias, "ep-"+strconv.Itoa(number)),
			Source:         sourceHuanggua,
			Title:          fmt.Sprintf("第%d集", number),
			PageURL:        host + "/play/" + url.PathEscape(alias) + "/" + strconv.Itoa(number),
			CurrentEpisode: rawEpisode(number),
		})
	}
	return chapters
}

// resolveHuangguaMedia 从播放页的 SSR 数据里取带签名的 m3u8 地址。
func (d *Downloader) resolveHuangguaMedia(ctx context.Context, task Task) (providerMedia, error) {
	chapter := task.Chapter
	pageURL := chapter.PageURL
	if pageURL == "" {
		if _, sourceID, valid := splitProviderDramaID(task.DramaID); valid {
			episode := chapterEpisodeNumber(chapter, task.Index)
			host, err := d.huangguaHostForRequest(ctx)
			if err != nil {
				return providerMedia{}, err
			}
			pageURL = host + "/play/" + url.PathEscape(sourceID) + "/" + strconv.Itoa(episode)
		}
	}
	if pageURL == "" {
		return providerMedia{}, errors.New("黄瓜短剧播放地址无效")
	}
	body, err := d.fetchProviderText(huangguaContext(ctx), pageURL, pageURL)
	if err != nil {
		return providerMedia{}, err
	}
	address := parseHuangguaPlayURL(body)
	if address == "" {
		return providerMedia{}, errors.New("黄瓜短剧没有返回播放地址")
	}
	return providerMedia{URL: address, Referer: pageURL}, nil
}

func parseHuangguaPlayURL(body string) string {
	if match := huangguaPlayURL.FindStringSubmatch(body); len(match) == 2 && strings.HasPrefix(match[1], "http") {
		return unescapeHuangguaURL(match[1])
	}
	if match := huangguaEscapedM3U8.FindString(body); match != "" {
		return unescapeHuangguaURL(match)
	}
	if match := regexp.MustCompile(`https?://[^"'\s]+\.m3u8[^"'\s]*`).FindString(body); match != "" {
		return unescapeHuangguaURL(match)
	}
	return ""
}

func unescapeHuangguaURL(raw string) string {
	replaced := strings.NewReplacer(`\u002F`, "/", `\u0026`, "&", `\/`, "/").Replace(raw)
	if unquoted, err := strconv.Unquote(`"` + strings.ReplaceAll(replaced, `"`, `\"`) + `"`); err == nil {
		replaced = unquoted
	}
	return replaced
}

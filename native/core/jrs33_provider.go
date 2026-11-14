package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	xhtml "golang.org/x/net/html"
)

const (
	jrs33MatchesCacheLiveTTL = 20 * time.Second
	jrs33MatchesCacheIdleTTL = 2 * time.Minute
	jrs33MaxMatches = 80
)

var (
	jrs33IndexScriptRE = regexp.MustCompile(`(?i)<script\b[^>]*\bsrc\s*=\s*["']([^"']*index\.js[^"']*)["'][^>]*>`)
	jrs33PlayHostRE = regexp.MustCompile(`(?i)([a-zA-Z0-9_]+)\s*:\s*atob\(\s*["']([A-Za-z0-9+/=]+)["']\s*\)`)
	jrs33PlayPathRE = regexp.MustCompile(`(?i)host\s*\+\s*["']([^"']*)["']\s*\+\s*id\s*\+\s*["']([^"']*)["']`)
	jrs33WriteRE = regexp.MustCompile(`(?s)document\.write\(\s*'(([^'\\]|\\.)*('\s*\+\s*getPlayUrl\("\w+",\s*"\w+"\)\s*\+\s*'([^'\\]|\\.)*)*)'\s*\);`)
	jrs33DateTimeRE = regexp.MustCompile(`(?i)(\d{1,2})[-月/](\d{1,2})日?\s+(\d{1,2}):(\d{2})`)
	jrs33ScoreRE = regexp.MustCompile(`(\d{1,2})\s*[:：-]\s*(\d{1,2})`)
	jrs33ChannelLabelRE = regexp.MustCompile(`(直播|信号|线路|高清|超清|蓝光|源)\s*[①-⑳0-9]`)
)

type jrs33LiveChannel struct {
	Name string `json:"name"`
	URL string `json:"url"`
}

type jrs33Match struct {
	ID string `json:"id"`
	League string `json:"league"`
	HomeTeam string `json:"homeTeam"`
	AwayTeam string `json:"awayTeam"`
	Kickoff string `json:"kickoff,omitempty"`
	Status string `json:"status"`
	HomeScore *int `json:"homeScore,omitempty"`
	AwayScore *int `json:"awayScore,omitempty"`
	DetailURL string `json:"detailUrl,omitempty"`
	Channels []jrs33LiveChannel `json:"channels,omitempty"`
}

type jrs33Provider struct {
	domains *jrs33DomainManager
	cachePath string
	mu sync.Mutex
	loaded bool
	cached []jrs33Match
	cachedAt time.Time
	detailChannels map[string][]jrs33LiveChannel
}

type jrs33MatchCache struct {
	Version int `json:"version"`
	CachedAt time.Time `json:"cachedAt"`
	Items []jrs33Match `json:"items"`
}

func newJRS33Provider(domains *jrs33DomainManager) *jrs33Provider {
	return &jrs33Provider{domains: domains, cachePath: filepath.Join(filepath.Dir(domains.cachePath), "matches.json"), detailChannels: make(map[string][]jrs33LiveChannel)}
}

func (p *jrs33Provider) matches(ctx context.Context, force bool) ([]jrs33Match, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loadCacheLocked()
	now := time.Now()
	if !force && len(p.cached) > 0 {
		ttl := jrs33MatchesCacheIdleTTL
		for _, match := range p.cached {
			if match.Status == "live" || match.Status == "halftime" {
				ttl = jrs33MatchesCacheLiveTTL
				break
			}
		}
		if now.Sub(p.cachedAt) >= 0 && now.Sub(p.cachedAt) < ttl {
			return cloneJRS33Matches(p.cached), "", nil
		}
	}
	resolved, err := p.domains.resolve(ctx)
	if err != nil {
		if len(p.cached) > 0 {
			return cloneJRS33Matches(p.cached), "JRS33 暂时不可访问，当前显示缓存赛事：" + err.Error(), nil
		}
		return nil, "", err
	}
	matches, err := p.fetchMatches(ctx, resolved.BaseURL)
	if err != nil {
		if len(p.cached) > 0 {
			return cloneJRS33Matches(p.cached), "JRS33 更新失败，当前显示缓存赛事：" + err.Error(), nil
		}
		return nil, "", err
	}
	p.cached = cloneJRS33Matches(matches)
	p.cachedAt = now
	cacheBody, _ := json.Marshal(jrs33MatchCache{Version: 1, CachedAt: now, Items: p.cached})
	_ = writeNativeCacheFile(p.cachePath, cacheBody)
	return matches, "", nil
}

// matchChannels fetches a match detail page only when the user expands its channel list.
// The list endpoint therefore stays fast and avoids one request per fixture.
func (p *jrs33Provider) matchChannels(ctx context.Context, matchID string) ([]jrs33LiveChannel, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" { return nil, errors.New("赛事编号不能为空") }
	p.mu.Lock()
	p.loadCacheLocked()
	for _, match := range p.cached {
		if match.ID != matchID { continue }
		if len(match.Channels) > 0 {
			channels := append([]jrs33LiveChannel(nil), match.Channels...)
			p.mu.Unlock()
			return channels, nil
		}
		if channels, ok := p.detailChannels[matchID]; ok {
			channels = append([]jrs33LiveChannel(nil), channels...)
			p.mu.Unlock()
			return channels, nil
		}
		detailURL := strings.TrimSpace(match.DetailURL)
		p.mu.Unlock()
		if detailURL == "" { return nil, errors.New("该赛事没有详情页地址") }
		parsed, err := url.Parse(detailURL)
		if err != nil || parsed.Scheme != "https" || !isJRS33Host(parsed.Hostname()) {
			return nil, errors.New("赛事详情页地址无效，已拒绝请求")
		}
		body, finalURL, err := p.getText(ctx, detailURL, parsed.Scheme+"://"+parsed.Host)
		if err != nil { return nil, fmt.Errorf("读取赛事详情失败: %w", err) }
		if isJRS33ChallengePage(body) { return nil, errors.New("赛事详情触发 Cloudflare/反爬验证") }
		channels := parseJRS33Channels(body, finalURL)
		p.mu.Lock()
		p.detailChannels[matchID] = append([]jrs33LiveChannel(nil), channels...)
		p.mu.Unlock()
		return channels, nil
	}
	p.mu.Unlock()
	return nil, errors.New("找不到对应赛事，请刷新赛事列表")
}

func (p *jrs33Provider) loadCacheLocked() {
	if p.loaded { return }
	p.loaded = true
	body, err := os.ReadFile(p.cachePath)
	if err != nil || len(body) > 4<<20 { return }
	var cache jrs33MatchCache
	if json.Unmarshal(body, &cache) != nil || cache.Version != 1 || cache.CachedAt.IsZero() { return }
	for _, item := range cache.Items {
		if item.ID == "" || item.HomeTeam == "" || item.AwayTeam == "" { return }
	}
	p.cached = cloneJRS33Matches(cache.Items)
	p.cachedAt = cache.CachedAt
}

func cloneJRS33Matches(matches []jrs33Match) []jrs33Match {
	result := make([]jrs33Match, len(matches))
	copy(result, matches)
	for i := range result {
		result[i].Channels = append([]jrs33LiveChannel(nil), matches[i].Channels...)
		if matches[i].HomeScore != nil { v := *matches[i].HomeScore; result[i].HomeScore = &v }
		if matches[i].AwayScore != nil { v := *matches[i].AwayScore; result[i].AwayScore = &v }
	}
	return result
}

func (p *jrs33Provider) fetchMatches(ctx context.Context, base string) ([]jrs33Match, error) {
	homeURL := strings.TrimRight(base, "/") + "/?lan=0"
	home, finalURL, err := p.getText(ctx, homeURL, "")
	if err != nil { return nil, fmt.Errorf("读取 JRS33 首页失败: %w", err) }
	if isJRS33ChallengePage(home) {
		return nil, errors.New("JRS33 返回 Cloudflare/反爬验证页，赛事数据未获取成功")
	}
	hasIndexScript := jrs33IndexScriptRE.MatchString(home)
	expanded, expandErr := p.expandIndexScript(ctx, home, finalURL)
	if expandErr == nil { home = expanded }
	matches := parseJRS33Home(home, finalURL)
	if len(matches) > jrs33MaxMatches { matches = matches[:jrs33MaxMatches] }
	if len(matches) == 0 {
		if hasIndexScript && expandErr != nil {
			return nil, fmt.Errorf("JRS33 动态赛事列表解析失败: %w", expandErr)
		}
		if !hasIndexScript && !strings.Contains(home, "暂无赛事") && !strings.Contains(home, "隐藏 0 场赛事") {
			return nil, errors.New("JRS33 页面未找到预期的赛事列表结构")
		}
		return []jrs33Match{}, nil
	}
	return matches, nil
}

func (p *jrs33Provider) getText(ctx context.Context, rawURL, referer string) (string, string, error) {
	client := p.domains.client
	if client == nil { client = &http.Client{Timeout: 12 * time.Second} }
	requestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, rawURL, nil)
	if err != nil { return "", "", err }
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/javascript,*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if referer != "" { req.Header.Set("Referer", referer) }
	requestClient := *client
	requestClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 { return errors.New("JRS33 页面重定向次数过多") }
		if request.URL.Scheme != "https" || !isJRS33Host(request.URL.Hostname()) {
			return errors.New("JRS33 页面跳转到非可信域名，已拒绝")
		}
		if client.CheckRedirect != nil { return client.CheckRedirect(request, via) }
		return nil
	}
	response, err := requestClient.Do(req)
	if err != nil { return "", "", err }
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 || response.Request == nil || response.Request.URL == nil {
		return "", "", fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if response.Request.URL.Scheme != "https" || !isJRS33Host(response.Request.URL.Hostname()) {
		return "", "", errors.New("请求跳转到非 JRS33 域名，已拒绝")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, jrs33MaxPageBytes+1))
	if err != nil { return "", "", err }
	if len(body) > jrs33MaxPageBytes { return "", "", errors.New("页面超过大小限制") }
	return string(body), response.Request.URL.String(), nil
}

func (p *jrs33Provider) expandIndexScript(ctx context.Context, home, baseURL string) (string, error) {
	m := jrs33IndexScriptRE.FindStringSubmatch(home)
	if len(m) < 2 { return home, nil }
	base, err := url.Parse(baseURL)
	if err != nil { return home, err }
	ref, err := url.Parse(stdhtml.UnescapeString(m[1]))
	if err != nil { return home, err }
	scriptURL := base.ResolveReference(ref)
	if scriptURL.Scheme != "https" || !isJRS33Host(scriptURL.Hostname()) {
		return home, errors.New("赛事脚本地址不属于 JRS33 域名")
	}
	script, _, err := p.getText(ctx, scriptURL.String(), baseURL)
	if err != nil { return home, err }
	hosts := make(map[string]string)
	for _, host := range jrs33PlayHostRE.FindAllStringSubmatch(home, -1) {
		decoded, decodeErr := base64.StdEncoding.DecodeString(host[2])
		if decodeErr == nil { hosts[host[1]] = strings.TrimRight(strings.TrimSpace(string(decoded)), "/") }
	}
	path := jrs33PlayPathRE.FindStringSubmatch(home)
	if len(hosts) == 0 || len(path) < 3 { return home, errors.New("首页未提供直播入口映射信息") }
	writes := jrs33WriteRE.FindAllStringSubmatch(script, -1)
	if len(writes) == 0 { return home, errors.New("赛事脚本未找到 document.write 列表") }
	var output strings.Builder
	callRE := regexp.MustCompile(`getPlayUrl\("(\w+)",\s*"(\w+)"\)`)
	for _, write := range writes {
		fragment := callRE.ReplaceAllStringFunc(write[1], func(call string) string {
			part := callRE.FindStringSubmatch(call)
			if len(part) < 3 || hosts[part[1]] == "" { return "" }
			return hosts[part[1]] + path[1] + part[2] + path[2]
		})
		fragment = strings.ReplaceAll(fragment, `\'`, `'`)
		fragment = strings.ReplaceAll(fragment, `\\`, `\`)
		output.WriteString(fragment)
		output.WriteByte('\n')
	}
	if output.Len() == 0 { return home, errors.New("赛事脚本没有生成比赛列表") }
	return output.String(), nil
}

func parseJRS33Home(source, baseURL string) []jrs33Match {
	doc, err := xhtml.Parse(strings.NewReader(source))
	if err != nil { return nil }
	rows := findJRS33Elements(doc, func(n *xhtml.Node) bool { return hasJRS33Class(n, "d-touch") })
	matches := make([]jrs33Match, 0, len(rows))
	seen := make(map[string]bool)
	for _, row := range rows {
		names := findJRS33Elements(row, func(n *xhtml.Node) bool { return hasJRS33Class(n, "name") })
		if len(names) < 3 { continue }
		league := cleanJRS33Text(jrs33NodeText(names[0]))
		home := cleanJRS33Text(jrs33NodeText(names[1]))
		away := cleanJRS33Text(jrs33NodeText(names[2]))
		if home == "" || away == "" { continue }
		times := findJRS33Elements(row, func(n *xhtml.Node) bool { return hasJRS33Class(n, "lab_time") })
		timeText := ""
		if len(times) > 0 { timeText = cleanJRS33Text(jrs33NodeText(times[0])) }
		detailURL := ""
		for _, a := range findJRS33Elements(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "a" }) {
			if hasJRS33Class(a, "me") { detailURL = resolveJRS33Link(baseURL, jrs33Attr(a, "href")); break }
		}
		id := jrs33StableMatchID(detailURL, league, home, away, timeText)
		if seen[id] { continue }
		seen[id] = true
		item := jrs33Match{ID:id, League:league, HomeTeam:home, AwayTeam:away, Kickoff:parseJRS33Kickoff(timeText), Status:parseJRS33Status(cleanJRS33Text(jrs33NodeText(row))), DetailURL:detailURL}
		// 首页 index.js 已把每场比赛的编号直播入口写入对应比赛行，优先直接解析这些入口，
		// 避免为了列表而逐场请求详情页。
		channelSeen := make(map[string]bool)
		for _, a := range findJRS33Elements(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "a" }) {
			name := cleanJRS33Text(jrs33NodeText(a))
			if !jrs33ChannelLabelRE.MatchString(name) { continue }
			link := resolveJRS33ChannelLink(baseURL, jrs33Attr(a, "href"))
			if link == "" || channelSeen[link] { continue }
			channelSeen[link] = true
			item.Channels = append(item.Channels, jrs33LiveChannel{Name:name, URL:link})
		}
		scores := findJRS33Elements(row, func(n *xhtml.Node) bool { return hasJRS33Class(n, "score") || hasJRS33Class(n, "lab_score") })
		if len(scores) > 0 {
			if hs, as, ok := parseJRS33Score(jrs33NodeText(scores[0])); ok { item.HomeScore, item.AwayScore = &hs, &as }
		}
		matches = append(matches, item)
	}
	return matches
}

func parseJRS33Channels(source, baseURL string) []jrs33LiveChannel {
	doc, err := xhtml.Parse(strings.NewReader(source))
	if err != nil { return nil }
	containers := findJRS33Elements(doc, func(n *xhtml.Node) bool { return hasJRS33Class(n, "sub_playlist") })
	seen := make(map[string]bool)
	var result []jrs33LiveChannel
	for _, container := range containers {
		anchors := findJRS33Elements(container, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "a" })
		for _, a := range anchors {
			raw := jrs33Attr(a, "data-play")
			if raw == "" { continue }
			name := ""
			strong := findJRS33Elements(a, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "strong" })
			if len(strong) > 0 { name = cleanJRS33Text(jrs33NodeText(strong[0])) }
			if name == "" { name = cleanJRS33Text(jrs33NodeText(a)) }
			link := resolveJRS33ChannelLink(baseURL, raw)
			if link == "" || name == "" || seen[link] { continue }
			seen[link] = true
			result = append(result, jrs33LiveChannel{Name:name, URL:link})
		}
	}
	return result
}

func findJRS33Elements(root *xhtml.Node, predicate func(*xhtml.Node) bool) []*xhtml.Node {
	var out []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if predicate(n) { out = append(out, n) }
		for child := n.FirstChild; child != nil; child = child.NextSibling { walk(child) }
	}
	walk(root)
	return out
}

func hasJRS33Class(n *xhtml.Node, class string) bool {
	if n == nil || n.Type != xhtml.ElementNode { return false }
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, token := range strings.Fields(a.Val) { if token == class { return true } }
		}
	}
	return false
}

func jrs33Attr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr { if strings.EqualFold(a.Key, key) { return strings.TrimSpace(a.Val) } }
	return ""
}

func jrs33NodeText(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(cur *xhtml.Node) {
		if cur.Type == xhtml.TextNode { b.WriteString(cur.Data); b.WriteByte(' ') }
		for child := cur.FirstChild; child != nil; child = child.NextSibling { walk(child) }
	}
	walk(n)
	return b.String()
}

func cleanJRS33Text(value string) string {
	value = stdhtml.UnescapeString(value)
	return strings.Join(strings.Fields(strings.ReplaceAll(value, " ", " ")), " ")
}

func resolveJRS33Link(baseURL, raw string) string {
	raw = strings.TrimSpace(stdhtml.UnescapeString(raw))
	if raw == "" || strings.HasPrefix(strings.ToLower(raw), "javascript:") || strings.HasPrefix(raw, "#") { return "" }
	base, err := url.Parse(baseURL)
	if err != nil { return "" }
	ref, err := url.Parse(raw)
	if err != nil { return "" }
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "https" || !isJRS33Host(resolved.Hostname()) { return "" }
	return resolved.String()
}

func resolveJRS33ChannelLink(baseURL, raw string) string {
	raw = strings.TrimSpace(stdhtml.UnescapeString(raw))
	if raw == "" || strings.HasPrefix(strings.ToLower(raw), "javascript:") || strings.HasPrefix(raw, "#") {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil { return "" }
	ref, err := url.Parse(raw)
	if err != nil { return "" }
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "https" || resolved.Host == "" || resolved.User != nil {
		return ""
	}
	if ip := net.ParseIP(resolved.Hostname()); ip != nil {
		return ""
	}
	return resolved.String()
}

func jrs33StableMatchID(detailURL, league, home, away, kickoff string) string {
	if detailURL != "" {
		if parsed, err := url.Parse(detailURL); err == nil {
			if path := strings.Trim(parsed.EscapedPath(), "/"); path != "" { return "jrs33:" + path }
		}
	}
	return "jrs33:" + strings.ToLower(strings.Join([]string{league, home, away, kickoff}, "|"))
}

func parseJRS33Kickoff(value string) string {
	m := jrs33DateTimeRE.FindStringSubmatch(value)
	if len(m) < 5 { return "" }
	month, _ := strconv.Atoi(m[1]); day, _ := strconv.Atoi(m[2])
	hour, _ := strconv.Atoi(m[3]); minute, _ := strconv.Atoi(m[4])
	now := time.Now()
	beijing := time.FixedZone("Asia/Shanghai", 8*60*60)
	t := time.Date(now.Year(), time.Month(month), day, hour, minute, 0, 0, beijing)
	if t.Before(now.AddDate(0, -1, 0)) { t = t.AddDate(1, 0, 0) }
	return t.Format(time.RFC3339)
}

func parseJRS33Status(value string) string {
	switch {
	case strings.Contains(value, "中场"), strings.Contains(value, "半场休息"): return "halftime"
	case strings.Contains(value, "进行中"), strings.Contains(value, "直播中"), strings.Contains(value, "上半场"), strings.Contains(value, "下半场"): return "live"
	case strings.Contains(value, "完场"), strings.Contains(value, "已结束"), strings.Contains(value, "全场"): return "finished"
	case strings.Contains(value, "延期"), strings.Contains(value, "推迟"): return "postponed"
	case strings.Contains(value, "取消"): return "cancelled"
	case strings.Contains(value, "未开赛"), strings.Contains(value, "赛前"): return "scheduled"
	default: return "scheduled"
	}
}

func parseJRS33Score(value string) (int, int, bool) {
	m := jrs33ScoreRE.FindStringSubmatch(cleanJRS33Text(value))
	if len(m) < 3 { return 0, 0, false }
	home, e1 := strconv.Atoi(m[1]); away, e2 := strconv.Atoi(m[2])
	return home, away, e1 == nil && e2 == nil
}

func marshalJRS33Matches(matches []jrs33Match) []map[string]any {
	out := make([]map[string]any, 0, len(matches))
	for _, m := range matches {
		item := map[string]any{"id":m.ID, "league":m.League, "homeTeam":m.HomeTeam, "awayTeam":m.AwayTeam, "status":m.Status, "channels":m.Channels, "detailUrl":m.DetailURL}
		if m.Kickoff != "" { item["kickoff"] = m.Kickoff }
		if m.HomeScore != nil { item["homeScore"] = *m.HomeScore }
		if m.AwayScore != nil { item["awayScore"] = *m.AwayScore }
		out = append(out, item)
	}
	return out
}

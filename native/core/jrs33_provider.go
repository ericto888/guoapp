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
	jrs33MatchesCacheLiveTTL = 15 * time.Second
	jrs33MatchesCacheIdleTTL = 30 * time.Second
	jrs33MaxMatches = 80
	// JRS33 currently hosts its dynamic fixture index on this exact Alibaba OSS
	// bucket hostname. Do not broaden this to aliyuncs.com or other OSS buckets.
	jrs33IndexScriptHost = "im-imgs-bucket.oss-accelerate.aliyuncs.com"
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
	MatchTime string `json:"matchTime,omitempty"`
	StatusEvidence string `json:"statusEvidence,omitempty"`
	HomeScore *int `json:"homeScore,omitempty"`
	AwayScore *int `json:"awayScore,omitempty"`
	HalfTimeHomeScore *int `json:"halfTimeHomeScore,omitempty"`
	HalfTimeAwayScore *int `json:"halfTimeAwayScore,omitempty"`
	CornerHome *int `json:"cornerHome,omitempty"`
	CornerAway *int `json:"cornerAway,omitempty"`
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
	activeBaseURL string
}

type jrs33MatchCache struct {
	Version int `json:"version"`
	CachedAt time.Time `json:"cachedAt"`
	BaseURL string `json:"baseUrl,omitempty"`
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
			if match.Status == "live" || match.Status == "halftime" || match.Status == "unknown" {
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
	matches, activeBaseURL, fallbackWarning, err := p.fetchMatchesWithFallback(ctx, resolved.BaseURL, resolved.Discovered)
	if err != nil {
		if len(p.cached) > 0 {
			return cloneJRS33Matches(p.cached), "JRS33 更新失败，当前显示缓存赛事：" + err.Error(), nil
		}
		return nil, "", err
	}
	p.activeBaseURL = activeBaseURL
	p.cached = cloneJRS33Matches(matches)
	p.cachedAt = now
	cacheBody, _ := json.Marshal(jrs33MatchCache{Version: 1, CachedAt: now, BaseURL: p.activeBaseURL, Items: p.cached})
	_ = writeNativeCacheFile(p.cachePath, cacheBody)
	return matches, fallbackWarning, nil
}

// matchChannels fetches a match detail page only when the user expands its channel list.
// The list endpoint therefore stays fast and avoids one request per fixture.
// resolveChannel converts a JRS33 channel landing page into a direct media URL.
 // It accepts only HTTPS pages on their original public hostname and never treats
 // an HTML page itself as a playable stream.
 func (p *jrs33Provider) resolveChannel(ctx context.Context, rawURL string) (string, map[string]string, error) {
 	parsed, err := url.Parse(strings.TrimSpace(rawURL))
 	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Port() != "" {
 		return "", nil, errors.New("直播频道地址无效，已拒绝请求")
 	}
 	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
 	if net.ParseIP(host) != nil { return "", nil, errors.New("直播频道主机无效，已拒绝请求") }
 	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
 	if err != nil || len(ips) == 0 { return "", nil, errors.New("直播频道域名暂时无法解析") }
 	for _, entry := range ips {
 		ip := entry.IP
 		if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
 			return "", nil, errors.New("直播频道解析到非公网地址，已拒绝请求")
 		}
 	}
 	body, finalURL, err := p.getTextWithHostPolicy(ctx, parsed.String(), parsed.Scheme+"://"+parsed.Host, host)
 	if err != nil { return "", nil, fmt.Errorf("读取直播频道失败: %w", err) }
 	if isJRS33ChallengePage(body) { return "", nil, errors.New("直播频道触发 Cloudflare/反爬验证") }
 	mediaURL := parseJRS33MediaURL(body, finalURL)
 	if mediaURL == "" { return "", nil, errors.New("频道页面没有找到可直接播放的 HLS/MP4 视频地址") }
 	return mediaURL, map[string]string{
 		"Referer": finalURL,
 		"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
 	}, nil
 }
 
 func (p *jrs33Provider) matchChannels(ctx context.Context, matchID string) ([]jrs33LiveChannel, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" { return nil, errors.New("赛事编号不能为空") }
	p.mu.Lock()
	p.loadCacheLocked()
	if p.detailChannels == nil {
		p.detailChannels = make(map[string][]jrs33LiveChannel)
	}
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
		if len(channels) > 0 {
			p.mu.Lock()
			p.detailChannels[matchID] = append([]jrs33LiveChannel(nil), channels...)
			p.mu.Unlock()
		}
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
	if validJRS33URL(cache.BaseURL) {
		p.activeBaseURL = cache.BaseURL
	}
}

func cloneJRS33Matches(matches []jrs33Match) []jrs33Match {
	result := make([]jrs33Match, len(matches))
	copy(result, matches)
	for i := range result {
		result[i].Channels = append([]jrs33LiveChannel(nil), matches[i].Channels...)
		if matches[i].HomeScore != nil { v := *matches[i].HomeScore; result[i].HomeScore = &v }
		if matches[i].AwayScore != nil { v := *matches[i].AwayScore; result[i].AwayScore = &v }
		if matches[i].HalfTimeHomeScore != nil { v := *matches[i].HalfTimeHomeScore; result[i].HalfTimeHomeScore = &v }
		if matches[i].HalfTimeAwayScore != nil { v := *matches[i].HalfTimeAwayScore; result[i].HalfTimeAwayScore = &v }
		if matches[i].CornerHome != nil { v := *matches[i].CornerHome; result[i].CornerHome = &v }
		if matches[i].CornerAway != nil { v := *matches[i].CornerAway; result[i].CornerAway = &v }
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
		if hasIndexScript {
			if expandErr != nil {
				return nil, fmt.Errorf("JRS33 动态赛事列表解析失败: %w", expandErr)
			}
			// A present and successfully downloaded index.js is not proof that the
			// fixture feed is empty. If its generated markup cannot be parsed, report
			// a parser failure instead of presenting a misleading "0 matches" state.
			return nil, errors.New("JRS33 动态赛事脚本已展开，但未解析出任何赛事行")
		}
		if !strings.Contains(home, "暂无赛事") && !strings.Contains(home, "隐藏 0 场赛事") {
			return nil, errors.New("JRS33 页面未找到预期的赛事列表结构")
		}
		return []jrs33Match{}, nil
	}
	return matches, nil
}

// fetchMatchesWithFallback avoids treating an empty primary mirror as the whole
// source being empty. JRKAN's own homepage publishes alternate domains; try
// those mirrors when the preferred domain returns no fixtures or an error.
func (p *jrs33Provider) fetchMatchesWithFallback(ctx context.Context, primary string, discovered []string) ([]jrs33Match, string, string, error) {
	candidates := []string{primary}
	candidates = append(candidates, discovered...)
	seen := make(map[string]bool)
	var firstErr error
	var emptyDomains []string
	attempts := 0
	for _, raw := range candidates {
		base := normalizeJRS33URL(raw)
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		if attempts >= 5 {
			break
		}
		attempts++
		matches, err := p.fetchMatches(ctx, base)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(matches) > 0 {
			warning := ""
			if base != normalizeJRS33URL(primary) {
				warning = "首选域名未返回赛事，已切换备用域名：" + base
			}
			return matches, base, warning, nil
		}
		emptyDomains = append(emptyDomains, base)
	}
	if firstErr != nil {
		return nil, "", "", fmt.Errorf("首选域名及备用域名未能确认赛事数据（已验证空列表域名：%s）：%w", strings.Join(emptyDomains, "、"), firstErr)
	}
	return []jrs33Match{}, normalizeJRS33URL(primary), "", nil
}

func (p *jrs33Provider) currentBaseURL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.activeBaseURL != "" {
		return p.activeBaseURL
	}
	return ""
}

func (p *jrs33Provider) getText(ctx context.Context, rawURL, referer string) (string, string, error) {
	return p.getTextWithHostPolicy(ctx, rawURL, referer, "")
}

// getTextFromIndexScript is deliberately narrower than getText: the initial
// script URL and every redirect must stay on the one observed OSS bucket host.
func (p *jrs33Provider) getTextFromIndexScript(ctx context.Context, rawURL, referer string) (string, string, error) {
	return p.getTextWithHostPolicy(ctx, rawURL, referer, jrs33IndexScriptHost)
}

func (p *jrs33Provider) getTextWithHostPolicy(ctx context.Context, rawURL, referer, exactHost string) (string, string, error) {
	allowedURL := func(target *url.URL) bool {
		if target == nil || target.Scheme != "https" || target.User != nil || target.Port() != "" {
			return false
		}
		host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
		if net.ParseIP(host) != nil {
			return false
		}
		if exactHost != "" {
			return host == exactHost
		}
		return isJRS33Host(host)
	}
	parsedURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !allowedURL(parsedURL) {
		return "", "", errors.New("请求地址不属于允许的 HTTPS 主机，已拒绝")
	}
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
		if !allowedURL(request.URL) {
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
	if !allowedURL(response.Request.URL) {
		return "", "", errors.New("请求跳转到非可信域名，已拒绝")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, jrs33MaxPageBytes+1))
	if err != nil { return "", "", err }
	if len(body) > jrs33MaxPageBytes { return "", "", errors.New("页面超过大小限制") }
	return string(body), response.Request.URL.String(), nil
}

func isJRS33IndexScriptURL(target *url.URL) bool {
	if target == nil || target.Scheme != "https" || target.User != nil || target.Port() != "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	return net.ParseIP(host) == nil && host == jrs33IndexScriptHost
}

func (p *jrs33Provider) expandIndexScript(ctx context.Context, home, baseURL string) (string, error) {
	m := jrs33IndexScriptRE.FindStringSubmatch(home)
	if len(m) < 2 { return home, nil }
	base, err := url.Parse(baseURL)
	if err != nil { return home, err }
	ref, err := url.Parse(stdhtml.UnescapeString(m[1]))
	if err != nil { return home, err }
	scriptURL := base.ResolveReference(ref)
	if !isJRS33IndexScriptURL(scriptURL) {
		return home, fmt.Errorf("赛事脚本地址不属于允许的赛事脚本主机：%s（主机名：%s）", scriptURL.String(), scriptURL.Hostname())
	}
	script, _, err := p.getTextFromIndexScript(ctx, scriptURL.String(), baseURL)
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
	matches := parseJRS33ClassRows(rows, baseURL)
	if len(matches) > 0 { return matches }
	// Some JRS mirrors render the same fixture feed as a conventional HTML table
	// instead of the mobile .d-touch cards. Fall back to the visible column headers;
	// never filter by league or gender (NBA and women's leagues are valid fixtures).
	return parseJRS33TableRows(doc, baseURL)
}

func parseJRS33ClassRows(rows []*xhtml.Node, baseURL string) []jrs33Match {
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
		item := buildJRS33MatchFromRow(row, baseURL, league, home, away, timeText)
		if item.ID == "" || seen[item.ID] { continue }
		seen[item.ID] = true
		matches = append(matches, item)
	}
	return matches
}

func parseJRS33TableRows(doc *xhtml.Node, baseURL string) []jrs33Match {
	rows := findJRS33Elements(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "tr" })
	var header []string
	for _, row := range rows {
		cells := directJRS33Cells(row)
		if len(cells) < 3 { continue }
		values := make([]string, len(cells))
		for i, cell := range cells { values[i] = cleanJRS33Text(jrs33NodeText(cell)) }
		hasHome, hasAway := false, false
		for _, value := range values {
			if strings.Contains(value, "主队") || strings.EqualFold(value, "home team") { hasHome = true }
			if strings.Contains(value, "客队") || strings.EqualFold(value, "away team") { hasAway = true }
		}
		if hasHome && hasAway { header = values; break }
	}
	if len(header) == 0 { return nil }
	index := func(keys ...string) int {
		for i, value := range header {
			for _, key := range keys {
				if strings.Contains(strings.ToLower(value), strings.ToLower(key)) { return i }
			}
		}
		return -1
	}
	leagueCol := index("赛事", "联赛", "league")
	timeCol := index("时间状态", "开赛时间", "时间", "time")
	homeCol := index("主队", "home team")
	awayCol := index("客队", "away team")
	scoreCol := index("比分", "score")
	halfCol := index("半场", "半场比分", "halftime")
	cornerCol := index("角球", "corners")
	if homeCol < 0 || awayCol < 0 { return nil }
	var result []jrs33Match
	seen := make(map[string]bool)
	for _, row := range rows {
		cells := directJRS33Cells(row)
		if len(cells) <= homeCol || len(cells) <= awayCol { continue }
		home := cleanJRS33Text(jrs33NodeText(cells[homeCol]))
		away := cleanJRS33Text(jrs33NodeText(cells[awayCol]))
		if home == "" || away == "" || strings.EqualFold(home, "home team") || strings.EqualFold(away, "away team") || home == "主队" || away == "客队" { continue }
		league, timeText := "", ""
		if leagueCol >= 0 && leagueCol < len(cells) { league = cleanJRS33Text(jrs33NodeText(cells[leagueCol])) }
		if timeCol >= 0 && timeCol < len(cells) { timeText = cleanJRS33Text(jrs33NodeText(cells[timeCol])) }
		item := buildJRS33MatchFromRow(row, baseURL, league, home, away, timeText)
		if scoreCol >= 0 && scoreCol < len(cells) {
			if hs, as, ok := parseJRS33Score(jrs33NodeText(cells[scoreCol])); ok { item.HomeScore, item.AwayScore = &hs, &as }
		}
		if halfCol >= 0 && halfCol < len(cells) {
			if hs, as, ok := parseJRS33Score(jrs33NodeText(cells[halfCol])); ok { item.HalfTimeHomeScore, item.HalfTimeAwayScore = &hs, &as }
		}
		if cornerCol >= 0 && cornerCol < len(cells) {
			if hs, as, ok := parseJRS33Score(jrs33NodeText(cells[cornerCol])); ok { item.CornerHome, item.CornerAway = &hs, &as }
		}
		if item.ID == "" || seen[item.ID] { continue }
		seen[item.ID] = true
		result = append(result, item)
	}
	return result
}

func directJRS33Cells(row *xhtml.Node) []*xhtml.Node {
	var cells []*xhtml.Node
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode && (child.Data == "td" || child.Data == "th") { cells = append(cells, child) }
	}
	return cells
}

func buildJRS33MatchFromRow(row *xhtml.Node, baseURL, league, home, away, timeText string) jrs33Match {
	detailURL := ""
	for _, a := range findJRS33Elements(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "a" }) {
		if hasJRS33Class(a, "me") { detailURL = resolveJRS33Link(baseURL, jrs33Attr(a, "href")); break }
	}
	id := jrs33StableMatchID(detailURL, league, home, away, timeText)
	status, matchTime := parseJRS33RowStatus(row, timeText)
	item := jrs33Match{ID:id, League:league, HomeTeam:home, AwayTeam:away, Kickoff:parseJRS33Kickoff(timeText), Status:status, MatchTime:matchTime, DetailURL:detailURL}
	if status == "unknown" {
		item.StatusEvidence = jrs33StatusEvidence(row, timeText)
	}
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
	if len(scores) > 0 && item.HomeScore == nil {
		if hs, as, ok := parseJRS33Score(jrs33NodeText(scores[0])); ok { item.HomeScore, item.AwayScore = &hs, &as }
	}
	halfNodes := findJRS33Elements(row, func(n *xhtml.Node) bool { return hasJRS33Class(n, "half") || hasJRS33Class(n, "half_score") || hasJRS33Class(n, "halftime") || hasJRS33Class(n, "lab_half") })
	if len(halfNodes) > 0 { if hs, as, ok := parseJRS33Score(jrs33NodeText(halfNodes[0])); ok { item.HalfTimeHomeScore, item.HalfTimeAwayScore = &hs, &as } }
	cornerNodes := findJRS33Elements(row, func(n *xhtml.Node) bool { return hasJRS33Class(n, "corner") || hasJRS33Class(n, "corners") || hasJRS33Class(n, "lab_corner") })
	if len(cornerNodes) > 0 { if hs, as, ok := parseJRS33Score(jrs33NodeText(cornerNodes[0])); ok { item.CornerHome, item.CornerAway = &hs, &as } }
	return item
}

var jrs33MediaExtensionRE = regexp.MustCompile("(?i)\\.(?:m3u8|mp4|m4v|mov|flv)(?:[?#][^\\s\"'<>]*)?")
var jrs33MediaAttributeRE = regexp.MustCompile("(?i)(?:url|src|file|playurl|m3u8)\\s*[:=]\\s*[\"']([^\"']+)[\"']")
 
 // parseJRS33MediaURL extracts explicit media resources from HTML and common player config.
 func parseJRS33MediaURL(source, baseURL string) string {
 	candidates := []string{}
 	doc, err := xhtml.Parse(strings.NewReader(source))
 	if err == nil {
 		for _, n := range findJRS33Elements(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode }) {
 			if n.Data != "video" && n.Data != "source" && n.Data != "iframe" { continue }
 			for _, key := range []string{"src", "data-src", "data-url"} {
 				if value := jrs33Attr(n, key); value != "" { candidates = append(candidates, value) }
 			}
 		}
 	}
 	for _, match := range jrs33MediaAttributeRE.FindAllStringSubmatch(source, -1) {
 		if len(match) > 1 { candidates = append(candidates, match[1]) }
 	}
 	for _, match := range jrs33MediaExtensionRE.FindAllStringIndex(source, -1) {
 		start, end := match[0], match[1]
 		left := start
		for left > 0 && !strings.ContainsRune("\"'<> \r\n\t", rune(source[left-1])) { left-- }
 		if left < start { candidates = append(candidates, source[left:end]) }
 	}
 	for _, raw := range candidates {
 		raw = strings.TrimSpace(raw)
		raw = strings.Trim(raw, "\"'")
		raw = strings.ReplaceAll(raw, "\\/", "/")
		raw = strings.ReplaceAll(raw, "\\u0026", "&")
 		raw = stdhtml.UnescapeString(raw)
 		if !jrs33MediaExtensionRE.MatchString(raw) { continue }
 		base, baseErr := url.Parse(baseURL)
 		ref, refErr := url.Parse(raw)
 		if baseErr != nil || refErr != nil { continue }
 		resolved := base.ResolveReference(ref)
 		if resolved.Scheme != "https" || resolved.Hostname() == "" || resolved.User != nil || resolved.Port() != "" { continue }
 		if ip := net.ParseIP(resolved.Hostname()); ip != nil { continue }
 		return resolved.String()
 	}
 	return ""
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

var jrs33MinuteRE = regexp.MustCompile("(?i)(?:第\\s*)?(\\d{1,3})\\s*(?:['′’]|分钟|分)(?:钟)?")
var jrs33StatusClassRE = regexp.MustCompile("(?i)(?:^|\\s)(?:live|playing|ing|half|halftime|finished|end|schedule|upcoming)(?:\\s|$)")

func parseJRS33RowStatus(row *xhtml.Node, timeText string) (string, string) {
	status := parseJRS33Status(timeText)
	minute := parseJRS33MatchTime(timeText)
	if status != "unknown" && minute != "" {
		return status, minute
	}
	for _, n := range findJRS33Elements(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode }) {
		class := strings.ToLower(jrs33Attr(n, "class"))
		isStatusNode := strings.Contains(class, "status") ||
			strings.Contains(class, "lab_time") ||
			strings.Contains(class, "time_status") ||
			strings.Contains(class, "match_time")
		if isStatusNode {
			value := cleanJRS33Text(jrs33NodeText(n))
			nodeStatus := parseJRS33Status(value)
			nodeMinute := parseJRS33MatchTime(value)
			if nodeStatus != "unknown" {
				status = nodeStatus
			}
			if nodeMinute != "" {
				minute = nodeMinute
			}
			if status != "unknown" {
				return status, minute
			}
		}
		for _, key := range []string{"data-status", "data-time", "data-minute", "data-period", "aria-label", "title"} {
			value := jrs33Attr(n, key)
			if value == "" {
				continue
			}
			attrStatus := parseJRS33Status(value)
			attrMinute := parseJRS33MatchTime(value)
			if attrStatus != "unknown" {
				status = attrStatus
			}
			if attrMinute != "" {
				minute = attrMinute
			}
			if status != "unknown" {
				return status, minute
			}
		}
	}
	rowText := jrs33RowStatusText(row, timeText)
	if status == "unknown" {
		status = parseJRS33Status(rowText)
	}
	if minute == "" {
		minute = parseJRS33MatchTime(rowText)
	}
	return status, minute
}

// jrs33StatusEvidence preserves the actual status-related markup seen on the
// source row. It is returned only for unknown states so a future parser fix can
// be based on real JRS33 markup instead of adding speculative class names.
func jrs33StatusEvidence(row *xhtml.Node, timeText string) string {
	parts := []string{}
	if value := cleanJRS33Text(timeText); value != "" { parts = append(parts, "time="+value) }
	for _, n := range findJRS33Elements(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode }) {
		class := strings.TrimSpace(jrs33Attr(n, "class"))
		attrs := []string{}
		for _, key := range []string{"data-status", "data-time", "data-minute", "data-period", "aria-label", "title"} {
			if value := cleanJRS33Text(jrs33Attr(n, key)); value != "" { attrs = append(attrs, key+"="+value) }
		}
		if class == "" && len(attrs) == 0 { continue }
		text := cleanJRS33Text(jrs33NodeText(n))
		if len(text) > 100 { text = text[:100] }
		part := n.Data
		if class != "" { part += "."+class }
		if text != "" { part += " text="+text }
		if len(attrs) > 0 { part += " "+strings.Join(attrs, " ") }
		parts = append(parts, part)
	}
	evidence := strings.Join(parts, " | ")
	if len(evidence) > 1200 { evidence = evidence[:1200] }
	return evidence
}

func jrs33RowStatusText(row *xhtml.Node, timeText string) string {
	parts := []string{timeText, cleanJRS33Text(jrs33NodeText(row))}
	for _, n := range findJRS33Elements(row, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode }) {
		for _, key := range []string{"title", "aria-label", "data-status", "data-time", "data-minute", "data-period"} {
			if value := jrs33Attr(n, key); value != "" {
				parts = append(parts, value)
			}
		}
		class := strings.ToLower(jrs33Attr(n, "class"))
		if class != "" && (jrs33StatusClassRE.MatchString(class) ||
			strings.Contains(class, "lab_time") || strings.Contains(class, "lab_status") ||
			strings.Contains(class, "match_status") || strings.Contains(class, "time_status")) {
			parts = append(parts, class, cleanJRS33Text(jrs33NodeText(n)))
		}
	}
	return cleanJRS33Text(strings.Join(parts, " "))
}

func jrs33CompactTimeStatus(value string) string {
	value = jrs33DateTimeRE.ReplaceAllString(value, " ")
	return strings.TrimSpace(cleanJRS33Text(value))
}

func parseJRS33MatchTime(value string) string {
	m := jrs33MinuteRE.FindStringSubmatch(value)
	if len(m) >= 2 {
		return m[1] + "′"
	}
	// Some compact JRS33 rows show the elapsed minute as a bare suffix:
	// "11-14 14:00 65". Only accept a standalone number after kickoff.
	compact := jrs33CompactTimeStatus(value)
	if regexp.MustCompile("^([1-9][0-9]?)$").MatchString(compact) {
		return compact + "′"
	}
	return ""
}

func parseJRS33Status(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(value, "中场"), strings.Contains(value, "半场休息"), strings.Contains(lower, "halftime"), strings.Contains(lower, "half-time"): return "halftime"
	case strings.Contains(value, "完场"), strings.Contains(value, "已结束"), strings.Contains(value, "全场"), strings.Contains(lower, "finished"), strings.Contains(lower, "ended"): return "finished"
	case strings.Contains(value, "延期"), strings.Contains(value, "推迟"), strings.Contains(lower, "postponed"): return "postponed"
	case strings.Contains(value, "取消"), strings.Contains(lower, "cancelled"), strings.Contains(lower, "canceled"): return "cancelled"
	case strings.Contains(value, "未开赛"), strings.Contains(value, "赛前"), strings.Contains(lower, "scheduled"), strings.Contains(lower, "upcoming"), jrs33CompactTimeStatus(value) == "未": return "scheduled"
	case strings.Contains(value, "进行中"), strings.Contains(value, "直播中"), strings.Contains(value, "上半场"), strings.Contains(value, "下半场"), strings.Contains(value, "加时"), strings.Contains(value, "点球"), strings.Contains(lower, "live"), strings.Contains(lower, "playing"), jrs33MinuteRE.MatchString(value), parseJRS33MatchTime(value) != "": return "live"
	case jrs33CompactTimeStatus(value) == "半" || jrs33CompactTimeStatus(value) == "中": return "halftime"
	case jrs33CompactTimeStatus(value) == "完": return "finished"
	default: return "unknown"
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
		if m.MatchTime != "" { item["matchTime"] = m.MatchTime }
		if m.StatusEvidence != "" { item["statusEvidence"] = m.StatusEvidence }
		if m.HalfTimeHomeScore != nil { item["halfTimeHomeScore"] = *m.HalfTimeHomeScore }
		if m.HalfTimeAwayScore != nil { item["halfTimeAwayScore"] = *m.HalfTimeAwayScore }
		if m.CornerHome != nil { item["cornerHome"] = *m.CornerHome }
		if m.CornerAway != nil { item["cornerAway"] = *m.CornerAway }
		out = append(out, item)
	}
	return out
}

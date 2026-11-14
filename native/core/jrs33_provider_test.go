package core

import (
	"context"
	"encoding/base64"
	"io"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	stdhtml "html"

	xhtml "golang.org/x/net/html"
)


func TestJRS33IndexScriptURLTrustPolicy(t *testing.T) {
	accepted, _ := url.Parse("https://im-imgs-bucket.oss-accelerate.aliyuncs.com/index.js?t_5")
	if !isJRS33IndexScriptURL(accepted) {
		t.Fatal("the observed JRS33 OSS index script host should be accepted")
	}
	rejected := []string{
		"http://im-imgs-bucket.oss-accelerate.aliyuncs.com/index.js",
		"https://sub.im-imgs-bucket.oss-accelerate.aliyuncs.com/index.js",
		"https://im-imgs-bucket.oss-accelerate.aliyuncs.com.evil.example/index.js",
		"https://im-imgs-bucket.oss-accelerate.aliyuncs.com:8443/index.js",
		"https://user@im-imgs-bucket.oss-accelerate.aliyuncs.com/index.js",
		"https://aliyuncs.com/index.js",
		"https://other-bucket.oss-accelerate.aliyuncs.com/index.js",
		"https://127.0.0.1/index.js",
	}
	for _, raw := range rejected {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if isJRS33IndexScriptURL(parsed) {
			t.Errorf("untrusted script URL was accepted: %s", raw)
		}
	}
}

func TestParseJRS33Home(t *testing.T) {
	html := `<!doctype html><html><body>
<div class="d-touch">
  <span class="name">英超</span>
  <span class="name">阿森纳</span>
  <span class="name">切尔西</span>
  <span class="lab_time">11-14 20:30 未开赛</span>
  <span class="score">0:0</span>
  <a class="me" href="/match/abc123.html">详情</a>
  <a href="https://play.sportsteam356.com/play/steam826532.html">直播①</a>
</div>
</body></html>`
	matches := parseJRS33Home(html, "https://www.jrkan.com/")
	if len(matches) != 1 { t.Fatalf("got %d matches, want 1", len(matches)) }
	got := matches[0]
	if got.League != "英超" || got.HomeTeam != "阿森纳" || got.AwayTeam != "切尔西" {
		t.Fatalf("unexpected teams: %#v", got)
	}
	if got.ID != "jrs33:match/abc123.html" { t.Fatalf("unexpected id: %q", got.ID) }
	if got.DetailURL != "https://www.jrkan.com/match/abc123.html" { t.Fatalf("unexpected detail URL: %q", got.DetailURL) }
	if got.Status != "scheduled" { t.Fatalf("unexpected status: %q", got.Status) }
	if len(got.Channels) != 1 || got.Channels[0].URL != "https://play.sportsteam356.com/play/steam826532.html" { t.Fatalf("unexpected homepage channels: %#v", got.Channels) }
	if got.HomeScore == nil || got.AwayScore == nil || *got.HomeScore != 0 || *got.AwayScore != 0 {
		t.Fatalf("unexpected score: %#v %#v", got.HomeScore, got.AwayScore)
	}
}

func TestParseJRS33IframeURLs(t *testing.T) {
	source := "<iframe src=\"/embed/live\"></iframe><iframe src=\"http://bad.example/player\"></iframe><iframe src=\"https://127.0.0.1/private\"></iframe>"
	got := parseJRS33IframeURLs(source, "https://play.example.com/watch/1.html")
	if len(got) != 1 || got[0] != "https://play.example.com/embed/live" {
		t.Fatalf("parseJRS33IframeURLs() = %#v", got)
	}
}

func TestParseJRS33MediaURL(t *testing.T) {
	tests := []struct {
		name string
		source string
		base string
		want string
	}{
		{
			name: "video source tag",
			source: "<video><source src=\"https://cdn.example.com/live/index.m3u8?token=abc\"></video>",
			base: "https://play.example.com/watch/1.html",
			want: "https://cdn.example.com/live/index.m3u8?token=abc",
		},
		{
			name: "escaped player config",
			source: "var player = { url: \"https:\\/\\/cdn.example.com\\/live\\/index.m3u8\" };",
			base: "https://play.example.com/watch/1.html",
			want: "https://cdn.example.com/live/index.m3u8",
		},
		{
			name: "relative media path",
			source: "<video src=\"/media/live.mp4\"></video>",
			base: "https://play.example.com/watch/1.html",
			want: "https://play.example.com/media/live.mp4",
		},
		{
			name: "reject insecure stream",
			source: "<video src=\"http://cdn.example.com/live.m3u8\"></video>",
			base: "https://play.example.com/watch/1.html",
			want: "",
		},
		{
			name: "reject page URL",
			source: "<iframe src=\"https://player.example.com/watch.html\"></iframe>",
			base: "https://play.example.com/watch/1.html",
			want: "",
		},
		{
			name: "reject IP literal",
			source: "<video src=\"https://127.0.0.1/live.m3u8\"></video>",
			base: "https://play.example.com/watch/1.html",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseJRS33MediaURL(tt.source, tt.base); got != tt.want {
				t.Fatalf("parseJRS33MediaURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseJRS33Channels(t *testing.T) {
	html := `<div class="sub_playlist">
<a data-play="/play/match_1.html"><strong>线路一</strong></a>
<a data-play="https://m.jw1104.com/play/match_2.html"><strong>线路二</strong></a>
<a data-play="javascript:void(0)"><strong>无效</strong></a>
</div>`
	channels := parseJRS33Channels(html, "https://www.jrkan.com/match/abc.html")
	if len(channels) != 2 { t.Fatalf("got %d channels, want 2", len(channels)) }
	if channels[0].URL != "https://www.jrkan.com/play/match_1.html" { t.Fatalf("unexpected first channel URL: %s", channels[0].URL) }
	if channels[1].URL != "https://m.jw1104.com/play/match_2.html" { t.Fatalf("unexpected second channel URL: %s", channels[1].URL) }
}

func TestJRS33StatusAndScore(t *testing.T) {
	if got := parseJRS33Status("上半场 进行中"); got != "live" { t.Fatalf("status=%q", got) }
	if got := parseJRS33Status("中场休息"); got != "halftime" { t.Fatalf("status=%q", got) }
	if got := parseJRS33Status("完场"); got != "finished" { t.Fatalf("status=%q", got) }
	home, away, ok := parseJRS33Score("2 : 1")
	if !ok || home != 2 || away != 1 { t.Fatalf("score=%d:%d ok=%v", home, away, ok) }
}

func TestResolveJRS33ChannelLink(t *testing.T) {
	if got := resolveJRS33ChannelLink("https://www.jrkan.com/match/a.html", "/play/a.html"); got != "https://www.jrkan.com/play/a.html" {
		t.Fatalf("relative channel URL = %q", got)
	}
	if got := resolveJRS33ChannelLink("https://www.jrkan.com/", "javascript:alert(1)"); got != "" {
		t.Fatalf("javascript URL should be rejected, got %q", got)
	}
	if got := resolveJRS33ChannelLink("https://www.jrkan.com/", "http://example.com/a"); got != "" {
		t.Fatalf("non-HTTPS URL should be rejected, got %q", got)
	}
}

func TestJRS33IndexScriptPatterns(t *testing.T) {
	home := `<script src="/js/index.js?v=2"></script><script>var PLAY_HOSTS={line1:atob("aHR0cHM6Ly9wbGF5LmV4YW1wbGUuY29t")};var playUrl=host+"/play/"+id+".html";</script>`
	if got := jrs33IndexScriptRE.FindStringSubmatch(home); len(got) != 2 || got[1] != "/js/index.js?v=2" {
		t.Fatalf("index script match = %#v", got)
	}
	if got := jrs33PlayHostRE.FindStringSubmatch(`line1: atob("aHR0cHM6Ly9wbGF5LmV4YW1wbGUuY29t")`); len(got) != 3 || got[1] != "line1" {
		t.Fatalf("play host match = %#v", got)
	}
	if got := jrs33PlayPathRE.FindStringSubmatch(`host + "/play/" + id + ".html"`); len(got) != 3 || got[1] != "/play/" || got[2] != ".html" {
		t.Fatalf("play path match = %#v", got)
	}
	script := `document.write('<a href="'+getPlayUrl("line1","match001")+'">直播①</a>');`
	if got := jrs33WriteRE.FindStringSubmatch(script); len(got) < 2 || !strings.Contains(got[1], "getPlayUrl") {
		t.Fatalf("document.write match = %#v", got)
	}
}

func TestParseJRS33Kickoff(t *testing.T) {
	got := parseJRS33Kickoff("11-14 20:30 未开赛")
	if got == "" { t.Fatal("kickoff parser returned empty string") }
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Fatalf("kickoff %q is not RFC3339: %v", got, err)
	}
}


func TestParseJRS33HomeKeepsAllSportsAndWomenLeagues(t *testing.T) {
	html := `<html><body>
<div class="d-touch"><span class="name">NBA</span><span class="name">湖人</span><span class="name">勇士</span><span class="lab_time">11-14 09:30 进行中</span></div>
<div class="d-touch"><span class="name">澳女联</span><span class="name">墨尔本城女足</span><span class="name">悉尼女足</span><span class="lab_time">11-14 16:00 进行中</span></div>
</body></html>`
	matches := parseJRS33Home(html, "https://www.jrkan.com/")
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want NBA and women's league fixtures", len(matches))
	}
	if matches[0].League != "NBA" || matches[1].League != "澳女联" {
		t.Fatalf("unexpected leagues: %q, %q", matches[0].League, matches[1].League)
	}
}

func TestParseJRS33TableLayout(t *testing.T) {
	html := `<html><body><table>
<tr><th>赛事</th><th>时间状态</th><th>主队</th><th>比分</th><th>客队</th><th>频道</th></tr>
<tr><td>澳女联</td><td>11-14 16:00 进行中</td><td>墨尔本城女足</td><td>1:0</td><td>悉尼女足</td><td><a href="https://play.example.com/live/1">直播①</a><a class="me" href="/match/women-1.html">详情</a></td></tr>
<tr><td>NBA</td><td>11-14 09:30 进行中</td><td>湖人</td><td>88:82</td><td>勇士</td><td><a class="me" href="/match/nba-1.html">详情</a></td></tr>
</table></body></html>`
	matches := parseJRS33Home(html, "https://www.jrkan.com/")
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	if matches[0].League != "澳女联" || matches[0].HomeTeam != "墨尔本城女足" || matches[0].AwayTeam != "悉尼女足" {
		t.Fatalf("unexpected women's fixture: %#v", matches[0])
	}
	if matches[0].HomeScore == nil || matches[0].AwayScore == nil || *matches[0].HomeScore != 1 || *matches[0].AwayScore != 0 {
		t.Fatalf("unexpected women's score: %#v %#v", matches[0].HomeScore, matches[0].AwayScore)
	}
	if matches[1].League != "NBA" || matches[1].DetailURL != "https://www.jrkan.com/match/nba-1.html" {
		t.Fatalf("unexpected NBA fixture: %#v", matches[1])
	}
}


func TestJRS33DataStatusDistinguishesEmptyAndParserFailure(t *testing.T) {
	if got := jrs33DataStatus(nil, nil); got != "empty" {
		t.Fatalf("empty status = %q, want empty", got)
	}
	if got := jrs33DataStatus([]jrs33Match{{ID: "1"}}, nil); got != "ok" {
		t.Fatalf("success status = %q, want ok", got)
	}
	if got := jrs33DataStatus(nil, errors.New("JRS33 动态赛事脚本已展开，但未解析出任何赛事行")); got != "script_parse_failed" {
		t.Fatalf("parser status = %q, want script_parse_failed", got)
	}
	if got := jrs33DataStatus(nil, errors.New("JRS33 返回 Cloudflare/反爬验证页")); got != "challenge" {
		t.Fatalf("challenge status = %q, want challenge", got)
	}
}

func TestParseJRS33LiveMatchMetadata(t *testing.T) {
	html := `<div class="d-touch">
<span class="name">澳女联</span><span class="name">中央海岸水手女足</span><span class="name">珀斯光荣女足</span>
<span class="lab_time">11-14 14:00</span><span class="lab_status" data-status="下半场" title="65′">65′</span>
<span class="score">0:0</span><span class="half_score">0:0</span><span class="corner">4:2</span>
<a class="me" href="/match/live-1.html">详情</a>
</div>`
	matches := parseJRS33Home(html, "https://www.jrkan.com/")
	if len(matches) != 1 { t.Fatalf("matches=%d, want 1", len(matches)) }
	got := matches[0]
	if got.Status != "live" || got.MatchTime != "65′" { t.Fatalf("status/time=%q/%q", got.Status, got.MatchTime) }
	if got.HomeScore == nil || got.AwayScore == nil || *got.HomeScore != 0 || *got.AwayScore != 0 { t.Fatalf("score=%v:%v", got.HomeScore, got.AwayScore) }
	if got.HalfTimeHomeScore == nil || got.HalfTimeAwayScore == nil || *got.HalfTimeHomeScore != 0 || *got.HalfTimeAwayScore != 0 { t.Fatalf("half=%v:%v", got.HalfTimeHomeScore, got.HalfTimeAwayScore) }
	if got.CornerHome == nil || got.CornerAway == nil || *got.CornerHome != 4 || *got.CornerAway != 2 { t.Fatalf("corners=%v:%v", got.CornerHome, got.CornerAway) }
}

func TestJRS33UnknownStatusIsNotScheduled(t *testing.T) {
	if got := parseJRS33Status("11-14 14:00"); got != "unknown" { t.Fatalf("unknown time status mapped to %q", got) }
	if got := parseJRS33Status("第 23 分钟"); got != "live" { t.Fatalf("minute status mapped to %q", got) }
	if got := parseJRS33MatchTime("下半场 23′"); got != "23′" { t.Fatalf("match time=%q", got) }
}

func TestJRS33CompactTimeStatus(t *testing.T) {
	cases := []struct{ input, status, minute string }{
		{"11-14 14:00 未", "scheduled", ""},
		{"11-14 14:00 半", "halftime", ""},
		{"11-14 14:00 完", "finished", ""},
		{"11-14 14:00 65", "live", "65′"},
		{"11-14 14:00 65′", "live", "65′"},
	}
	for _, tc := range cases {
		if got := parseJRS33Status(tc.input); got != tc.status {
			t.Errorf("parseJRS33Status(%q)=%q, want %q", tc.input, got, tc.status)
		}
		if got := parseJRS33MatchTime(tc.input); got != tc.minute {
			t.Errorf("parseJRS33MatchTime(%q)=%q, want %q", tc.input, got, tc.minute)
		}
	}
}

func TestJRS33StatusCellIsParsedIndependently(t *testing.T) {
	cases := []struct {
		statusText string
		wantStatus string
		wantMinute string
	}{
		{"未", "scheduled", ""},
		{"半", "halftime", ""},
		{"完", "finished", ""},
		{"65", "live", "65′"},
	}
	for _, tc := range cases {
		html := `<div class="d-touch">
<span class="name">澳女联</span><span class="name">主队</span><span class="name">客队</span>
<span class="lab_time">11-14 14:00</span><span class="status">` + tc.statusText + `</span>
<a class="me" href="/match/status-cell.html">详情</a></div>`
		matches := parseJRS33Home(html, "https://www.jrkan.com/")
		if len(matches) != 1 {
			t.Fatalf("status %q: got %d matches", tc.statusText, len(matches))
		}
		if matches[0].Status != tc.wantStatus || matches[0].MatchTime != tc.wantMinute {
			t.Errorf("status cell %q parsed as %q/%q, want %q/%q",
				tc.statusText, matches[0].Status, matches[0].MatchTime, tc.wantStatus, tc.wantMinute)
		}
	}
}


// TestFetchLiveJRS33SourceSnapshot is a temporary network diagnostic. It is
// removed after one CI run; it does not build an app or call third-party score APIs.
func TestFetchLiveJRS33SourceSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 12 * time.Second}
	provider := &jrs33Provider{
		domains: &jrs33DomainManager{client: client},
		detailChannels: make(map[string][]jrs33LiveChannel),
	}
	bases := []string{
		"https://www.jrkan.com",
		"https://www.jrs945.com",
		"https://www.jrs80.com",
		"https://www.jrs04.com",
		"https://www.jrs03.com",
		"https://m.jrs33.com",
		"https://www.jrskk.com",
		"https://m.jrskk.com",
		"https://m.jrs21.com",
	}
	found := false
	for _, base := range bases {
		if base == "https://www.jrkan.com" {
			home, finalURL, homeErr := provider.getText(ctx, base+"/?lan=0", "")
			if homeErr == nil {
				rawHome := home
				if req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, base+"/?lan=0", nil); reqErr == nil {
					req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile/15E148 Safari/604.1")
					if resp, respErr := client.Do(req); respErr == nil {
						t.Logf("HOME_HEADERS date=%q age=%q cache=%q lastmod=%q", resp.Header.Get("Date"), resp.Header.Get("Age"), resp.Header.Get("CF-Cache-Status"), resp.Header.Get("Last-Modified"))
						resp.Body.Close()
					}
				}
				rawDoc, rawParseErr := xhtml.Parse(strings.NewReader(rawHome))
				if rawParseErr == nil {
					scripts := findJRS33Elements(rawDoc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "script" })
					for i, scriptNode := range scripts {
						if i >= 16 { break }
						src := jrs33Attr(scriptNode, "src")
						if src == "" { continue }
						baseURL, _ := url.Parse(finalURL); refURL, _ := url.Parse(stdhtml.UnescapeString(src))
						if baseURL == nil || refURL == nil { continue }
						scriptURL := baseURL.ResolveReference(refURL)
						t.Logf("PAGE_SCRIPT %s", scriptURL.String())
						if !isJRS33Host(scriptURL.Hostname()) { continue }
						if scriptBody, _, scriptErr := provider.getText(ctx, scriptURL.String(), finalURL); scriptErr == nil {
							lower := strings.ToLower(scriptBody)
							if strings.Contains(lower, "lab_vs") || strings.Contains(lower, "data-lid") || strings.Contains(lower, "lab_bc") || strings.Contains(lower, "lab_jq") || strings.Contains(lower, "sub_status") || strings.Contains(lower, "getjson") || strings.Contains(lower, "ajax") || strings.Contains(lower, "setinterval") {
								t.Logf("PAGE_SCRIPT_MATCH url=%q bytes=%d", scriptURL.String(), len(scriptBody))
								for _, token := range []string{"lab_vs", "data-lid", "lab_bc", "lab_jq", "sub_status", "getJSON", "ajax", "setInterval"} {
									pos := strings.Index(lower, strings.ToLower(token))
									if pos >= 0 { start, finish := pos-250, pos+1000; if start < 0 { start = 0 }; if finish > len(scriptBody) { finish = len(scriptBody) }; t.Logf("PAGE_SCRIPT_%s %s", token, scriptBody[start:finish]) }
								}
							}
						}
					}
				}
				expanded, expandErr := provider.expandIndexScript(ctx, home, finalURL)
				if expandErr == nil { home = expanded }
				doc, parseErr := xhtml.Parse(strings.NewReader(home))
				if parseErr == nil {
					rows := findJRS33Elements(doc, func(n *xhtml.Node) bool { return hasJRS33Class(n, "d-touch") })
					t.Logf("RAW_HOME final=%q bytes=%d rows=%d expanded=%v", finalURL, len(home), len(rows), expandErr == nil)
					vsClasses := make(map[string]int)
					for _, row := range rows {
						vs := findJRS33Elements(row, func(n *xhtml.Node) bool { return hasJRS33Class(n, "lab_vs") })
						if len(vs) > 0 { vsClasses[jrs33Attr(vs[0], "class")]++ }
					}
					t.Logf("VS_CLASSES %#v", vsClasses)
					for i, row := range rows {
						if i >= 12 { break }
						var markup strings.Builder
						_ = xhtml.Render(&markup, row)
						t.Logf("RAW_ROW[%d] %s", i, markup.String())
					}
					for _, hostMatch := range jrs33PlayHostRE.FindAllStringSubmatch(rawHome, -1) {
						if len(hostMatch) > 2 {
							decoded, decodeErr := base64.StdEncoding.DecodeString(hostMatch[2])
							t.Logf("PLAY_HOST key=%q decoded=%q err=%v", hostMatch[1], strings.TrimSpace(string(decoded)), decodeErr)
						}
					}
					t.Logf("PLAY_PATH pattern=%q", jrs33PlayPathRE.FindStringSubmatch(rawHome))
					for _, token := range []string{"function getPlayUrl", "getPlayUrl =", "function openRandomUrl1", "function getPlay"} {
						pos := strings.Index(rawHome, token)
						if pos >= 0 {
							start, finish := pos-300, pos+1800
							if start < 0 { start = 0 }
							if finish > len(rawHome) { finish = len(rawHome) }
							t.Logf("HOME_%s %s", token, rawHome[start:finish])
						}
					}
					for _, scriptNode := range findJRS33Elements(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "script" }) {
						src := jrs33Attr(scriptNode, "src")
						if src == "" { continue }
						baseURL, _ := url.Parse(finalURL)
						refURL, _ := url.Parse(stdhtml.UnescapeString(src))
						if baseURL == nil || refURL == nil { continue }
						scriptURL := baseURL.ResolveReference(refURL)
						t.Logf("PAGE_SCRIPT %s", scriptURL.String())
						if !isJRS33Host(scriptURL.Hostname()) { continue }
						if scriptBody, _, scriptErr := provider.getText(ctx, scriptURL.String(), finalURL); scriptErr == nil {
							lower := strings.ToLower(scriptBody)
							if strings.Contains(lower, "lab_vs") || strings.Contains(lower, "data-lid") || strings.Contains(lower, "lab_bc") || strings.Contains(lower, "lab_jq") || strings.Contains(lower, "sub_status") || strings.Contains(lower, "getjson") || strings.Contains(lower, "ajax") {
								t.Logf("PAGE_SCRIPT_MATCH url=%q bytes=%d", scriptURL.String(), len(scriptBody))
								for _, token := range []string{"lab_vs", "data-lid", "lab_bc", "lab_jq", "sub_status", "getJSON", "ajax"} {
									pos := strings.Index(lower, strings.ToLower(token))
									if pos >= 0 {
										start, finish := pos-250, pos+1000
										if start < 0 { start = 0 }
										if finish > len(scriptBody) { finish = len(scriptBody) }
										t.Logf("PAGE_SCRIPT_%s %s", token, scriptBody[start:finish])
									}
								}
							}
						}
					}
					for _, host := range []string{"play.sportsteam368.com", "play.jgdhds.com", "play.sportsteam7777.com"} {
						for _, scheme := range []string{"https", "http"} {
							probeURL := scheme + "://" + host + "/play/steam826545.html"
							probeClient := &http.Client{Timeout: 4 * time.Second}
							probeClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
								if len(via) > 3 || !strings.EqualFold(req.URL.Hostname(), host) { return errors.New("cross-host or excessive redirect") }
								return nil
							}
							resp, probeErr := probeClient.Get(probeURL)
							if probeErr != nil {
								t.Logf("PLAYER_PROBE %s ERROR %v", probeURL, probeErr)
								continue
							}
							body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
							resp.Body.Close()
							t.Logf("PLAYER_PROBE %s status=%d final=%q type=%q bytes=%d media=%q iframes=%q body=%q", probeURL, resp.StatusCode, resp.Request.URL.String(), resp.Header.Get("Content-Type"), len(body), parseJRS33MediaURL(string(body), resp.Request.URL.String()), parseJRS33IframeURLs(string(body), resp.Request.URL.String()), string(body[:min(len(body), 1200)]))
						}
					}
					if scriptRef := jrs33IndexScriptRE.FindStringSubmatch(rawHome); len(scriptRef) > 1 {
						baseURL, _ := url.Parse(finalURL)
						refURL, _ := url.Parse(stdhtml.UnescapeString(scriptRef[1]))
						if baseURL != nil && refURL != nil {
							scriptURL := baseURL.ResolveReference(refURL)
							if script, _, scriptErr := provider.getTextFromIndexScript(ctx, scriptURL.String(), finalURL); scriptErr == nil {
								t.Logf("INDEX_SCRIPT url=%q bytes=%d", scriptURL.String(), len(script))
								for _, token := range []string{"getPlayUrl", "document.write", "providerid", "urlid", "data-play"} {
									pos := strings.Index(script, token)
									if pos >= 0 {
										start, finish := pos-500, pos+1800
										if start < 0 { start = 0 }
										if finish > len(script) { finish = len(script) }
										t.Logf("SCRIPT_%s %s", token, script[start:finish])
									}
								}
							}
						}
					}
				}
			} else {
				t.Logf("RAW_HOME_ERROR %v", homeErr)
			}
		}
		matches, err := provider.fetchMatches(ctx, base)
		if err != nil {
			t.Logf("SOURCE %s ERROR %v", base, err)
			continue
		}
		t.Logf("SOURCE %s MATCH_COUNT %d", base, len(matches))
		if len(matches) == 0 { continue }
		found = true
		provider.cached = matches
		provider.activeBaseURL = base
		for i, match := range matches {
			if i >= 8 { break }
			t.Logf("MATCH[%d] id=%q league=%q home=%q score=%v:%v away=%q status=%q minute=%q evidence=%q detail=%q",
				i, match.ID, match.League, match.HomeTeam, match.HomeScore, match.AwayScore,
				match.AwayTeam, match.Status, match.MatchTime, match.StatusEvidence, match.DetailURL)
		}
		for _, match := range matches {
			if match.DetailURL == "" { continue }
			channels, channelErr := provider.matchChannels(ctx, match.ID)
			if channelErr != nil {
				t.Logf("CHANNELS match=%q ERROR %v", match.ID, channelErr)
				continue
			}
			t.Logf("CHANNELS match=%q count=%d", match.ID, len(channels))
			for i, channel := range channels {
				if i >= 4 { break }
				t.Logf("CHANNEL[%d] name=%q url=%q", i, channel.Name, channel.URL)
				mediaURL, headers, resolveErr := provider.resolveChannel(ctx, channel.URL)
				if resolveErr != nil {
					t.Logf("RESOLVE[%d] ERROR %v", i, resolveErr)
				} else {
					t.Logf("RESOLVE[%d] media=%q headers=%v", i, mediaURL, headers)
				}
			}
			break
		}
		break
	}
	if !found { t.Log("NO_LIVE_JRS33_SOURCE_REACHED") }
}

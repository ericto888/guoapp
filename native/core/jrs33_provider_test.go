package core

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
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

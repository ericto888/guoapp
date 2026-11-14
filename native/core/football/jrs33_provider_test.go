package core

import "testing"

func TestParseJRS33Home(t *testing.T) {
	html := `<!doctype html><html><body>
<div class="d-touch">
  <span class="name">英超</span>
  <span class="name">阿森纳</span>
  <span class="name">切尔西</span>
  <span class="lab_time">11-14 20:30 未开赛</span>
  <span class="score">0:0</span>
  <a class="me" href="/match/abc123.html">详情</a>
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

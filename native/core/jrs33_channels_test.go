package core

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

type jrs33TestRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip jrs33TestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestJRS33EmptyChannelResultCanBeRetried(t *testing.T) {
	requests := 0
	client := &http.Client{
		Transport: jrs33TestRoundTripper(func(request *http.Request) (*http.Response, error) {
			requests++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`<div class="sub_playlist"></div>`)),
				Request:    request,
			}, nil
		}),
	}
	provider := &jrs33Provider{
		domains: &jrs33DomainManager{client: client},
		cached: []jrs33Match{{
			ID:        "jrs33:match/empty.html",
			DetailURL: "https://www.jrkan.com/match/empty.html",
		}},
	}

	for attempt := 0; attempt < 2; attempt++ {
		channels, err := provider.matchChannels(context.Background(), "jrs33:match/empty.html")
		if err != nil {
			t.Fatalf("attempt %d: matchChannels returned error: %v", attempt+1, err)
		}
		if len(channels) != 0 {
			t.Fatalf("attempt %d: got %d channels, want empty result", attempt+1, len(channels))
		}
	}
	if requests != 2 {
		t.Fatalf("detail page requests = %d, want 2 so empty results can be retried", requests)
	}
}

func TestParseJRS33PlayerMediaAndIframeURLs(t *testing.T) {
	page := `<html><body>
<video><source src="../live/index.m3u8?token=abc&amp;expires=123" type="application/x-mpegURL"></video>
<script>var player = new Player({url: "https://cdn.example.net/backup/stream.m3u8?auth=xyz"});</script>
</body></html>`
	got := parseJRS33MediaURL(page, "https://play.example.net/watch/channel.html")
	if got != "https://play.example.net/live/index.m3u8?token=abc&expires=123" {
		t.Fatalf("media URL = %q, want first same-page HTTPS HLS resource", got)
	}
	if got := parseJRS33MediaURL(`<script>var url="http://cdn.example.net/live.m3u8";</script>`, "https://play.example.net/watch.html"); got != "" {
		t.Fatalf("insecure HTTP media URL must not be returned: %q", got)
	}
}

func TestParseJRS33PlayerIframeURLs(t *testing.T) {
	html := `<iframe src="/embed/live.html"></iframe>
<iframe src="http://insecure.example/live.html"></iframe>
<iframe src="https://embed.example.net/player.html"></iframe>`
	got := parseJRS33IframeURLs(html, "https://play.example.net/watch/index.html")
	if len(got) != 2 {
		t.Fatalf("iframe URLs=%v, want two HTTPS iframe URLs", got)
	}
	if got[0] != "https://play.example.net/embed/live.html" ||
		got[1] != "https://embed.example.net/player.html" {
		t.Fatalf("unexpected iframe URLs: %v", got)
	}
}

func TestJRS33ResolveChannelRejectsNonHTTPSEntry(t *testing.T) {
	provider := &jrs33Provider{}
	if _, _, err := provider.resolveChannel(context.Background(), "http://play.example.net/live.html"); err == nil {
		t.Fatal("non-HTTPS channel entry should be rejected")
	}
	if _, _, err := provider.resolveChannel(context.Background(), "https://127.0.0.1/live.m3u8"); err == nil {
		t.Fatal("IP-literal channel entry should be rejected")
	}
}

func TestParseJRS33MediaURLFromBase64PlayerConfig(t *testing.T) {
	config := `var player = {url: "https://play.example.net/live/index.m3u8?token=abc"};`
	encoded := base64.StdEncoding.EncodeToString([]byte(config))
	page := `<script>eval(atob("` + encoded + `"));</script>`
	got := parseJRS33MediaURL(page, "https://play.example.net/watch.html")
	if got != "https://play.example.net/live/index.m3u8?token=abc" {
		t.Fatalf("decoded media URL = %q", got)
	}
}

func TestParseJRS33HomeDoesNotTreatBFTitleAsScore(t *testing.T) {
	// This mirrors the current JRKAN row shape: title="2" appears on both
	// .bf nodes, but neither node contains a visible score.
	page := `<ul>
<li class="d-touch">
  <span class="name">U17亚洲杯</span>
  <div class="lab_time">11-14 16:00</div>
  <span class="name lab_team_home">伊拉克U17<em class="bf" title="2"></em></span>
  <span class="name lab_team_away">中国台北U17<em class="bf" title="2"></em></span>
  <div class="sub_status"><em class="spin_ant"></em></div>
  <a class="item ok" href="https://play.example.net/live/1">直播①</a>
</li>
</ul>`
	matches := parseJRS33Home(page, "https://www.jrkan.com/")
	if len(matches) != 1 {
		t.Fatalf("parsed matches = %d, want 1", len(matches))
	}
	match := matches[0]
	if match.HomeScore != nil || match.AwayScore != nil {
		t.Fatalf("placeholder title was parsed as score %v:%v; want unknown score", match.HomeScore, match.AwayScore)
	}
	if match.MatchTime != "" {
		t.Fatalf("kickoff time was incorrectly parsed as elapsed match time: %q", match.MatchTime)
	}
	if match.Status != "unknown" {
		t.Fatalf("empty spinner/status markup must remain unknown, got %q", match.Status)
	}
}

func TestParseJRS33CurrentMinuteFromStatusNode(t *testing.T) {
	page := `<ul>
<li class="d-touch">
  <span class="name">英超</span>
  <div class="lab_time">11-14 16:00</div>
  <span class="name lab_team_home">主队<em class="bf">1</em></span>
  <span class="name lab_team_away">客队<em class="bf">0</em></span>
  <div class="sub_status">65'<em class="spin_ant"></em></div>
</li>
</ul>`
	matches := parseJRS33Home(page, "https://www.jrkan.com/")
	if len(matches) != 1 {
		t.Fatalf("parsed matches = %d, want 1", len(matches))
	}
	if matches[0].HomeScore == nil || *matches[0].HomeScore != 1 ||
		matches[0].AwayScore == nil || *matches[0].AwayScore != 0 {
		t.Fatalf("visible scores not parsed: %v:%v", matches[0].HomeScore, matches[0].AwayScore)
	}
	if matches[0].Status != "live" || matches[0].MatchTime != "65′" {
		t.Fatalf("status/time = %q/%q, want live/65′", matches[0].Status, matches[0].MatchTime)
	}
}

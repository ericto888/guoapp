package core

import (
	"context"
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

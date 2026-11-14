package core

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type jrs33FallbackRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip jrs33FallbackRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestJRS33FetchMatchesFallsBackWhenPrimaryIsEmpty(t *testing.T) {
	primaryRequests := 0
	backupRequests := 0
	client := &http.Client{
		Transport: jrs33FallbackRoundTripper(func(request *http.Request) (*http.Response, error) {
			body := `<html><body>JRSKAN直播 暂无赛事</body></html>`
			switch request.URL.Hostname() {
			case "www.jrkan.com":
				primaryRequests++
			case "www.jrs13.com":
				backupRequests++
				body = `<!doctype html><html><body><div class="d-touch">
<span class="name">英超</span><span class="name">阿森纳</span><span class="name">切尔西</span>
<span class="lab_time">11-14 20:30 未开赛</span><a class="me" href="/match/abc123.html">详情</a>
</div></body></html>`
			default:
				return nil, http.ErrNoLocation
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    request,
			}, nil
		}),
	}
	provider := &jrs33Provider{domains: &jrs33DomainManager{client: client}}
	matches, activeDomain, warning, err := provider.fetchMatchesWithFallback(
		context.Background(),
		"https://www.jrkan.com",
		[]string{"https://www.jrs13.com"},
	)
	if err != nil {
		t.Fatalf("fetchMatchesWithFallback returned error: %v", err)
	}
	if len(matches) != 1 || matches[0].HomeTeam != "阿森纳" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
	if activeDomain != "https://www.jrs13.com" {
		t.Fatalf("active domain = %q, want backup domain", activeDomain)
	}
	if warning == "" {
		t.Fatal("expected a fallback warning")
	}
	if primaryRequests != 1 || backupRequests != 1 {
		t.Fatalf("requests primary=%d backup=%d, want 1 each", primaryRequests, backupRequests)
	}
}

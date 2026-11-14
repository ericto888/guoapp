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

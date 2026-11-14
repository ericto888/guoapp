package core

import (
	"reflect"
	"testing"
)

func TestNormalizeJRS33URL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"m.jrs33.com", "https://m.jrs33.com"},
		{"https://www.jrs13.com/", "https://www.jrs13.com"},
		{"//www.jrkan.com", "https://www.jrkan.com"},
		{"https://m.jrskk.com", "https://m.jrskk.com"},
		{"https://www.jrs945.com", "https://www.jrs945.com"},
		{"https://www.jrs80.com", "https://www.jrs80.com"},
		{"https://www.jrs04.com", "https://www.jrs04.com"},
		{"http://m.jrs33.com", ""},
		{"https://jrs33.com:8443", ""},
		{"https://evil.example", ""},
		{"https://127.0.0.1", ""},
	}
	for _, test := range tests {
		if got := normalizeJRS33URL(test.input); got != test.want {
			t.Errorf("normalizeJRS33URL(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestExtractJRS33Domains(t *testing.T) {
	page := `<footer>备用域名 www.jrs945.com www.jrs80.com www.jrs04.com www.jrs03.com m.jrs33.com</footer>`
	got := extractJRS33Domains(page)
	want := []string{"https://www.jrs945.com", "https://www.jrs80.com", "https://www.jrs04.com", "https://www.jrs03.com", "https://m.jrs33.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractJRS33Domains() = %#v, want %#v", got, want)
	}
}

func TestUniqueJRS33CandidatesSortsAndDeduplicates(t *testing.T) {
	got := uniqueJRS33Candidates([]jrs33DomainCandidate{
		{URL: "https://m.jrs33.com/", Priority: 10},
		{URL: "m.jrs33.com", Priority: 50},
		{URL: "https://www.jrs03.com", Priority: 20},
		{URL: "https://not-jrs.example", Priority: 100},
	})
	want := []jrs33DomainCandidate{
		{URL: "https://m.jrs33.com", Priority: 50},
		{URL: "https://www.jrs03.com", Priority: 20},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueJRS33Candidates() = %#v, want %#v", got, want)
	}
}

func TestIsJRS33ChallengePage(t *testing.T) {
	challengePages := []string{
		`<html><title>Just a moment...</title><script src="/cdn-cgi/challenge-platform/scripts/jsd/main.js"></script></html>`,
		`<div id="cf-chl-widget">Checking your browser before accessing</div>`,
		`<html><body>Enable JavaScript and cookies to continue</body></html>`,
	}
	for _, page := range challengePages {
		if !isJRS33ChallengePage(page) {
			t.Errorf("expected challenge page to be rejected: %q", page)
		}
	}
	ordinaryPages := []string{
		`<html><body>JRSKAN直播 暂无赛事</body></html>`,
		`<html><body>英超 阿森纳 - 切尔西</body></html>`,
	}
	for _, page := range ordinaryPages {
		if isJRS33ChallengePage(page) {
			t.Errorf("ordinary JRS33 page was incorrectly rejected: %q", page)
		}
	}
}

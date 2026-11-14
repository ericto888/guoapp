package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJRS33MatchCacheRestoresActiveMirror(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matches.json")
	cachedAt := time.Now()
	body, err := json.Marshal(jrs33MatchCache{
		Version: 1,
		CachedAt: cachedAt,
		BaseURL: "https://www.jrs13.com",
		Items: []jrs33Match{{ID: "jrs33:match/a.html", HomeTeam: "主队", AwayTeam: "客队", Status: "scheduled"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	provider := &jrs33Provider{cachePath: path}
	provider.mu.Lock()
	provider.loadCacheLocked()
	provider.mu.Unlock()
	if got := provider.currentBaseURL(); got != "https://www.jrs13.com" {
		t.Fatalf("restored active mirror = %q, want backup domain", got)
	}
}

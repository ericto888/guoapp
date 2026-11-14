package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	jrs33RemoteConfigURL = "https://raw.githubusercontent.com/ericto888/guoapp/main/config/jrs33-domains.json"
	jrs33DomainCacheTTL  = 15 * time.Minute
	jrs33ProbeTimeout    = 5 * time.Second
	jrs33ConfigTimeout   = 3 * time.Second
	jrs33MaxPageBytes    = 2 << 20
)

var (
	jrs33DomainPattern = regexp.MustCompile(`(?i)(m\.|www\.)?(jrs[a-z0-9-]+|jrskk|jrkan)\.com`)
	jrs33DomainMarkers = []string{"JRSKAN", "JRKAN直播", "JRS直播"}
)

type jrs33DomainCandidate struct {
	URL      string `json:"url"`
	Priority int    `json:"priority,omitempty"`
}

type jrs33DomainConfig struct {
	Version int                    `json:"version"`
	Domains []jrs33DomainCandidate `json:"domains"`
}

type jrs33DomainCache struct {
	Version     int       `json:"version"`
	BaseURL     string    `json:"baseUrl"`
	VerifiedAt  time.Time `json:"verifiedAt"`
	LastChecked time.Time `json:"lastChecked"`
	LastProbeFailed bool `json:"lastProbeFailed,omitempty"`
	Discovered  []string  `json:"discovered,omitempty"`
}

type jrs33ResolvedDomain struct {
	BaseURL    string
	Stale      bool
	VerifiedAt time.Time
	Discovered []string
}

type jrs33DomainManager struct {
	client    *http.Client
	cachePath string
	mu        sync.Mutex
	cache     jrs33DomainCache
	loaded    bool
}

func newJRS33DomainManager(client *http.Client, dataDir string) *jrs33DomainManager {
	return &jrs33DomainManager{
		client: client,
		cachePath: filepath.Join(dataDir, "football", "jrs33-domain.json"),
	}
}

func (manager *jrs33DomainManager) loadCacheLocked() {
	if manager.loaded {
		return
	}
	manager.loaded = true
	body, err := os.ReadFile(manager.cachePath)
	if err != nil || len(body) > 64<<10 {
		return
	}
	var cache jrs33DomainCache
	if json.Unmarshal(body, &cache) == nil && cache.Version == 1 && validJRS33URL(cache.BaseURL) {
		manager.cache = cache
	}
}

func validJRS33URL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Port() != "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if net.ParseIP(host) != nil || !isJRS33Host(host) {
		return false
	}
	return parsed.Path == "" || parsed.Path == "/"
}

func isJRS33Host(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "jrkan.com" || strings.HasSuffix(host, ".jrkan.com") {
		return true
	}
	if host == "jrskk.com" || strings.HasSuffix(host, ".jrskk.com") {
		return true
	}
	return regexp.MustCompile(`^(m\.|www\.)?jrs[a-z0-9-]+\.com$`).MatchString(host)
}

func normalizeJRS33URL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "" {
		parsed, err = url.Parse("https://" + raw)
		if err != nil {
			return ""
		}
	}
	host := strings.ToLower(parsed.Hostname())
	if !isJRS33Host(host) || parsed.User != nil || parsed.Port() != "" {
		return ""
	}
	return "https://" + host
}

func (manager *jrs33DomainManager) resolve(ctx context.Context) (jrs33ResolvedDomain, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.loadCacheLocked()

	now := time.Now()
	if validJRS33URL(manager.cache.BaseURL) && now.Sub(manager.cache.LastChecked) >= 0 && now.Sub(manager.cache.LastChecked) < jrs33DomainCacheTTL {
		return jrs33ResolvedDomain{BaseURL: manager.cache.BaseURL, Stale: manager.cache.LastProbeFailed, VerifiedAt: manager.cache.VerifiedAt, Discovered: append([]string(nil), manager.cache.Discovered...)}, nil
	}

	candidates := []jrs33DomainCandidate{
		{URL: "https://www.jrkan.com", Priority: 110},
		{URL: "https://www.jrs945.com", Priority: 105},
		{URL: "https://www.jrs80.com", Priority: 100},
		{URL: "https://www.jrs04.com", Priority: 95},
		{URL: "https://www.jrs03.com", Priority: 90},
		{URL: "https://m.jrs33.com", Priority: 70},
		{URL: "https://www.jrskk.com", Priority: 60},
		{URL: "https://m.jrskk.com", Priority: 50},
		{URL: "https://m.jrs21.com", Priority: 40},
	}
	for _, discovered := range manager.cache.Discovered {
		candidates = append(candidates, jrs33DomainCandidate{URL: discovered, Priority: 95})
	}
	if config, err := manager.fetchRemoteConfig(ctx); err == nil {
		candidates = append(candidates, config.Domains...)
	}
	if validJRS33URL(manager.cache.BaseURL) {
		candidates = append(candidates, jrs33DomainCandidate{URL: manager.cache.BaseURL, Priority: 110})
	}
	candidates = uniqueJRS33Candidates(candidates)

	type probeResult struct {
		candidate jrs33DomainCandidate
		base string
		links []string
		err error
	}
	results := make(chan probeResult, len(candidates))
	for _, candidate := range candidates {
		candidate := candidate
		go func() {
			base := normalizeJRS33URL(candidate.URL)
			if base == "" {
				results <- probeResult{candidate: candidate, err: errors.New("域名格式无效")}
				return
			}
			links, err := manager.probe(ctx, base)
			results <- probeResult{candidate: candidate, base: base, links: links, err: err}
		}()
	}

	var best *probeResult
	var lastErr error
	for range candidates {
		select {
		case <-ctx.Done():
			lastErr = ctx.Err()
			break
		case result := <-results:
			if result.err != nil {
				lastErr = result.err
				continue
			}
			if best == nil || result.candidate.Priority > best.candidate.Priority {
				copy := result
				best = &copy
			}
		}
		if ctx.Err() != nil { break }
	}
	if best != nil {
		discovered := uniqueJRS33Strings(best.links)
		manager.cache = jrs33DomainCache{
			Version: 1, BaseURL: best.base, VerifiedAt: time.Now(), LastChecked: time.Now(),
			LastProbeFailed: false, Discovered: append([]string(nil), discovered...),
		}
		_ = writeNativeCacheFile(manager.cachePath, marshalJRS33DomainCache(manager.cache))
		return jrs33ResolvedDomain{BaseURL: best.base, VerifiedAt: manager.cache.VerifiedAt, Discovered: discovered}, nil
	}

	if validJRS33URL(manager.cache.BaseURL) {
		manager.cache.LastChecked = now
		manager.cache.LastProbeFailed = true
		_ = writeNativeCacheFile(manager.cachePath, marshalJRS33DomainCache(manager.cache))
		return jrs33ResolvedDomain{
			BaseURL: manager.cache.BaseURL, Stale: true, VerifiedAt: manager.cache.VerifiedAt,
			Discovered: append([]string(nil), manager.cache.Discovered...),
		}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("没有发现可验证的 JRS33 域名")
	}
	return jrs33ResolvedDomain{}, fmt.Errorf("JRS33 域名发现失败: %w", lastErr)
}

func (manager *jrs33DomainManager) fetchRemoteConfig(ctx context.Context) (jrs33DomainConfig, error) {
	var config jrs33DomainConfig
	client := manager.client
	if client == nil {
		client = &http.Client{Timeout: jrs33ProbeTimeout}
	}
	requestCtx, cancel := context.WithTimeout(ctx, jrs33ConfigTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, jrs33RemoteConfigURL, nil)
	if err != nil {
		return config, err
	}
	req.Header.Set("User-Agent", "GuoApp/FootballDomainDiscovery")
	response, err := client.Do(req)
	if err != nil {
		return config, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Request == nil || response.Request.URL.Hostname() != "raw.githubusercontent.com" {
		return config, errors.New("远程域名配置不可用")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil || len(body) >= 64<<10 {
		return config, errors.New("远程域名配置读取失败")
	}
	if err := json.Unmarshal(body, &config); err != nil || config.Version != 1 || len(config.Domains) == 0 {
		return config, errors.New("远程域名配置格式无效")
	}
	return config, nil
}

func (manager *jrs33DomainManager) probe(ctx context.Context, base string) ([]string, error) {
	client := manager.client
	if client == nil {
		client = &http.Client{Timeout: jrs33ProbeTimeout}
	}
	requestCtx, cancel := context.WithTimeout(ctx, jrs33ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, base+"/", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 || response.Request == nil {
		return nil, fmt.Errorf("域名探测返回 HTTP %d", response.StatusCode)
	}
	finalURL := response.Request.URL
	if finalURL == nil || finalURL.Scheme != "https" || !isJRS33Host(finalURL.Hostname()) {
		return nil, errors.New("域名跳转目标不属于已识别的 JRS33 域名")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, jrs33MaxPageBytes+1))
	if err != nil || len(body) > jrs33MaxPageBytes {
		return nil, errors.New("JRS33 首页读取失败或超出大小限制")
	}
	page := strings.ToUpper(string(body))
	matched := false
	for _, marker := range jrs33DomainMarkers {
		if strings.Contains(page, strings.ToUpper(marker)) {
			matched = true
			break
		}
	}
	if !matched {
		return nil, errors.New("页面未通过 JRS33 特征校验（可能是验证页或域名已失效）")
	}
	return extractJRS33Domains(string(body)), nil
}

func extractJRS33Domains(page string) []string {
	matches := jrs33DomainPattern.FindAllString(page, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		if normalized := normalizeJRS33URL(match); normalized != "" {
			result = append(result, normalized)
		}
	}
	return uniqueJRS33Strings(result)
}

func uniqueJRS33Candidates(items []jrs33DomainCandidate) []jrs33DomainCandidate {
	best := make(map[string]int)
	for _, item := range items {
		base := normalizeJRS33URL(item.URL)
		if base == "" {
			continue
		}
		if old, ok := best[base]; !ok || item.Priority > old {
			best[base] = item.Priority
		}
	}
	result := make([]jrs33DomainCandidate, 0, len(best))
	for base, priority := range best {
		result = append(result, jrs33DomainCandidate{URL: base, Priority: priority})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Priority > result[j].Priority })
	return result
}

func uniqueJRS33Strings(items []string) []string {
	seen := make(map[string]bool, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func marshalJRS33DomainCache(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}

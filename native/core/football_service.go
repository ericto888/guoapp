package core

import (
	"context"
	"errors"
	"strings"
	"time"
)

func jrs33DataStatus(matches []jrs33Match, err error) string {
	if err == nil {
		if len(matches) == 0 {
			return "empty"
		}
		return "ok"
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "cloudflare") || strings.Contains(message, "反爬验证") {
		return "challenge"
	}
	if strings.Contains(message, "动态赛事") || strings.Contains(message, "赛事脚本") {
		return "script_parse_failed"
	}
	return "fetch_failed"
}

// footballMatches returns JRS33 fixtures and their public match-page channel
// entry links. It deliberately does not extract or proxy the underlying media.
func (engine *nativeEngine) footballMatches(ctx context.Context, force bool) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if engine.jrs33Provider == nil {
		return map[string]any{
			"items": []any{}, "configured": false, "dataReady": false,
			"source": "jrs33", "dataStatus": "unconfigured", "warning": "JRS33 数据服务尚未初始化",
		}, nil
	}
	matches, warning, err := engine.jrs33Provider.matches(ctx, force)
	if err != nil {
		return map[string]any{
			"items": []any{}, "configured": false, "dataReady": false,
			"source": "jrs33", "dataStatus": jrs33DataStatus(nil, err), "warning": err.Error(),
		}, nil
	}
	resolved, domainErr := engine.jrs33Domains.resolve(ctx)
	result := map[string]any{
		"items": marshalJRS33Matches(matches), "configured": true, "dataReady": true,
		"source": "jrs33", "dataStatus": jrs33DataStatus(matches, nil), "updatedAt": time.Now(),
	}
	if domainErr == nil {
		activeBaseURL := engine.jrs33Provider.currentBaseURL()
		if activeBaseURL == "" {
			activeBaseURL = resolved.BaseURL
		}
		result["baseUrl"] = activeBaseURL
		result["domainStale"] = resolved.Stale
		result["verifiedAt"] = resolved.VerifiedAt
	}
	if warning != "" {
		result["warning"] = warning
	}
	return result, nil
}

// footballMatchChannels resolves channels on demand for one selected fixture.
func (engine *nativeEngine) footballMatchChannels(ctx context.Context, matchID string) (any, error) {
	if err := ctx.Err(); err != nil { return nil, err }
	if engine.jrs33Provider == nil { return map[string]any{"items": []any{}, "warning": "JRS33 数据服务尚未初始化"}, nil }
	channels, err := engine.jrs33Provider.matchChannels(ctx, matchID)
	if err != nil { return nil, err }
	return map[string]any{"items": channels}, nil
}

// footballResolveChannel resolves a selected JRS33 channel page to a direct media URL.
func (engine *nativeEngine) footballResolveChannel(ctx context.Context, channelURL string) (any, error) {
	if err := ctx.Err(); err != nil { return nil, err }
	if engine.jrs33Provider == nil { return nil, errors.New("JRS33 数据服务尚未初始化") }
	mediaURL, headers, err := engine.jrs33Provider.resolveChannel(ctx, channelURL)
	if err != nil { return nil, err }
	return map[string]any{"url": mediaURL, "headers": headers}, nil
}

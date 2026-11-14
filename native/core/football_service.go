package core

import (
	"context"
	"time"
)

// footballMatches returns JRS33 fixtures and their public match-page channel
// entry links. It deliberately does not extract or proxy the underlying media.
func (engine *nativeEngine) footballMatches(ctx context.Context, force bool) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if engine.jrs33Provider == nil {
		return map[string]any{
			"items": []any{}, "configured": false, "dataReady": false,
			"source": "jrs33", "warning": "JRS33 数据服务尚未初始化",
		}, nil
	}
	matches, warning, err := engine.jrs33Provider.matches(ctx, force)
	if err != nil {
		return map[string]any{
			"items": []any{}, "configured": false, "dataReady": false,
			"source": "jrs33", "warning": err.Error(),
		}, nil
	}
	resolved, domainErr := engine.jrs33Domains.resolve(ctx)
	result := map[string]any{
		"items": marshalJRS33Matches(matches), "configured": true, "dataReady": true,
		"source": "jrs33", "updatedAt": time.Now(),
	}
	if domainErr == nil {
		result["baseUrl"] = resolved.BaseURL
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

package core

import "context"

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
		"source": "jrs33", "updatedAt": engine.jrs33Provider.cachedAt,
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

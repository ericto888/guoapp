package core

import "context"

// footballMatches exposes the independent football data contract and resolves
// the current JRS33 entry point. Match parsing is intentionally kept separate
// from domain discovery so a future JRS33 page/API change cannot poison the
// domain failover state.
func (engine *nativeEngine) footballMatches(ctx context.Context) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if engine.jrs33Domains == nil {
		return map[string]any{
			"items":       []any{},
			"configured":  false,
			"dataReady":   false,
			"source":      "jrs33",
			"warning":     "JRS33 域名管理器尚未初始化",
		}, nil
	}
	resolved, err := engine.jrs33Domains.resolve(ctx)
	if err != nil {
		// Keep the football page usable and allow cached content to be shown
		// by Flutter, rather than turning an upstream outage into a hard error.
		return map[string]any{
			"items":      []any{},
			"configured": false,
			"dataReady":  false,
			"source":     "jrs33",
			"warning":    err.Error(),
		}, nil
	}
	return map[string]any{
		"items":       []any{},
		"configured":  true,
		"dataReady":   false,
		"source":      "jrs33",
		"baseUrl":     resolved.BaseURL,
		"domainStale": resolved.Stale,
		"verifiedAt":  resolved.VerifiedAt,
		"discovered":  resolved.Discovered,
	}, nil
}

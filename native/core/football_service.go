package core

import "context"

// footballMatches 提供独立足球赛事数据接口。
// 第一阶段先固定 Flutter/Go 数据契约；赛事供应商接入在后续阶段完成。
func (engine *nativeEngine) footballMatches(ctx context.Context) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return map[string]any{
		"items":      []any{},
		"configured": false,
	}, nil
}

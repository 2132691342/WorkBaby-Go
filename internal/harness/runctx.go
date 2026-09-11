// 运行上下文与能力注入的转发层。
//
// 身份 key（runID / sessionID）与委派接口的唯一定义在 tool 包——工具需要据此解析
// 会话工作区、发起子任务；harness 只做转发，避免两处各存一套 key 导致读写对不上。
package harness

import (
	"context"

	"WorkBaby/internal/tool"
)

// WithRunContext 注入 runID / sessionID。
func WithRunContext(ctx context.Context, runID, sessionID string) context.Context {
	return tool.WithRunIdentity(ctx, runID, sessionID)
}

// RunIDFromCtx 取 runID（缺省空串）。
func RunIDFromCtx(ctx context.Context) string { return tool.RunIDFromCtx(ctx) }

// SessionIDFromCtx 取 sessionID（缺省空串）。
func SessionIDFromCtx(ctx context.Context) string { return tool.SessionIDFromCtx(ctx) }

// WithDelegator 注入委派能力（Runner 在 run 开始时注入自身）。
func WithDelegator(ctx context.Context, d tool.Delegator) context.Context {
	return tool.WithDelegator(ctx, d)
}

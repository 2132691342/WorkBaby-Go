// 运行上下文与能力注入的转发层。
// 身份 key（runID / sessionID）与委派接口唯一定义在 tool 包，harness 只做转发。
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

// resumedKey 标记「本 run 是检查点续跑的回放」：审批门据此允许按已决记录快速裁决。
type resumedKey struct{}

// WithResumedRun 标记续跑回放（Runner.Resume 注入）。
func WithResumedRun(ctx context.Context) context.Context {
	return context.WithValue(ctx, resumedKey{}, true)
}

// IsResumedRun 本轮工具调用是否处于续跑回放。
func IsResumedRun(ctx context.Context) bool {
	v, _ := ctx.Value(resumedKey{}).(bool)
	return v
}

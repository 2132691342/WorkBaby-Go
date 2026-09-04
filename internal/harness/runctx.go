package harness

import (
	"context"

	"WorkBaby/internal/tool"
)

// runCtxKey 私有键类型，避免与调用方 ctx 值冲突。
type runCtxKey int

const (
	runCtxKeyRunID runCtxKey = iota
	runCtxKeySessionID
)

// WithRunContext 把 runID/sessionID 注入 ctx。
//
// 用途：深层工具（如 exec 的审批门 tool.Approver）无需把身份参数一路穿透
// Tool 接口签名，即可在事件载荷里带上 runId/sessionId（前端按 runId 过滤）。
func WithRunContext(ctx context.Context, runID, sessionID string) context.Context {
	ctx = context.WithValue(ctx, runCtxKeyRunID, runID)
	return context.WithValue(ctx, runCtxKeySessionID, sessionID)
}

// RunIDFromCtx 取 run 上下文中的 runID（缺省空串）。
func RunIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(runCtxKeyRunID).(string)
	return id
}

// SessionIDFromCtx 取 run 上下文中的 sessionID（缺省空串）。
func SessionIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(runCtxKeySessionID).(string)
	return id
}

// WithDelegator 把委派能力注入 ctx（Runner 在 run 开始时注入自身）。
//
// 委派接口与 ctx 存取定义在 tool 包（与 Approver 对称）：工具不感知 harness，
// 这里只做一个薄转发，调用点无需 import tool。
func WithDelegator(ctx context.Context, d tool.Delegator) context.Context {
	return tool.WithDelegator(ctx, d)
}

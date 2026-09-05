package harness

import (
	"context"

	"WorkBaby/internal/tool"
)

// WithRunContext 把 runID/sessionID 注入 ctx。
//
// 身份存取的唯一实现在 tool 包（工具要据此解析会话工作区根），这里只做薄转发，
// 避免 harness 与 tool 各存一套 key 导致写入与读取对不上。
func WithRunContext(ctx context.Context, runID, sessionID string) context.Context {
	return tool.WithRunIdentity(ctx, runID, sessionID)
}

// RunIDFromCtx 取 run 上下文中的 runID（缺省空串）。
func RunIDFromCtx(ctx context.Context) string { return tool.RunIDFromCtx(ctx) }

// SessionIDFromCtx 取 run 上下文中的 sessionID（缺省空串）。
func SessionIDFromCtx(ctx context.Context) string { return tool.SessionIDFromCtx(ctx) }

// WithDelegator 把委派能力注入 ctx（Runner 在 run 开始时注入自身）。
//
// 委派接口与 ctx 存取定义在 tool 包（与 Approver 对称）：工具不感知 harness，
// 这里只做一个薄转发，调用点无需 import tool。
func WithDelegator(ctx context.Context, d tool.Delegator) context.Context {
	return tool.WithDelegator(ctx, d)
}

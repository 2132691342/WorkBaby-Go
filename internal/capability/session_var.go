package capability

import (
	"context"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/tool"
)

// SessionVarSource 结构化状态读取（对齐 service 实现，避免包耦合）。
type SessionVarSource interface {
	// List 返回会话级 + 用户级合并清单。
	List(ctx context.Context, sessionID string) ([]domain.SessionVarItem, error)
	// ListTemp 返回 run 级临时态（run 结束即失效）。
	ListTemp(runID string) []domain.SessionVarItem
}

// SessionVar 结构化状态能力：把三层 State（user/session/temp）作为 system 段注入，跨轮/跨压缩不丢。
type SessionVar struct{ src SessionVarSource }

// NewSessionVar 构造结构化状态能力；src 为 nil 时 Preload 恒为空。
func NewSessionVar(src SessionVarSource) *SessionVar { return &SessionVar{src: src} }

// ID 实现 Capability。
func (v *SessionVar) ID() string { return "session_var" }

// Tools 实现 Capability：session_var 工具由装配方单独注册，此处不重复暴露。
func (v *SessionVar) Tools() []tool.Tool { return nil }

// Capture 实现 Capability：变量由工具与前端显式写入，run 后无沉淀。
func (v *SessionVar) Capture(ctx context.Context, c *CaptureCtx) error { return nil }

// Preload 实现 Capability：按作用域分组注入（用户级偏好 → 会话状态 → 本轮临时态）。
func (v *SessionVar) Preload(ctx context.Context, c *PreloadCtx) ([]harness.ContextPiece, error) {
	if v == nil || v.src == nil {
		return nil, nil
	}
	items, err := v.src.List(ctx, c.SessionID)
	if err != nil {
		items = nil // 读取异常静默跳过，不阻断 run
	}
	if ts := v.src.ListTemp(c.RunID); len(ts) > 0 {
		items = append(items, ts...)
	}
	if len(items) == 0 {
		return nil, nil
	}
	body := renderScopedVars(items)
	if body == "" {
		return nil, nil
	}
	return []harness.ContextPiece{{
		Key:      "session_vars",
		Title:    "结构化状态（user 跨会话 / session 会话内 / temp 本轮）",
		Body:     body,
		Priority: harness.PriorityLow,
	}}, nil
}

// renderScopedVars 按作用域分节渲染；顺序固定 user → session → temp。
func renderScopedVars(items []domain.SessionVarItem) string {
	groups := []struct {
		scope domain.SessionVarScope
		title string
	}{
		{domain.SessionVarScopeUser, "用户级（跨会话）"},
		{domain.SessionVarScopeSession, "会话级"},
		{domain.SessionVarScopeTemp, "本轮临时"},
	}
	var b strings.Builder
	for _, g := range groups {
		var wrote bool
		for _, it := range items {
			if it.Scope != g.scope {
				continue
			}
			if !wrote {
				b.WriteString("[" + g.title + "]\n")
				wrote = true
			}
			b.WriteString("- " + it.Key + " = " + it.Value + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

package harness

import (
	"strings"

	"WorkBaby/internal/llm"
)

// ContextPiece 一条上下文装配材料（ContextAssembler）。
type ContextPiece struct {
	Key   string // persona | memory | skill | workspace | plan（仅语义/调试，装配序由 Add 顺序决定）
	Title string // 段标题（拼进 system 帮助模型识别来源）
	Body  string
}

// ContextAssembler 按 Add 顺序把各段 system 材料装配为一条 system 消息。
//
// 与人格变体解耦：Definition.Persona 只是其中一段（key=persona），记忆/技能/工作区等
// 段由装配方按需追加。空装配返回 nil（不产生 system 消息）。
type ContextAssembler struct {
	pieces []ContextPiece
}

// NewContextAssembler 构造空装配器。
func NewContextAssembler() *ContextAssembler { return &ContextAssembler{} }

// Add 追加一段装配材料（返回自身便于链式）。
func (a *ContextAssembler) Add(piece ContextPiece) *ContextAssembler {
	if piece.Body == "" {
		return a
	}
	a.pieces = append(a.pieces, piece)
	return a
}

// Clear 清空已装配段。
func (a *ContextAssembler) Clear() { a.pieces = a.pieces[:0] }

// Count 当前段数。
func (a *ContextAssembler) Count() int { return len(a.pieces) }

// Build 把全部段合并为一条 system 消息；无段返回 nil。
func (a *ContextAssembler) Build() *llm.Message {
	if len(a.pieces) == 0 {
		return nil
	}
	var sb strings.Builder
	for i, p := range a.pieces {
		if p.Title != "" {
			sb.WriteString("【" + p.Title + "】\n")
		}
		sb.WriteString(p.Body)
		if i < len(a.pieces)-1 {
			sb.WriteString("\n\n")
		}
	}
	return llm.SystemMessage(sb.String())
}

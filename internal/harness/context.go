package harness

import (
	"strings"

	"WorkBaby/internal/llm"
)

// system 段裁剪优先级：数值越小越核心。system 消息被压缩器无条件保留，
// 而能力注入只增不减，没有裁剪序时大段材料会吃光小窗口模型的预算。
const (
	PriorityEssential = 10 // 人格 / 环境信息 / 工作区纪律：永不裁剪
	PriorityHigh      = 30 // 历史归档摘要 / 压缩保留指示
	PriorityMedium    = 50 // 记忆 / 知识库召回（默认值）
	PriorityLow       = 70 // Skill 正文 / 计划清单
	PriorityLowest    = 80 // 工作流清单等参考材料
)

// DefaultPiecePriority 各段默认优先级（能力未显式声明时）。
const DefaultPiecePriority = PriorityMedium

// ContextPiece 一条上下文装配材料（ContextAssembler）。
type ContextPiece struct {
	Key      string // persona | memory | skill | workspace | plan（仅语义/调试，装配序由 Add 顺序决定）
	Title    string // 段标题（拼进 system 帮助模型识别来源）
	Body     string
	Priority int // 裁剪优先级（数值越小越核心，超预算时从大到小丢弃）；0 = DefaultPiecePriority
}

// contextPriority 段生效优先级。
func (p ContextPiece) contextPriority() int {
	if p.Priority <= 0 {
		return DefaultPiecePriority
	}
	return p.Priority
}

// ContextAssembler 按 Add 顺序把各段 system 材料装配为一条 system 消息。
//
// 人格只是其中一段（key=persona），记忆 / 技能 / 工作区等由装配方按需追加；
// 无段返回 nil（不产生 system 消息），超预算裁剪走 BuildWithin。
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
	return assemble(a.pieces)
}

// BuildWithin 预算内装配：总 rune 数超过 maxRunes 时按 Priority 从低到高丢段
// （同优先级取后加入者），返回被丢弃段的 key 列表（供可观测）。
// maxRunes<=0 或丢无可丢（只剩 essential）时保留全部剩余段。
func (a *ContextAssembler) BuildWithin(maxRunes int) (*llm.Message, []string) {
	if maxRunes <= 0 {
		return assemble(a.pieces), nil
	}
	pieces := append([]ContextPiece(nil), a.pieces...)
	var dropped []string
	for pieceRunes(pieces) > maxRunes {
		idx := -1
		worst := PriorityEssential - 1
		for i, p := range pieces {
			if p.contextPriority() > worst {
				worst = p.contextPriority()
				idx = i
			}
		}
		// 只剩 essential（或已无段）仍超预算：保留剩余，不再裁剪
		if idx < 0 || worst <= PriorityEssential {
			break
		}
		dropped = append(dropped, pieces[idx].Key)
		pieces = append(pieces[:idx], pieces[idx+1:]...)
	}
	return assemble(pieces), dropped
}

func assemble(pieces []ContextPiece) *llm.Message {
	if len(pieces) == 0 {
		return nil
	}
	var sb strings.Builder
	for i, p := range pieces {
		if p.Title != "" {
			sb.WriteString("【" + p.Title + "】\n")
		}
		sb.WriteString(p.Body)
		if i < len(pieces)-1 {
			sb.WriteString("\n\n")
		}
	}
	return llm.SystemMessage(sb.String())
}

// pieceRunes 估算段的 rune 占用（标题 + 正文 + 分隔）。
func pieceRunes(pieces []ContextPiece) int {
	n := 0
	for _, p := range pieces {
		n += len([]rune(p.Title)) + len([]rune(p.Body)) + 4
	}
	return n
}

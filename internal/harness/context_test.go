package harness

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// context_test.go system 装配预算：system 段被所有压缩器无条件保留，
// 超预算时必须按优先级从低到高裁剪，且 essential（人格/工作区）永不丢弃。

// TestBuildWithinDropsLowPriority 回归：预算超限时按 Priority 降序丢段。
// 没有裁剪序时 Skill 正文 / 工作流清单会吃光小窗口模型的全部预算。
func TestBuildWithinDropsLowPriority(t *testing.T) {
	a := NewContextAssembler()
	a.Add(ContextPiece{Key: "persona", Title: "角色", Body: "你是助手", Priority: PriorityEssential})
	a.Add(ContextPiece{Key: "memory", Title: "记忆", Body: strings.Repeat("记", 300)}) // 默认 Medium
	a.Add(ContextPiece{Key: "skill", Title: "技能", Body: strings.Repeat("技", 300), Priority: PriorityLow})
	a.Add(ContextPiece{Key: "workflows", Title: "工作流", Body: strings.Repeat("流", 300), Priority: PriorityLowest})

	// 预算只装得下 persona + memory + skill → workflows 被丢
	msg, dropped := a.BuildWithin(760)
	assert.NotNil(t, msg)
	assert.Equal(t, []string{"workflows"}, dropped)

	// 预算更紧 → skill 与 memory 也丢，只剩 essential
	msg, dropped = a.BuildWithin(30)
	assert.NotNil(t, msg)
	assert.Equal(t, []string{"workflows", "skill", "memory"}, dropped)
	assert.Contains(t, msg.Content, "你是助手", "essential 永不丢弃")

	// 丢无可丢：只剩 essential 且仍超预算时保留剩余（宁超也不丢核心指令）
	b := NewContextAssembler()
	b.Add(ContextPiece{Key: "core", Title: "核心", Body: strings.Repeat("核", 100), Priority: PriorityEssential})
	msg2, dropped2 := b.BuildWithin(10)
	assert.Empty(t, dropped2, "essential 不参与裁剪")
	assert.Contains(t, msg2.Content, strings.Repeat("核", 100))

	// 无预算限制 = 全量 Build
	full := a.Build()
	within, dropped := a.BuildWithin(0)
	assert.Empty(t, dropped)
	assert.Equal(t, full.Content, within.Content)
}

// TestBuildWithinZeroBudgetPiece 优先级为 0 的段按默认 Medium 处理（不参与首轮裁剪）。
func TestBuildWithinZeroBudgetPiece(t *testing.T) {
	a := NewContextAssembler()
	a.Add(ContextPiece{Key: "p1", Title: "a", Body: strings.Repeat("字", 100), Priority: PriorityLowest})
	a.Add(ContextPiece{Key: "p2", Title: "b", Body: strings.Repeat("字", 100)}) // 默认 Medium

	msg, dropped := a.BuildWithin(120)
	assert.NotNil(t, msg)
	assert.Equal(t, []string{"p1"}, dropped)
	assert.Contains(t, msg.Content, strings.Repeat("字", 100))
}

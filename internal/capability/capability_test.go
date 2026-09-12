package capability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/llm"
)

// TestRelevanceGate 重型段门控：显式 opt-in > 关键词 > 默认关闭；opt-in 串不残留在检索词里。
func TestRelevanceGate(t *testing.T) {
	g := NewRelevanceGate(memoryGateTokens, memoryGateKeywords)
	cases := []struct {
		in   string
		open bool
		why  string
	}{
		{"@memory 帮我想想", true, "opt_in"},
		{"@Memory 帮我想想", true, "opt_in"},
		{"上次我们说的那个方案", true, "keyword"},
		{"今天天气不错", false, "off"},
		{"", false, "off"},
	}
	for _, c := range cases {
		got := g.Evaluate(c.in)
		require.Equal(t, c.open, got.Open, "输入 %q", c.in)
		require.Equal(t, c.why, got.Why, "输入 %q", c.in)
	}
	require.Equal(t, "帮我查一下 部署流程", NewRelevanceGate([]string{"@knowledge"}, nil).Clean("@knowledge 帮我查一下 部署流程"))
}

// fakeSink 记录入箱候选。
type fakeSink struct{ got []domain.InboxCandidate }

func (f *fakeSink) Submit(_ context.Context, _, _ string, c []domain.InboxCandidate) (int, error) {
	f.got = append(f.got, c...)
	return len(c), nil
}

// TestInboxCapture 收件箱抽取的三道闸：记忆开关优先、琐碎回合不抽取、有过程或长输入才抽取。
func TestInboxCapture(t *testing.T) {
	def := harness.Agent("default")
	def.Memory.Enabled = true

	t.Run("memory_disabled_skips", func(t *testing.T) {
		off := def
		off.Memory.Enabled = false
		sink := &fakeSink{}
		c := NewInbox(sink, func(context.Context, *CaptureCtx) ([]domain.InboxCandidate, error) {
			return []domain.InboxCandidate{{Kind: domain.InboxKindFact, Title: "t"}}, nil
		}, nil)
		require.NoError(t, c.Capture(context.Background(), &CaptureCtx{
			UserInput: "很长的用户输入内容用来绕过成本闸", Def: off,
			Transcript: []llm.Message{{Role: llm.RoleUser, Content: "很长"}},
		}))
		require.Empty(t, sink.got)
	})

	t.Run("trivial_turn_skips", func(t *testing.T) {
		sink := &fakeSink{}
		called := false
		c := NewInbox(sink, func(context.Context, *CaptureCtx) ([]domain.InboxCandidate, error) {
			called = true
			return nil, nil
		}, nil)
		require.NoError(t, c.Capture(context.Background(), &CaptureCtx{
			UserInput: "你好", Def: def,
			Transcript: []llm.Message{{Role: llm.RoleUser, Content: "你好"}},
		}))
		require.False(t, called, "无工具且短输入不应再花一次 LLM 调用")
	})

	t.Run("tool_call_extracts", func(t *testing.T) {
		sink := &fakeSink{}
		c := NewInbox(sink, func(context.Context, *CaptureCtx) ([]domain.InboxCandidate, error) {
			return []domain.InboxCandidate{{Kind: domain.InboxKindFact, Title: "user:x"}}, nil
		}, nil)
		require.NoError(t, c.Capture(context.Background(), &CaptureCtx{
			UserInput: "hi", Def: def,
			Transcript: []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{Function: llm.FunctionCall{Name: "file_read"}}}}},
		}))
		require.Len(t, sink.got, 1)
	})
}

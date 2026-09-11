package harness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolCallMsg 构造带单个 tool_call 的 assistant 消息。
func toolCallMsg(id, name string) *llm.Message {
	return &llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID:       id,
			Type:     "function",
			Function: llm.FunctionCall{Name: name, Arguments: "{}"},
		}},
	}
}

// assertPairingIntact 断言消息序列满足上游 LLM 的工具配对协议：
// 每条 tool 消息的 tool_call_id 都能在前置 assistant.tool_calls 中找到；
// 每个带 tool_calls 的 assistant 后面紧跟覆盖其全部调用 id 的 tool 结果。
func assertPairingIntact(t *testing.T, ms []*llm.Message) {
	t.Helper()
	seen := map[string]bool{}
	for i, m := range ms {
		if m == nil {
			continue
		}
		switch m.Role {
		case llm.RoleTool:
			require.NotEmpty(t, m.ToolCallID, "tool 消息缺少 tool_call_id @%d", i)
			require.True(t, seen[m.ToolCallID], "孤儿 tool 消息：%s 无前置 assistant tool_calls @%d", m.ToolCallID, i)
		case llm.RoleAssistant:
			ids := map[string]bool{}
			for _, tc := range m.ToolCalls {
				ids[tc.ID] = true
			}
			for id := range ids {
				seen[id] = true
			}
			if len(ids) > 0 {
				for j := i + 1; j < len(ms) && ms[j] != nil && ms[j].Role == llm.RoleTool; j++ {
					delete(ids, ms[j].ToolCallID)
				}
				assert.Empty(t, ids, "assistant(tool_calls) 缺少对应的 tool 结果 @%d", i)
			}
		}
	}
}

// TestMicroCompressorPreservesToolPairing 回归：压缩绝不拆散 assistant(tool_calls) 与其 tool 结果。
// 旧实现第一遍只删 tool 结果（assistant 悬空）、第二遍盲切（尾部孤儿 tool），两条路都会
// 触发上游 400「tool result's tool id not found」。
//
// 两种布局各跑一遍：多工具段（折叠路径）与「切点恰好落在工具对中间」（第二遍截断回退路径）。
func TestMicroCompressorPreservesToolPairing(t *testing.T) {
	big := strings.Repeat("工具结果很长", 40)
	cases := []struct {
		name   string
		msgs   []*llm.Message
		budget int
	}{
		{
			name: "多工具段折叠后仍配对",
			msgs: []*llm.Message{
				llm.SystemMessage("sys"),
				llm.UserMessage("start"),
				toolCallMsg("CALL_A", "exec"),
				llm.ToolMessage("CALL_A", "exec", big),
				llm.UserMessage("中间一轮"),
				toolCallMsg("CALL_B", "file_read"),
				llm.ToolMessage("CALL_B", "file_read", big),
				llm.UserMessage("继续"),
			},
			budget: 100,
		},
		{
			name:   "截断切点落在工具对中间需回退",
			msgs:   threeSegments(),
			budget: 120,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Greater(t, EstimateTokens(tc.msgs), tc.budget, "前置：估算应超预算")
			out := (MicroCompressor{}).Compress(tc.msgs, tc.budget)
			assertPairingIntact(t, out)
			assert.Equal(t, llm.RoleSystem, out[0].Role, "首条 system 保留")
		})
	}
}

// threeSegments 构造 3 个「assistant(tool_calls) + 大 tool 结果」段，使对半截断的切点落在段中间。
func threeSegments() []*llm.Message {
	out := []*llm.Message{llm.SystemMessage("sys"), llm.UserMessage("start")}
	for i := 0; i < 3; i++ {
		id := "CALL_" + string(rune('A'+i))
		out = append(out, toolCallMsg(id, "exec"),
			llm.ToolMessage(id, "exec", strings.Repeat("结果内容", 60)))
	}
	return append(out, llm.UserMessage("end"))
}

// TestRunnerContextBudgetCompress 冒烟：极小上下文预算下超长上下文仍能正常跑完（压缩不打断循环）。
func TestRunnerContextBudgetCompress(t *testing.T) {
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{{ToolCall: &llm.NormalizedToolCall{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"msg":"hi"}`)}}},
		{{Delta: llm.Message{Role: llm.RoleAssistant, Content: "done"}}},
	}}
	sink := &recordingSink{}
	reg := tool.NewRegistry()
	require.NoError(t, reg.Register(echoTool{}))
	cfg := DefaultConfig()
	cfg.ContextBudget = 50 // 极小预算强制每轮压缩
	r := NewRunner(p, sink, cfg).WithTools(reg, []llm.ToolDefinition{{Name: "echo", Description: "e"}})

	msgs := []*llm.Message{
		llm.SystemMessage("sys"),
		llm.UserMessage("long context" + strings.Repeat("z", 5000)),
	}
	res := r.RunMessages(context.Background(), "RUN_C", "SESSION_C", "MSG_C", "mock", msgs)
	require.NoError(t, res.Err)
	assert.Equal(t, ReasonEndTurn, res.Reason)
	assert.Contains(t, res.Content, "done")
}

// TestToolResultTruncateRuneSafe 回归：截断必须落在 rune 边界——旧实现按 byte 切，
// 50k 边界会把 3 字节汉字劈成残片，乱码回填给 LLM（严格上游直接 400）。
func TestToolResultTruncateRuneSafe(t *testing.T) {
	s := strings.Repeat("汉", 51_000) // 153_000 bytes，超 rune 限
	got := truncate(s, 50_000)
	assert.True(t, utf8.ValidString(got), "截断结果必须是合法 UTF-8")
	assert.True(t, strings.HasSuffix(got, "\n... (truncated)"))
	assert.LessOrEqual(t, len([]rune(got)), 50_000+len("\n... (truncated)"))
	assert.True(t, strings.HasPrefix(got, strings.Repeat("汉", 10)), "内容前缀无损")
}

// TestEstimateTokensCountsChinese 回归：中文按 rune 口径估算，不能按「字符数/4」低估。
// 低估 3~4 倍会让压缩的尾部保留量失准，摘要吞掉本该保留的近期上下文。
// 断言行为区间（而非内部函数等价），实现换代时测试不会无故失效。
func TestEstimateTokensCountsChinese(t *testing.T) {
	text := &llm.Message{Role: llm.RoleUser, Content: strings.Repeat("部署配置", 100)} // 400 字
	got := EstimateTokens([]*llm.Message{text})
	assert.Greater(t, got, 200, "400 字中文不应被估到 200 token 以下")
	assert.Less(t, got, 1000, "也不应高估到离谱")
}

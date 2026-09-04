package harness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


func TestMicroCompressorCleansOldToolResults(t *testing.T) {
	big := strings.Repeat("工具结果很长", 40) // ≈70 token
	msgs := []*llm.Message{
		llm.SystemMessage("sys"),
		llm.UserMessage("start"),
		llm.ToolMessage("t1", "exec", big),
		{Role: llm.RoleAssistant, Content: "下一步"},
		llm.ToolMessage("t2", "file_read", big),
		llm.UserMessage("继续"),
	}
	budget := 100
	require.Greater(t, EstimateTokens(msgs), budget, "前置：估算应超预算")

	out := (MicroCompressor{}).Compress(msgs, budget)
	assert.LessOrEqual(t, EstimateTokens(out), budget, "压缩后应回到预算内")
	assert.NotNil(t, out[0], "首条 system 保留")
	assert.Equal(t, llm.RoleSystem, out[0].Role)
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

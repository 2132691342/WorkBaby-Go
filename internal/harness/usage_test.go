package harness

import (
	"context"
	"encoding/json"
	"testing"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// usage_test.go 用量口径回归：RunResult 的两个口径（末轮 per-turn / 全 run 累计）
// 各有消费方——ContextUsage 展示用末轮，run_records 落库用累计，混用会让
// 展示放大 N 倍或消耗统计偏低。

// TestRunResultAccumulatedUsage 累计口径必须等于各轮之和；Usage 保持末轮 per-turn。
func TestRunResultAccumulatedUsage(t *testing.T) {
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"msg":"hi"}`)}},
			{FinalUsage: &llm.TokenUsage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
			{FinalUsage: &llm.TokenUsage{InputTokens: 120, OutputTokens: 5, TotalTokens: 125}},
		},
	}}
	reg := tool.NewRegistry()
	require.NoError(t, reg.Register(echoTool{}))
	r := NewRunner(p, &recordingSink{}, DefaultConfig()).
		WithTools(reg, []llm.ToolDefinition{{Name: "echo", Description: "e"}})

	res := r.RunMessages(context.Background(), "RUN_U", "SESSION_U", "MSG_U", "mock",
		[]*llm.Message{llm.SystemMessage("sys"), llm.UserMessage("go")})
	require.NoError(t, res.Err)
	require.Len(t, res.Turns, 2)

	assert.Equal(t, 220, res.Accumulated.InputTokens, "累计 input = 各轮之和")
	assert.Equal(t, 15, res.Accumulated.OutputTokens)
	assert.Equal(t, 235, res.Accumulated.TotalTokens)
	assert.Equal(t, 120, res.Usage.InputTokens, "Usage 保持末轮 per-turn（ContextUsage 口径）")
	assert.Equal(t, 125, res.Usage.TotalTokens)
}

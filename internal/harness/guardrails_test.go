package harness

// 护栏（guardrails）：预算 / 死循环 / 单工具超时三道闸。
//
// 判据是「越过边界时 run 停在可解释的终态，且模型看得到原因」——
// 预算耗尽要少花钱，循环要早停而不是跑满轮次，单工具卡死不能拖垮整轮。

import (
	"context"
	"encoding/json"
	"testing"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/tool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunnerResourceGuards token 预算熔断 / 交替循环护栏 / 单工具超时隔离。
func TestRunnerResourceGuards(t *testing.T) {
	t.Run("token_budget_stops_run", testGuardTokenBudgetStopsRun)
	t.Run("alternating_loop_refused", testGuardAlternatingLoopRefused)
	t.Run("tool_timeout_isolated", testGuardToolTimeoutIsolated)
	t.Run("refused_reason_structured", testGuardRefusedReasonStructured)
}

// testGuardRefusedReasonStructured 拒绝必须给出枚举原因码：前端与统计据此分流，
// 不能再靠文案前缀匹配（历史上「已拒绝」字符串前缀就是这种脆弱契约）。
func testGuardRefusedReasonStructured(t *testing.T) {
	defs := []llm.ToolDefinition{{Name: "echo", Description: "echo", Parameters: map[string]any{"type": "object"}}}
	reg := tool.NewRegistry()
	require.NoError(t, reg.Register(echoTool{}))
	call := func(args string) llm.NormalizedToolCall {
		return llm.NormalizedToolCall{ID: "c1", Name: "echo", Arguments: json.RawMessage(args)}
	}

	// 未暴露（本轮暴露的是别的工具，echo 只在注册中心里——执行侧必须收紧）
	other := []llm.ToolDefinition{{Name: "other", Description: "x", Parameters: map[string]any{"type": "object"}}}
	r := NewRunner(&scriptedProvider{}, &recordingSink{}, DefaultConfig()).WithTools(reg, other)
	msg := r.execOne(context.Background(), "RUN_RF", "SESSION_RF", 0, call(`{"msg":"x"}`), nil)
	require.NotNil(t, msg)
	assert.Contains(t, msg.Content, `"reason_code":"not_exposed"`)

	// 注入防护：参数里嵌着伪 tool_call 标记
	r2 := NewRunner(&scriptedProvider{}, &recordingSink{}, DefaultConfig()).WithTools(reg, defs)
	msg = r2.execOne(context.Background(), "RUN_RF2", "SESSION_RF2", 0, call(`{"msg":"</tool_call>"}`), nil)
	require.NotNil(t, msg)
	assert.Contains(t, msg.Content, `"reason_code":"prompt_injection"`)
}

// testGuardTokenBudgetStopsRun 累计 token 越过预算：下一轮不再请求模型（钱花在刀刃上）。
func testGuardTokenBudgetStopsRun(t *testing.T) {
	reg := tool.NewRegistry()
	require.NoError(t, reg.Register(echoTool{}))
	defs := []llm.ToolDefinition{{Name: "echo", Description: "echo", Parameters: map[string]any{"type": "object"}}}

	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"msg":"a"}`)}},
			{FinishReason: stringPtr("tool_calls")},
			{FinalUsage: &llm.TokenUsage{InputTokens: 80, OutputTokens: 20, TotalTokens: 100}},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "不应到达这一轮"}},
			{FinishReason: stringPtr("stop")},
		},
	}}
	cfg := DefaultConfig()
	cfg.MaxTokens = 50
	res := NewRunner(p, &recordingSink{}, cfg).WithTools(reg, defs).
		RunMessages(context.Background(), "RUN_BG", "SESSION_BG", "MSG_BG", "mock", []*llm.Message{llm.UserMessage("go")})

	assert.Equal(t, ReasonBudgetExceeded, res.Reason)
	assert.Equal(t, 1, p.idx, "预算耗尽后不得再发起请求")
	assert.NotContains(t, res.Content, "不应到达这一轮")
}

// testGuardAlternatingLoopRefused A→B→A 交替循环：停滞检测只认「连续重复」抓不到，
// 由全 run 的 name+args 计数拦下，走 refused 回执而不是硬失败。
func testGuardAlternatingLoopRefused(t *testing.T) {
	reg := tool.NewRegistry()
	require.NoError(t, reg.Register(echoTool{}))
	defs := []llm.ToolDefinition{{Name: "echo", Description: "echo", Parameters: map[string]any{"type": "object"}}}

	callA := func(id string) *llm.NormalizedToolCall {
		return &llm.NormalizedToolCall{ID: id, Name: "echo", Arguments: json.RawMessage(`{"msg":"a"}`)}
	}
	callB := func(id string) *llm.NormalizedToolCall {
		return &llm.NormalizedToolCall{ID: id, Name: "echo", Arguments: json.RawMessage(`{"msg":"b"}`)}
	}
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: callA("c1")}, {ToolCall: callB("c2")},
			{FinishReason: stringPtr("tool_calls")},
		},
		{
			{ToolCall: callA("c3")},
			{FinishReason: stringPtr("tool_calls")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "改用别的办法"}},
			{FinishReason: stringPtr("stop")},
		},
	}}
	cfg := DefaultConfig()
	cfg.LoopLimit = 2
	sink := &recordingSink{}
	res := NewRunner(p, sink, cfg).WithTools(reg, defs).
		RunMessages(context.Background(), "RUN_LP", "SESSION_LP", "MSG_LP", "mock", []*llm.Message{llm.UserMessage("go")})

	assert.Equal(t, ReasonEndTurn, res.Reason, "循环被拦后模型应能自行换路收尾")
	assert.Contains(t, res.Content, "改用别的办法")

	var refused bool
	for _, e := range sink.events {
		if e.Kind != EventToolResult {
			continue
		}
		p, ok := e.Payload.(ToolResultPayload)
		if ok && p.Refused && p.RefusedReason == string(RefusedLoopGuard) {
			refused = true
		}
	}
	assert.True(t, refused, "第二次重复调用应收到 loop_guard 结构化拒绝（不再靠文案判定）")
}

// testGuardToolTimeoutIsolated 单工具超时：ctx 传到工具、超时结果回填给模型，run 照常收尾。
func testGuardToolTimeoutIsolated(t *testing.T) {
	reg := tool.NewRegistry()
	require.NoError(t, reg.Register(slowTool{}))
	defs := []llm.ToolDefinition{{Name: "slow", Description: "slow", Parameters: map[string]any{"type": "object"}}}

	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "s1", Name: "slow", Arguments: json.RawMessage(`{}`)}},
			{FinishReason: stringPtr("tool_calls")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "工具超时，换条路"}},
			{FinishReason: stringPtr("stop")},
		},
	}}
	cfg := DefaultConfig()
	cfg.ToolCallTimeout = 80_000_000 // 80ms
	sink := &recordingSink{}
	res := NewRunner(p, sink, cfg).WithTools(reg, defs).
		RunMessages(context.Background(), "RUN_TO", "SESSION_TO", "MSG_TO", "mock", []*llm.Message{llm.UserMessage("go")})

	assert.Equal(t, ReasonEndTurn, res.Reason)
	assert.Contains(t, res.Content, "工具超时，换条路")

	var gotErr string
	for _, e := range sink.events {
		if e.Kind != EventToolResult {
			continue
		}
		if p, ok := e.Payload.(ToolResultPayload); ok && p.Name == "slow" {
			gotErr = p.Err
		}
	}
	assert.Contains(t, gotErr, "context deadline exceeded", "工具必须感知到超时 ctx")
}

package harness

import (
	"WorkBaby/internal/llm"
	"WorkBaby/internal/tool"
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockProvider struct {
	chunks []llm.StreamChunk
	err    error
}

func (m *mockProvider) Name() string           { return "mock" }
func (m *mockProvider) Kind() llm.ProviderKind { return "openai" }
func (m *mockProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}
func (m *mockProvider) Models(_ context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (m *mockProvider) Ping(_ context.Context) error                      { return nil }
func (m *mockProvider) Stream(ctx context.Context, _ *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	if m.err != nil {
		return nil, m.err
	}
	out := make(chan llm.StreamChunk, len(m.chunks)+1)
	go func() {
		defer close(out)
		for _, ch := range m.chunks {
			select {
			case <-ctx.Done():
				return
			default:
				out <- ch
			}
		}
	}()
	return out, nil
}

// scriptedProvider 按调用序号返回预设 chunk 序列，模拟多轮 ReAct。
type scriptedProvider struct {
	mu      sync.Mutex
	calls   [][]llm.StreamChunk
	idx     int
	lastReq *llm.ChatRequest
}

func (s *scriptedProvider) Name() string           { return "scripted" }
func (s *scriptedProvider) Kind() llm.ProviderKind { return "openai" }
func (s *scriptedProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}
func (s *scriptedProvider) Models(_ context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (s *scriptedProvider) Ping(_ context.Context) error                      { return nil }

func (s *scriptedProvider) Stream(_ context.Context, req *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastReq = req
	var out []llm.StreamChunk
	if s.idx < len(s.calls) {
		out = s.calls[s.idx]
		s.idx++
	}
	ch := make(chan llm.StreamChunk, len(out)+1)
	for _, c := range out {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (s *scriptedProvider) requestTools() []llm.ToolDefinition {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastReq == nil {
		return nil
	}
	return s.lastReq.Tools
}

// recordingSink 记录事件用于断言。
type recordingSink struct {
	events []Event
}

func (r *recordingSink) Emit(e Event) { r.events = append(r.events, e) }

func (r *recordingSink) kinds() []EventKind {
	out := make([]EventKind, len(r.events))
	for i, e := range r.events {
		out[i] = e.Kind
	}
	return out
}

// echoTool / failTool ReAct 测试用的最小工具。
type echoTool struct{}

func (echoTool) Name() string              { return "echo" }
func (echoTool) Description() string       { return "echo tool" }
func (echoTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }
func (echoTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "echo", Parameters: json.RawMessage(`{"type":"object","required":["msg"],"properties":{"msg":{"type":"string"}}}`)}
}
func (echoTool) Execute(_ context.Context, args json.RawMessage) tool.ToolResult {
	var p struct {
		Msg string `json:"msg"`
	}
	_ = json.Unmarshal(args, &p)
	return tool.ToolResult{Content: "echo:" + p.Msg}
}

type failTool struct{}

func (failTool) Name() string              { return "fail" }
func (failTool) Description() string       { return "always fail" }
func (failTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }
func (failTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "fail", Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (failTool) Execute(_ context.Context, _ json.RawMessage) tool.ToolResult {
	return tool.ToolResult{Err: errMockFail}
}

type mockError struct{ msg string }

func (e mockError) Error() string { return e.msg }

var (
	errMockLLM  = mockError{"mock LLM error"}
	errMockFail = mockError{"boom"}
)

func stringPtr(s string) *string { return &s }

// ===== 核心场景 =====

// TestRunnerThinkingSeparate 推理文本走 EventTurnThinking，不混入 Content。
func TestRunnerThinkingSeparate(t *testing.T) {
	p := &mockProvider{chunks: []llm.StreamChunk{
		{Delta: llm.Message{Role: llm.RoleAssistant, Thinking: "让我想想…"}},
		{Delta: llm.Message{Role: llm.RoleAssistant, Content: "答案是 Go"}},
	}}
	sink := &recordingSink{}
	r := NewRunner(p, sink, DefaultConfig())
	res := r.RunMessages(context.Background(), "RUN_2", "SESSION_2", "MSG_2", "mock", nil)
	if res.Err != nil {
		t.Fatalf("run failed: %v", res.Err)
	}
	if res.Thinking != "让我想想…" {
		t.Fatalf("want thinking '让我想想…', got %q", res.Thinking)
	}
	if res.Content != "答案是 Go" {
		t.Fatalf("want content '答案是 Go', got %q", res.Content)
	}
	hasThinkingEvent := false
	for _, e := range sink.events {
		if e.Kind == EventTurnThinking {
			hasThinkingEvent = true
		}
	}
	if !hasThinkingEvent {
		t.Fatalf("expected EventTurnThinking event")
	}
}

// TestRunnerReactToolCall 多轮 ReAct：round1 调工具 → 执行回填 → round2 终答。
func TestRunnerReactToolCall(t *testing.T) {
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "我先查一下"}},
			{ToolCall: &llm.NormalizedToolCall{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"msg":"hello"}`)}},
			{FinishReason: stringPtr("tool_calls")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "结果是 echo:hello"}},
			{FinishReason: stringPtr("stop")},
		},
	}}
	reg := tool.NewRegistry()
	if err := reg.Register(echoTool{}); err != nil {
		t.Fatal(err)
	}
	defs := []llm.ToolDefinition{{Name: "echo", Description: "echo tool", Parameters: map[string]any{"type": "object"}}}

	sink := &recordingSink{}
	r := NewRunner(p, sink, DefaultConfig()).WithTools(reg, defs)
	res := r.RunMessages(context.Background(), "RUN_R1", "SESSION_R1", "MSG_R1", "mock", []*llm.Message{llm.UserMessage("帮我echo hello")})

	if res.Err != nil {
		t.Fatalf("run failed: %v", res.Err)
	}
	if res.Content != "我先查一下结果是 echo:hello" {
		t.Fatalf("want combined content, got %q", res.Content)
	}
	if res.Reason != ReasonEndTurn {
		t.Fatalf("want end_turn, got %v", res.Reason)
	}
	// 工具事件完整：tool.call / tool.start / tool.result
	has := map[EventKind]bool{}
	for _, e := range sink.events {
		has[e.Kind] = true
	}
	for _, k := range []EventKind{EventToolCall, EventToolStart, EventToolResult} {
		if !has[k] {
			t.Fatalf("missing event %s; got %v", k, sink.kinds())
		}
	}
	// 第二轮请求必须带上工具定义（LLM 可见）
	if n := len(p.requestTools()); n != 1 {
		t.Fatalf("want 1 tool definition exposed, got %d", n)
	}
}

// TestRunnerStagnation 连续失败触发停滞熔断（避免死循环烧 token）。
func TestRunnerStagnation(t *testing.T) {
	call := []llm.StreamChunk{
		{ToolCall: &llm.NormalizedToolCall{ID: "f1", Name: "fail", Arguments: json.RawMessage(`{}`)}},
		{FinishReason: stringPtr("tool_calls")},
	}
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		call, call, call, call, call, // 5 次都调 fail
	}}
	reg := tool.NewRegistry()
	_ = reg.Register(failTool{})
	sink := &recordingSink{}
	r := NewRunner(p, sink, DefaultConfig()).WithTools(reg, nil)
	res := r.RunMessages(context.Background(), "RUN_R3", "SESSION_R3", "MSG_R3", "mock", nil)
	if res.Reason != ReasonStagnation {
		t.Fatalf("want stagnation, got %v", res.Reason)
	}
}

// TestRunnerTruncatedToolCallRetry 截断重发：finishReason=length 时 tool call
// 参数可能残缺——必须不执行、回填 truncated 错误让模型缩短后重发。
func TestRunnerTruncatedToolCallRetry(t *testing.T) {
	track := newTrackingTool("echo", tool.RiskReadOnly)
	track.exec = func(_ context.Context, args json.RawMessage) tool.ToolResult {
		var p struct {
			Msg string `json:"msg"`
		}
		_ = json.Unmarshal(args, &p)
		return tool.ToolResult{Content: "echo:" + p.Msg}
	}
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "我来调用"}},
			{ToolCall: &llm.NormalizedToolCall{ID: "call_t", Name: "echo", Arguments: json.RawMessage(`{"msg":"he`)}},
			{FinishReason: stringPtr("length")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "缩短参数后重发"}},
			{ToolCall: &llm.NormalizedToolCall{ID: "call_t2", Name: "echo", Arguments: json.RawMessage(`{"msg":"hello"}`)}},
			{FinishReason: stringPtr("tool_calls")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "结果是 echo:hello"}},
			{FinishReason: stringPtr("stop")},
		},
	}}
	reg := tool.NewRegistry()
	if err := reg.Register(track); err != nil {
		t.Fatal(err)
	}
	defs := []llm.ToolDefinition{{Name: "echo", Parameters: map[string]any{"type": "object"}}}

	sink := &recordingSink{}
	r := NewRunner(p, sink, DefaultConfig()).WithTools(reg, defs)
	res := r.RunMessages(context.Background(), "RUN_T1", "SESSION_T1", "MSG_T1", "mock", nil)

	if res.Err != nil {
		t.Fatalf("run failed: %v", res.Err)
	}
	if res.Reason != ReasonEndTurn {
		t.Fatalf("want end_turn, got %v", res.Reason)
	}
	// 截断轮的工具调用绝不执行；重发轮的合法调用执行恰好一次
	if track.n != 1 {
		t.Fatalf("tool executed %d times, want 1 (truncated call must not run)", track.n)
	}
	// 截断回执事件：前端按错误呈现「未执行」
	var truncResult bool
	for _, e := range sink.events {
		if e.Kind == EventToolResult {
			if pl, ok := e.Payload.(ToolResultPayload); ok && pl.Err != "" {
				truncResult = true
			}
		}
	}
	if !truncResult {
		t.Fatalf("missing truncated tool result event; got %v", sink.kinds())
	}
}

// TestRunnerSameArgsStagnation 同名同参熔断：工具一直成功但模型反复发同一调用
// （参数原样重复）达阈值 → stagnation，而不是无限跑满 MaxTurns。
func TestRunnerSameArgsStagnation(t *testing.T) {
	call := []llm.StreamChunk{
		{ToolCall: &llm.NormalizedToolCall{ID: "c", Name: "echo", Arguments: json.RawMessage(`{"msg":"x"}`)}},
		{FinishReason: stringPtr("tool_calls")},
	}
	calls := make([][]llm.StreamChunk, 0, 8)
	for range 8 {
		calls = append(calls, call)
	}
	p := &scriptedProvider{calls: calls}
	reg := tool.NewRegistry()
	if err := reg.Register(echoTool{}); err != nil {
		t.Fatal(err)
	}

	sink := &recordingSink{}
	r := NewRunner(p, sink, DefaultConfig()).WithTools(reg, nil)
	res := r.RunMessages(context.Background(), "RUN_S2", "SESSION_S2", "MSG_S2", "mock", nil)

	if res.Reason != ReasonStagnation {
		t.Fatalf("want stagnation, got %v (turns used < MaxTurns expected)", res.Reason)
	}
	if res.Err != nil {
		t.Fatalf("stagnation is a controlled stop, not an error: %v", res.Err)
	}
}

// TestRunnerShouldStopAfterTurn 优雅停止：第一轮工具跑完即满足停止条件，
// 循环主动收尾（end_turn），不再消费后续 LLM 轮次。
func TestRunnerShouldStopAfterTurn(t *testing.T) {
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"msg":"x"}`)}},
			{FinishReason: stringPtr("tool_calls")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "不该走到这"}},
			{FinishReason: stringPtr("stop")},
		},
	}}
	reg := tool.NewRegistry()
	_ = reg.Register(echoTool{})

	sink := &recordingSink{}
	r := NewRunner(p, sink, DefaultConfig()).WithTools(reg, nil).
		WithShouldStopAfterTurn(func(_ context.Context, sig *TurnSignal) bool {
			return sig.Turn >= 0 // 首轮即停
		})
	res := r.RunMessages(context.Background(), "RUN_H1", "SESSION_H1", "MSG_H1", "mock", nil)

	if res.Reason != ReasonEndTurn {
		t.Fatalf("want end_turn, got %v", res.Reason)
	}
	if p.idx != 1 {
		t.Fatalf("provider consumed %d rounds, want 1 (stop before next turn)", p.idx)
	}
}

// TestRunnerAfterToolCallRewrite 工具后处理：结果在回填模型与发事件前被钩子覆盖。
func TestRunnerAfterToolCallRewrite(t *testing.T) {
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"msg":"secret"}`)}},
			{FinishReason: stringPtr("tool_calls")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
			{FinishReason: stringPtr("stop")},
		},
	}}
	reg := tool.NewRegistry()
	_ = reg.Register(echoTool{})

	sink := &recordingSink{}
	r := NewRunner(p, sink, DefaultConfig()).WithTools(reg, nil).
		WithAfterToolCall(func(_ context.Context, _ string, _ json.RawMessage, res *tool.ToolResult) {
			res.Content = "CLEANED"
		})
	res := r.RunMessages(context.Background(), "RUN_H2", "SESSION_H2", "MSG_H2", "mock", nil)
	if res.Err != nil {
		t.Fatalf("run failed: %v", res.Err)
	}

	for _, e := range sink.events {
		if e.Kind == EventToolResult {
			if pl, ok := e.Payload.(ToolResultPayload); ok && pl.Content != "CLEANED" {
				t.Fatalf("tool result not rewritten by hook: %q", pl.Content)
			}
		}
	}
}

type trackingTool struct {
	name string
	risk tool.RiskLevel
	exec func(context.Context, json.RawMessage) tool.ToolResult

	mu sync.Mutex
	n  int
}

func newTrackingTool(name string, risk tool.RiskLevel) *trackingTool {
	return &trackingTool{name: name, risk: risk}
}

func (t *trackingTool) Name() string              { return t.name }
func (t *trackingTool) Description() string       { return "tracking " + t.name }
func (t *trackingTool) RiskLevel() tool.RiskLevel { return t.risk }
func (t *trackingTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: t.name, Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (t *trackingTool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	t.mu.Lock()
	t.n++
	t.mu.Unlock()
	if t.exec != nil {
		return t.exec(ctx, args)
	}
	return tool.ToolResult{Content: "ok:" + t.name}
}

func (t *trackingTool) calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.n
}

// panicTool Execute 直接 panic（验证 B7 panic 隔离）。
type panicTool struct{}

func (panicTool) Name() string              { return "panic_me" }
func (panicTool) Description() string       { return "panics" }
func (panicTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }
func (panicTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "panic_me", Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (panicTool) Execute(_ context.Context, _ json.RawMessage) tool.ToolResult {
	panic("boom-from-tool")
}

// newRunnerWithTools 构造带工具注册表的 Runner。
func newRunnerWithTools(t *testing.T, prov llm.Provider, tools ...tool.Tool) *Runner {
	t.Helper()
	reg := tool.NewRegistry()
	defs := make([]llm.ToolDefinition, 0, len(tools))
	for _, tk := range tools {
		require.NoError(t, reg.Register(tk))
		defs = append(defs, llm.ToolDefinition{Name: tk.Name(), Description: tk.Description()})
	}
	return NewRunner(prov, &recordingSink{}, DefaultConfig()).WithTools(reg, defs)
}

// TestRunnerReadonlyParallel 验证 B7：全只读工具轮并发执行（总耗时 ≈ 单次执行，
// 而非串行 × N）且结果按调用顺序回填。
func TestRunnerReadonlyParallel(t *testing.T) {
	const toolDelay = 100 * time.Millisecond
	ra := newTrackingTool("r_a", tool.RiskReadOnly)
	ra.exec = func(context.Context, json.RawMessage) tool.ToolResult {
		time.Sleep(toolDelay)
		return tool.ToolResult{Content: "ok:r_a"}
	}
	rb := newTrackingTool("r_b", tool.RiskReadOnly)
	rb.exec = func(context.Context, json.RawMessage) tool.ToolResult {
		time.Sleep(toolDelay)
		return tool.ToolResult{Content: "ok:r_b"}
	}

	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "c1", Name: "r_a", Arguments: json.RawMessage(`{}`)}},
			{ToolCall: &llm.NormalizedToolCall{ID: "c2", Name: "r_b", Arguments: json.RawMessage(`{}`)}},
		},
		{{Delta: llm.Message{Role: llm.RoleAssistant, Content: "done"}}},
	}}
	r := newRunnerWithTools(t, p, ra, rb)
	start := time.Now()
	res := r.RunMessages(context.Background(), "RUN_P", "SESSION_P", "MSG_P", "mock", []*llm.Message{llm.UserMessage("go")})
	elapsed := time.Since(start)
	require.NoError(t, res.Err)
	assert.Equal(t, ReasonEndTurn, res.Reason)
	assert.Equal(t, 1, ra.calls())
	assert.Equal(t, 1, rb.calls())
	assert.Less(t, elapsed, 2*toolDelay, "全只读工具应并发执行：串行需要 2× 单次耗时")
	// 工具结果按调用顺序回填（下一轮上下文中 a 在 b 前）
	joined := joinContents(p.lastMessages())
	assert.Less(t, strings.Index(joined, "ok:r_a"), strings.Index(joined, "ok:r_b"))
}

// TestRunnerToolPanicRecovered 验证 B7：单工具 panic 不拖垮 run，以 ToolResult 上报。
func TestRunnerToolPanicRecovered(t *testing.T) {
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{{ToolCall: &llm.NormalizedToolCall{ID: "c1", Name: "panic_me", Arguments: json.RawMessage(`{}`)}}},
		{{Delta: llm.Message{Role: llm.RoleAssistant, Content: "recovered"}}},
	}}
	sink := &recordingSink{}
	reg := tool.NewRegistry()
	require.NoError(t, reg.Register(panicTool{}))
	r := NewRunner(p, sink, DefaultConfig()).WithTools(reg, []llm.ToolDefinition{{Name: "panic_me", Description: "p"}})

	res := r.RunMessages(context.Background(), "RUN_P", "SESSION_P", "MSG_P", "mock", []*llm.Message{llm.UserMessage("go")})
	require.NoError(t, res.Err, "工具 panic 应被恢复，run 不崩")
	assert.Equal(t, ReasonEndTurn, res.Reason)
	assert.Contains(t, res.Content, "recovered")

	found := false
	for _, e := range sink.events {
		if e.Kind != EventToolResult {
			continue
		}
		pl := e.Payload.(ToolResultPayload)
		if pl.Name == "panic_me" && strings.Contains(pl.Err, "tool panic: boom-from-tool") {
			found = true
		}
	}
	assert.True(t, found, "工具 panic 应以 ToolResult Err 上报给模型")
}

// TestRunnerToolGateDeny 验证 P1-D：deny 规则下工具不执行且模型收到可见拒绝原因。
func TestRunnerToolGateDeny(t *testing.T) {
	blocked := newTrackingTool("blocked", tool.RiskReadOnly)
	ok := newTrackingTool("okay", tool.RiskReadOnly)
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "c1", Name: "blocked", Arguments: json.RawMessage(`{}`)}},
			{ToolCall: &llm.NormalizedToolCall{ID: "c2", Name: "okay", Arguments: json.RawMessage(`{}`)}},
		},
		{{Delta: llm.Message{Role: llm.RoleAssistant, Content: "done"}}},
	}}
	r := newRunnerWithTools(t, p, blocked, ok).
		WithToolGate(tool.NewGate(tool.SessionModeYolo).Deny("blocked"), nil)

	res := r.RunMessages(context.Background(), "RUN_G", "SESSION_G", "MSG_G", "mock", []*llm.Message{llm.UserMessage("go")})
	require.NoError(t, res.Err)
	assert.Equal(t, 0, blocked.calls(), "deny 工具不得执行")
	assert.Equal(t, 1, ok.calls())
	// Refused 语义：拒绝回执为结构化 JSON（refused=true + reason），模型据此换路自愈
	assert.Contains(t, joinContents(p.lastMessages()), `"refused":true`)
	assert.Contains(t, joinContents(p.lastMessages()), `"reason":"denied by policy"`)
	assert.Contains(t, joinContents(p.lastMessages()), `"tool":"blocked"`)
}

// TestRunnerToolGateAskApprover 验证 P1-D：ask 委托人工审批，拒绝则不执行。
func TestRunnerToolGateAskApprover(t *testing.T) {
	needAsk := newTrackingTool("ask_me", tool.RiskWriteLocal)
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{{ToolCall: &llm.NormalizedToolCall{ID: "c1", Name: "ask_me", Arguments: json.RawMessage(`{}`)}}},
		{{Delta: llm.Message{Role: llm.RoleAssistant, Content: "done"}}},
	}}
	var asked string
	r := newRunnerWithTools(t, p, needAsk).
		WithToolGate(tool.NewGate(tool.SessionModeDefault), func(_ context.Context, desc, _ string) bool {
			asked = desc
			return false
		})

	res := r.RunMessages(context.Background(), "RUN_G", "SESSION_G", "MSG_G", "mock", []*llm.Message{llm.UserMessage("go")})
	require.NoError(t, res.Err)
	assert.Equal(t, 0, needAsk.calls(), "审批拒绝不得执行")
	assert.Contains(t, asked, "ask_me", "approver 应收到完整工具描述")
	// Refused 语义：用户拒绝 → 结构化回执（refused=true + reason=denied by user）
	assert.Contains(t, joinContents(p.lastMessages()), `"refused":true`)
	assert.Contains(t, joinContents(p.lastMessages()), `"reason":"denied by user"`)
}

// lastMessages 返回最后一次请求的消息（runner 结束后即最终上下文）。
func (s *scriptedProvider) lastMessages() []*llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastReq == nil {
		return nil
	}
	return s.lastReq.Messages
}

func joinContents(msgs []*llm.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m != nil {
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

type riskyTool struct{}

func (riskyTool) Name() string              { return "risky" }
func (riskyTool) Description() string       { return "write tool" }
func (riskyTool) RiskLevel() tool.RiskLevel { return tool.RiskWriteLocal }
func (riskyTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "risky", Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (riskyTool) Execute(_ context.Context, _ json.RawMessage) tool.ToolResult {
	return tool.ToolResult{Content: "should not run"}
}

func refusedTestRunner(p *scriptedProvider, sink *recordingSink, approver func(context.Context, string, string) bool) *Runner {
	reg := tool.NewRegistry()
	_ = reg.Register(riskyTool{})
	return NewRunner(p, sink, DefaultConfig()).
		WithTools(reg, nil).
		WithToolGate(tool.NewGate(tool.SessionModeDefault), approver)
}

// hiddenTool 已注册但未暴露给 defs 的工具（执行侧收紧测试）。
type hiddenTool struct{}

func (hiddenTool) Name() string              { return "secret" }
func (hiddenTool) Description() string       { return "registered but not exposed" }
func (hiddenTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }
func (hiddenTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "secret", Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (hiddenTool) Execute(_ context.Context, _ json.RawMessage) tool.ToolResult {
	return tool.ToolResult{Content: "should not run"}
}

// TestRunnerApprovalRefusedDenied 用户拒绝 ask 审批：run 正常 end_turn、事件 refused=true、模型续跑第二轮。
func TestRunnerApprovalRefusedDenied(t *testing.T) {
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "call_1", Name: "risky", Arguments: json.RawMessage(`{}`)}},
			{FinishReason: stringPtr("tool_calls")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "好的，我换一种方式"}},
		},
	}}
	sink := &recordingSink{}
	denied := false
	r := refusedTestRunner(p, sink, func(_ context.Context, _ string, _ string) bool {
		denied = true
		return false // 用户拒绝
	})

	res := r.RunMessages(context.Background(), "RUN_R", "SESSION_R", "MSG_R", "mock", nil)

	if res.Err != nil {
		t.Fatalf("refused run should not fail: %v", res.Err)
	}
	if res.Reason != ReasonEndTurn {
		t.Fatalf("want end_turn, got %v", res.Reason)
	}
	if !denied {
		t.Fatalf("approver should have been consulted")
	}
	refusedSeen := false
	for _, e := range sink.events {
		if e.Kind != EventToolResult {
			continue
		}
		if tp, ok := e.Payload.(ToolResultPayload); ok && tp.Refused {
			refusedSeen = true
			if !strings.Contains(tp.Content, `"refused":true`) {
				t.Fatalf("refused content should carry structured refusal, got %q", tp.Content)
			}
		}
	}
	if !refusedSeen {
		t.Fatalf("expected refused tool result event")
	}
	if p.idx != 2 {
		t.Fatalf("model should continue after refusal, provider calls = %d", p.idx)
	}
}

type capturingProvider struct {
	inner *scriptedProvider
	mu    sync.Mutex
	reqs  [][]*llm.Message
}

func (c *capturingProvider) Name() string           { return c.inner.Name() }
func (c *capturingProvider) Kind() llm.ProviderKind { return c.inner.Kind() }
func (c *capturingProvider) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return c.inner.Chat(ctx, req)
}
func (c *capturingProvider) Models(ctx context.Context) ([]llm.ModelInfo, error) {
	return c.inner.Models(ctx)
}
func (c *capturingProvider) Ping(ctx context.Context) error { return c.inner.Ping(ctx) }

func (c *capturingProvider) Stream(ctx context.Context, req *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	c.mu.Lock()
	c.reqs = append(c.reqs, req.Messages)
	c.mu.Unlock()
	return c.inner.Stream(ctx, req)
}

func (c *capturingProvider) requestCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reqs)
}

// hasContent 指定轮次的请求里是否含某条消息正文。
func (c *capturingProvider) hasContent(turn int, want string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if turn >= len(c.reqs) {
		return false
	}
	for _, m := range c.reqs[turn] {
		if m.Content == want {
			return true
		}
	}
	return false
}

func newEchoRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	if err := reg.Register(echoTool{}); err != nil {
		t.Fatal(err)
	}
	return reg
}

// TestRunnerSteeringInjection steering 缝：本轮工具执行后插入的用户消息应出现在下一轮请求上下文中。
func TestRunnerSteeringInjection(t *testing.T) {
	p := &capturingProvider{inner: &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "我先查一下"}},
			{ToolCall: &llm.NormalizedToolCall{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"msg":"hello"}`)}},
			{FinishReason: stringPtr("tool_calls")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "改好了"}},
			{FinishReason: stringPtr("stop")},
		},
	}}}
	const steerText = "别查了，直接用 Go 写"
	calls := 0
	r := NewRunner(p, &recordingSink{}, DefaultConfig()).
		WithTools(newEchoRegistry(t), []llm.ToolDefinition{{Name: "echo", Description: "echo", Parameters: map[string]any{"type": "object"}}}).
		WithSteering(func(context.Context) []*llm.Message {
			calls++
			if calls > 1 {
				return nil
			}
			return []*llm.Message{llm.UserMessage(steerText)}
		})

	res := r.RunMessages(context.Background(), "RUN_STEER", "SESSION_STEER", "MSG_STEER", "mock", nil)
	if res.Err != nil {
		t.Fatalf("run failed: %v", res.Err)
	}
	// content 跨轮累加（与 follow-up 用例一致）：取末轮增量判断
	if !strings.HasSuffix(res.Content, "改好了") {
		t.Fatalf("want last turn content '改好了', got %q", res.Content)
	}
	if got := p.requestCount(); got != 2 {
		t.Fatalf("want 2 llm calls, got %d", got)
	}
	if !p.hasContent(1, steerText) {
		t.Fatalf("steering message missing in second turn context")
	}
	if p.hasContent(0, steerText) {
		t.Fatalf("steering message must not leak into the first turn")
	}
}

// TestRunnerFollowUpContinues follow-up 缝：模型说完（本轮无工具调用）后仍有排队输入 → run 自动续接下一波。
func TestRunnerFollowUpContinues(t *testing.T) {
	p := &capturingProvider{inner: &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "第一件事做完了"}},
			{FinishReason: stringPtr("stop")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "第二件事也做完了"}},
			{FinishReason: stringPtr("stop")},
		},
	}}}
	const followText = "还有第二件事"
	calls := 0
	r := NewRunner(p, &recordingSink{}, DefaultConfig()).
		WithFollowUp(func(context.Context) []*llm.Message {
			calls++
			if calls > 1 {
				return nil
			}
			return []*llm.Message{llm.UserMessage(followText)}
		})

	res := r.RunMessages(context.Background(), "RUN_FOLLOW", "SESSION_FOLLOW", "MSG_FOLLOW", "mock", nil)
	if res.Err != nil {
		t.Fatalf("run failed: %v", res.Err)
	}
	if got := p.requestCount(); got != 2 {
		t.Fatalf("want 2 llm calls (run resumed by follow-up), got %d", got)
	}
	if !p.hasContent(1, followText) {
		t.Fatalf("follow-up message missing in resumed turn context")
	}
	if res.Content != "第一件事做完了第二件事也做完了" {
		t.Fatalf("content should accumulate across resumed turns, got %q", res.Content)
	}
	if res.Reason != ReasonEndTurn {
		t.Fatalf("want ReasonEndTurn, got %v", res.Reason)
	}
	if calls != 2 {
		t.Fatalf("follow-up should be consulted until empty, got %d calls", calls)
	}
}

type countingTool struct {
	mu    sync.Mutex
	calls int
}

func (t *countingTool) Name() string              { return "count" }
func (t *countingTool) Description() string       { return "counting tool" }
func (t *countingTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }
func (t *countingTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "count", Parameters: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`)}
}
func (t *countingTool) Execute(_ context.Context, _ json.RawMessage) tool.ToolResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	return tool.ToolResult{Content: "ok"}
}
func (t *countingTool) executed() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func newCountingRegistry(t *testing.T, ct *countingTool) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	if err := reg.Register(ct); err != nil {
		t.Fatal(err)
	}
	return reg
}

func countDefs() []llm.ToolDefinition {
	return []llm.ToolDefinition{{Name: "count", Description: "count", Parameters: map[string]any{"type": "object"}}}
}

// TestRunnerTruncatedAnthropicStopReason Anthropic 的 max_tokens 同样触发重发语义。
func TestRunnerTruncatedAnthropicStopReason(t *testing.T) {
	ct := &countingTool{}
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "c1", Name: "count", Arguments: json.RawMessage(`{"msg":"hel`)}},
			{FinishReason: stringPtr("max_tokens")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "ok"}},
			{FinishReason: stringPtr("stop")},
		},
	}}
	r := NewRunner(p, &recordingSink{}, DefaultConfig()).
		WithTools(newCountingRegistry(t, ct), countDefs())

	res := r.RunMessages(context.Background(), "RUN_TRUNC2", "SESSION_TRUNC2", "MSG_TRUNC2", "mock", nil)
	if res.Err != nil {
		t.Fatalf("run failed: %v", res.Err)
	}
	if got := ct.executed(); got != 0 {
		t.Fatalf("截断调用不应执行，got %d", got)
	}
	if got := p.idx; got != 2 {
		t.Fatalf("want 2 llm calls, got %d", got)
	}
}

// TestRunnerPathTrustDeny 信任闸门返回 false → Refused（不计失败熔断），模型换路续跑。
func TestRunnerPathTrustDeny(t *testing.T) {
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "t1", Name: "exec",
				Arguments: json.RawMessage(`{"command":"go","args":["version"]}`)}},
			{FinishReason: stringPtr("tool_calls")},
		},
		{{Delta: llm.Message{Role: llm.RoleAssistant, Content: "换路"}, FinishReason: stringPtr("end_turn")}},
	}}
	execTool := execScripted("should-not-run", nil)
	sink := &recordingSink{}
	r := trustRunner(p, sink, execTool, func(context.Context, string, json.RawMessage) (bool, string) {
		return false, "directory is not trusted"
	})
	res := r.RunMessages(context.Background(), "RUN_t", "SES_t", "M_t", "test", nil)
	if res.Reason == ReasonError && res.Err != nil {
		t.Fatalf("denied tool should not be hard-error; got %v %v", res.Reason, res.Err)
	}
	// 必须出现 refused=true 的 tool.result 事件
	foundRefused := false
	for _, ev := range sink.events {
		if ev.Kind != EventToolResult {
			continue
		}
		p, ok := ev.Payload.(ToolResultPayload)
		if !ok || p.Name != "exec" {
			continue
		}
		if p.Refused && (p.Content != "" || p.Err != "") {
			foundRefused = true
		}
	}
	if !foundRefused {
		t.Fatalf("expected refused=true tool result, events=%+v", sink.events)
	}
}

// execScripted 简单脚本工具（不真执行命令，输出给定字符串）。
func execScripted(output string, _ *string) tool.Tool {
	return scriptedExec{out: output}
}

type scriptedExec struct{ out string }

func (scriptedExec) Name() string              { return "exec" }
func (scriptedExec) Description() string       { return "scripted exec" }
func (scriptedExec) RiskLevel() tool.RiskLevel { return tool.RiskExec }
func (scriptedExec) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "exec", Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (scriptedExec) Execute(_ context.Context, _ json.RawMessage) tool.ToolResult {
	return tool.ToolResult{Content: "ok"}
}

// trustRunner 拼一个最小可跑 Runner，注入 PathTrust。
func trustRunner(p llm.Provider, sink Sink, t tool.Tool, trust PathTrust) *Runner {
	reg := tool.NewRegistry()
	_ = reg.Register(t)
	return NewRunner(p, sink, DefaultConfig()).
		WithTools(reg, nil).
		WithPathTrust(trust)
}

type modelAwareProvider struct {
	mu        sync.Mutex
	broken    map[string]bool
	chunks    []llm.StreamChunk // 正常模型的返回（每次调用重建 channel）
	toolTurns bool              // true = 每轮返回一个参数不同的工具调用（驱动多轮）
	models    []string          // 逐次记录请求的模型
}

func (m *modelAwareProvider) Name() string           { return "model-aware" }
func (m *modelAwareProvider) Kind() llm.ProviderKind { return "openai" }
func (m *modelAwareProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}
func (m *modelAwareProvider) Models(_ context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (m *modelAwareProvider) Ping(_ context.Context) error                      { return nil }

func (m *modelAwareProvider) Stream(_ context.Context, req *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.models = append(m.models, req.Model)
	if m.broken[req.Model] {
		return nil, errMockLLM
	}
	if m.toolTurns {
		// 每轮一个参数不同的工具调用（避免同名同参触发停滞熔断）
		n := len(m.models)
		ch := make(chan llm.StreamChunk, 2)
		ch <- llm.StreamChunk{ToolCall: &llm.NormalizedToolCall{ID: fmt.Sprintf("c%d", n), Name: "count", Arguments: []byte(fmt.Sprintf(`{"msg":"t%d"}`, n))}}
		ch <- llm.StreamChunk{FinishReason: strPtr("tool_calls")}
		close(ch)
		return ch, nil
	}
	ch := make(chan llm.StreamChunk, len(m.chunks)+1)
	for _, c := range m.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (m *modelAwareProvider) requested() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.models...)
}

func strPtr(s string) *string { return &s }

// TestTurnAdjusterDowngradesOnStreamError 主模型建流失败 → 切到备用模型重试本轮并正常收尾。
func TestTurnAdjusterDowngradesOnStreamError(t *testing.T) {
	p := &modelAwareProvider{
		broken: map[string]bool{"primary": true},
		chunks: []llm.StreamChunk{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "备用模型接手"}},
			{FinishReason: strPtr("stop")},
		},
	}
	r := NewRunner(p, &recordingSink{}, DefaultConfig()).
		WithTurnAdjuster(func(_ context.Context, sig *TurnSignal) TurnUpdate {
			if sig != nil && sig.StreamErr != nil {
				return TurnUpdate{Model: strPtr("fallback")}
			}
			return TurnUpdate{}
		})

	res := r.RunMessages(context.Background(), "RUN_ADJ", "SESSION_ADJ", "MSG_ADJ", "primary", nil)
	if res.Err != nil {
		t.Fatalf("run failed after downgrade: %v", res.Err)
	}
	if !strings.HasSuffix(res.Content, "备用模型接手") {
		t.Fatalf("want fallback model output, got %q", res.Content)
	}
	got := p.requested()
	if len(got) != 2 || got[0] != "primary" || got[1] != "fallback" {
		t.Fatalf("want [primary fallback], got %v", got)
	}
}

// ===== 统一护栏链：策略门用 per-call 风险裁决，审批只问一次 =====

// classifiedTool 带 tool.RiskClassifier 的最小工具：按参数返回 per-call 风险。
type classifiedTool struct{}

func (classifiedTool) Name() string              { return "guarded" }
func (classifiedTool) Description() string       { return "guard test tool" }
func (classifiedTool) RiskLevel() tool.RiskLevel { return tool.RiskExec }

func (classifiedTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "guarded", Parameters: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`)}
}

func (classifiedTool) Execute(_ context.Context, _ json.RawMessage) tool.ToolResult {
	return tool.ToolResult{Content: "ok"}
}

// ClassifyArgs 白名单内命令 → (cmd, "")；其它 → (cmd, needs_approval)；坏 JSON → ("", "")。
func (classifiedTool) ClassifyArgs(args json.RawMessage) (string, string) {
	var req struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return "", ""
	}
	if req.Command == "" {
		return "", ""
	}
	if req.Command == "git status" {
		return req.Command, ""
	}
	return req.Command, tool.RiskApprovalNeeds
}

// countingApprover 记录每次审批的描述与风险。
type countingApprover struct {
	calls int
	desc  string
	risk  string
	ok    bool
}

func (c *countingApprover) Approve(_ context.Context, command, risk string) bool {
	c.calls++
	c.desc, c.risk = command, risk
	return c.ok
}

func guardRunner(mode tool.SessionMode, approver *countingApprover) *Runner {
	r := NewRunner(&scriptedProvider{}, &recordingSink{}, DefaultConfig())
	return r.WithToolGate(tool.NewGate(mode), approver.Approve)
}

func guardCall(command string) llm.NormalizedToolCall {
	return llm.NormalizedToolCall{
		ID: "c1", Name: "guarded",
		Arguments: json.RawMessage(`{"command":"` + command + `"}`),
	}
}

// TestGateSafeCommandSkipsApproval 白名单安全命令（per-call risk 空）免审放行。
func TestGateSafeCommandSkipsApproval(t *testing.T) {
	approver := &countingApprover{ok: true}
	r := guardRunner(tool.SessionModeDefault, approver)

	msg := r.gateTool(context.Background(), "RUN_G", "SES_G", 0, guardCall("git status"), classifiedTool{})
	require.Nil(t, msg, "安全命令应放行")
	assert.Zero(t, approver.calls, "安全命令不应触发审批")
}

// TestGateAskUsesPerCallRisk 危险命令：审批描述为具体命令、只问一次；拒绝回结构化回执。
func TestGateAskUsesPerCallRisk(t *testing.T) {
	approver := &countingApprover{ok: true}
	r := guardRunner(tool.SessionModeDefault, approver)

	msg := r.gateTool(context.Background(), "RUN_G", "SES_G", 0, guardCall("rm -rf /"), classifiedTool{})
	require.Nil(t, msg, "批准后放行")
	assert.Equal(t, 1, approver.calls, "单层闸门：只问一次")
	assert.Equal(t, "rm -rf /", approver.desc, "审批描述应为具体命令")
	assert.Equal(t, tool.RiskApprovalNeeds, approver.risk)

	denied := &countingApprover{ok: false}
	r2 := guardRunner(tool.SessionModeDefault, denied)
	require.NotNil(t, r2.gateTool(context.Background(), "RUN_G", "SES_G", 0, guardCall("rm -rf /"), classifiedTool{}),
		"拒绝应返回 tool 消息（Refused 语义）")
}

// TestGateYoloNeverAsks 完全访问模式：任何命令都不触发审批。
func TestGateYoloNeverAsks(t *testing.T) {
	approver := &countingApprover{ok: true}
	r := guardRunner(tool.SessionModeYolo, approver)

	msg := r.gateTool(context.Background(), "RUN_G", "SES_G", 0, guardCall("anything"), classifiedTool{})
	require.Nil(t, msg)
	assert.Zero(t, approver.calls)
}

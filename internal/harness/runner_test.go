package harness

// ReAct runner 长链路测试：主循环 / 工具执行 / 闸门审批 / 注入缝 / 护栏 / 会话生命周期。
// 场景是私有函数（testXxx），由文件末尾按能力域划分的父测试以 t.Run 聚合；
// 跑一个父测试即可定位整类行为，共享夹具见 runner_helpers_test.go。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/tool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===== 核心循环 =====

// TestRunnerThinkingSeparate 推理文本走 EventTurnThinking，不混入 Content。
func testRunnerThinkingSeparate(t *testing.T) {
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
func testRunnerReactToolCall(t *testing.T) {
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
func testRunnerStagnation(t *testing.T) {
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
func testRunnerTruncatedToolCallRetry(t *testing.T) {
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
func testRunnerSameArgsStagnation(t *testing.T) {
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
func testRunnerShouldStopAfterTurn(t *testing.T) {
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
func testRunnerReadonlyParallel(t *testing.T) {
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
func testRunnerToolPanicRecovered(t *testing.T) {
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

// TestRunnerToolGateDeny deny 规则下工具不执行，且模型收到可见的拒绝原因。
func testRunnerToolGateDeny(t *testing.T) {
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
func testRunnerApprovalRefusedDenied(t *testing.T) {
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

// ===== 注入缝 =====

// TestRunnerSteeringInjection steering 缝：本轮工具执行后插入的用户消息应出现在下一轮请求上下文中。
func testRunnerSteeringInjection(t *testing.T) {
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
func testRunnerFollowUpContinues(t *testing.T) {
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

// ===== 闸门 / 审批 =====

// TestRunnerPathTrustDeny 信任闸门返回 false → Refused（不计失败熔断），模型换路续跑。
func testRunnerPathTrustDeny(t *testing.T) {
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
func testTurnAdjusterDowngradesOnStreamError(t *testing.T) {
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

// guardLayer 单独驱动策略门这一层：守卫已收敛为可组合 layer，测试按层定位即可，
// 不必为了测一道闸门而把整条 run 跑起来。
func guardLayer(r *Runner, ctx context.Context, runID, sessionID string, turn int, call llm.NormalizedToolCall, t tool.Tool) *llm.Message {
	return r.layerPolicyGate(&toolCallCtx{ctx: ctx, runID: runID, sessionID: sessionID, turn: turn, call: call, tool: t})
}

func guardCall(command string) llm.NormalizedToolCall {
	return llm.NormalizedToolCall{
		ID: "c1", Name: "guarded",
		Arguments: json.RawMessage(`{"command":"` + command + `"}`),
	}
}

// TestGateAskUsesPerCallRisk 危险命令：审批描述为具体命令、只问一次；拒绝回结构化回执。
func testGateAskUsesPerCallRisk(t *testing.T) {
	approver := &countingApprover{ok: true}
	r := guardRunner(tool.SessionModeDefault, approver)

	msg := guardLayer(r, context.Background(), "RUN_G", "SES_G", 0, guardCall("rm -rf /"), classifiedTool{})
	require.Nil(t, msg, "批准后放行")
	assert.Equal(t, 1, approver.calls, "单层闸门：只问一次")
	assert.Equal(t, "rm -rf /", approver.desc, "审批描述应为具体命令")
	assert.Equal(t, tool.RiskApprovalNeeds, approver.risk)

	denied := &countingApprover{ok: false}
	r2 := guardRunner(tool.SessionModeDefault, denied)
	require.NotNil(t, guardLayer(r2, context.Background(), "RUN_G", "SES_G", 0, guardCall("rm -rf /"), classifiedTool{}),
		"拒绝应返回 tool 消息（Refused 语义）")
}

// TestGateYoloNeverAsks 完全访问模式：任何命令都不触发审批。
func testGateYoloNeverAsks(t *testing.T) {
	approver := &countingApprover{ok: true}
	r := guardRunner(tool.SessionModeYolo, approver)

	msg := guardLayer(r, context.Background(), "RUN_G", "SES_G", 0, guardCall("anything"), classifiedTool{})
	require.Nil(t, msg)
	assert.Zero(t, approver.calls)
}

// TestGateAllowKeepsCommandLevelCheck 显式放行（Allow）不等于关闭命令级裁决。
//
// 只认 Allow 就无脑放行会让 exec 的白名单与危险正则整体失效——工具内部又因
// GuardChainActive 跳过自有审批，两头都放开等于裸执行。
func testGateAllowKeepsCommandLevelCheck(t *testing.T) {
	cases := []struct {
		name      string
		tool      tool.Tool
		args      string
		wantCalls int
	}{
		{"白名单内命令免审", classifiedTool{}, `{"command":"git status"}`, 0},
		{"白名单外命令仍需确认", classifiedTool{}, `{"command":"curl http://x"}`, 1},
		{"无命令级风险的工具直接放行", newTrackingTool("writer", tool.RiskWriteLocal), `{}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			approver := &countingApprover{ok: true}
			r := NewRunner(&scriptedProvider{}, &recordingSink{}, DefaultConfig()).
				WithToolGate(tool.NewGate(tool.SessionModeDefault).Allow(tc.tool.Name()), approver.Approve)

			call := llm.NormalizedToolCall{ID: "c1", Name: tc.tool.Name(), Arguments: json.RawMessage(tc.args)}
			msg := guardLayer(r, context.Background(), "RUN_A", "SES_A", 0, call, tc.tool)
			require.Nil(t, msg, "批准后应放行")
			assert.Equal(t, tc.wantCalls, approver.calls)
		})
	}
}

// ===== 子 Agent 委派 / 断点续跑 / 注入防护 =====

// delegateSink 记录事件，校验 delegate 路径所需字段。
type delegateSink struct {
	mu     sync.Mutex
	events []Event
}

func (r *delegateSink) Emit(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *delegateSink) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.events))
	copy(out, r.events)
	return out
}

// queueProvider 按调用序号返回不同响应，并记录最后一次请求的消息列表。
type queueProvider struct {
	mu      sync.Mutex
	queue   [][]llm.StreamChunk
	calls   int
	lastReq *llm.ChatRequest
}

func (s *queueProvider) Name() string           { return "scripted" }
func (s *queueProvider) Kind() llm.ProviderKind { return "openai" }
func (s *queueProvider) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}
func (s *queueProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (s *queueProvider) Ping(context.Context) error                      { return nil }
func (s *queueProvider) Stream(_ context.Context, req *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastReq = req
	var chunks []llm.StreamChunk
	if s.calls < len(s.queue) {
		chunks = s.queue[s.calls]
		s.calls++
	}
	ch := make(chan llm.StreamChunk, len(chunks)+1)
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (s *queueProvider) requestMessages() []*llm.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastReq == nil {
		return nil
	}
	return s.lastReq.Messages
}

// TestDelegateContextIsolationOnly 子 Agent 拿到的消息列表只有「人设 + 任务」，看不到父历史。
func testDelegateContextIsolationOnly(t *testing.T) {
	p := &queueProvider{queue: [][]llm.StreamChunk{
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "子任务结果"}},
			{FinishReason: stringPtr("stop")},
		},
	}}
	sink := &delegateSink{}
	r := NewRunner(p, sink, DefaultConfig()).WithTools(tool.NewRegistry(), nil)

	summary, err := r.Delegate(context.Background(), "coding", "独立任务描述")
	require.NoError(t, err)
	assert.Contains(t, summary, "子任务结果")

	got := p.requestMessages()
	require.Len(t, got, 2, "子 run 只能拿到 persona + task，看不到父历史")
	assert.Equal(t, llm.RoleSystem, got[0].Role, "首条必须是子 Agent persona（system）")
	assert.Contains(t, got[0].Content, "工程助手", "coding agent persona 应作为 system 注入")
	assert.Equal(t, llm.RoleUser, got[1].Role)
	assert.Equal(t, "独立任务描述", got[1].Content)
}

// testDelegateInheritsGuardHooks 子 Agent 与父 run 受同一套护栏约束。
//
// 子 run 由 Delegate 新建；不继承 ToolGate / 目录信任时会退化为「无策略门」，
// exec / run_skill_script 这类靠策略门裁决的工具将不再被询问。
func testDelegateInheritsGuardHooks(t *testing.T) {
	probe := &guardProbeTool{}
	reg := tool.NewRegistry()
	require.NoError(t, reg.Register(probe))
	defs := []llm.ToolDefinition{{Name: "probe", Description: "probe", Parameters: map[string]any{"type": "object"}}}

	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "d1", Name: "probe", Arguments: json.RawMessage(`{}`)}},
			{FinishReason: stringPtr("tool_calls")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "子任务完成"}},
			{FinishReason: stringPtr("stop")},
		},
	}}
	parent := NewRunner(p, &recordingSink{}, DefaultConfig()).
		WithTools(reg, defs).
		WithToolGate(tool.NewGate(tool.SessionModeYolo), nil)

	summary, err := parent.Delegate(context.Background(), "writer", "跑一下 probe")
	require.NoError(t, err)
	assert.Contains(t, summary, "子任务完成")
	assert.True(t, probe.guarded, "子 Agent 必须继承父 run 的护栏链（策略门裁决通过后标记）")
}

// TestRunnerResume 两轮工具调用落检查点 → 新 Runner Resume 续跑拿到终答。
func testRunnerResume(t *testing.T) {
	dir := t.TempDir()
	reg := tool.NewRegistry()
	_ = reg.Register(echoTool{})

	// run1：两轮都调 echo 工具（每轮结束落检查点）
	p1 := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"msg":"hello"}`)}},
			{FinishReason: stringPtr("tool_calls")},
		},
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "call_2", Name: "echo", Arguments: json.RawMessage(`{"msg":"world"}`)}},
			{FinishReason: stringPtr("tool_calls")},
		},
	}}
	r1 := NewRunner(p1, &recordingSink{}, DefaultConfig()).
		WithTools(reg, []llm.ToolDefinition{{Name: "echo", Description: "echo", Parameters: map[string]any{"type": "object"}}}).
		WithCheckpointDir(dir)
	res1 := r1.RunMessages(context.Background(), "RUN_CP", "SESSION_CP", "MSG_CP", "mock", []*llm.Message{llm.UserMessage("do it")})
	if res1.Err != nil {
		t.Fatalf("run1 failed: %v", res1.Err)
	}

	if _, err := os.Stat(filepath.Join(dir, "SESSION_CP", "RUN_CP.jsonl")); err != nil {
		t.Fatalf("checkpoint file missing: %v", err)
	}
	cp, err := NewCheckpointStore(dir).LoadLast("SESSION_CP", "RUN_CP")
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if cp.Turn != 1 || len(cp.Messages) != 5 {
		t.Fatalf("want turn=1 & 5 msgs (user, asst, tool, asst, tool), got turn=%d msgs=%d", cp.Turn, len(cp.Messages))
	}

	// run2：Resume，provider 只剩终答
	p2 := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
			{FinishReason: stringPtr("stop")},
			{FinalUsage: &llm.TokenUsage{InputTokens: 100, OutputTokens: 5, TotalTokens: 105}},
		},
	}}
	r2 := NewRunner(p2, &recordingSink{}, DefaultConfig()).
		WithTools(reg, []llm.ToolDefinition{{Name: "echo", Description: "echo", Parameters: map[string]any{"type": "object"}}}).
		WithCheckpointDir(dir)
	res2 := r2.Resume(context.Background(), "RUN_CP", "SESSION_CP", "MSG_CP", "mock")
	if res2.Err != nil {
		t.Fatalf("resume failed: %v", res2.Err)
	}
	if res2.Content != "done" {
		t.Fatalf("want final content 'done', got %q", res2.Content)
	}
	if res2.Usage.TotalTokens < 100 {
		t.Fatalf("usage not accumulated across resume, got %+v", res2.Usage)
	}
}

// TestHasNestedToolCallMarker 注入防护护栏：伪调用形态命中、正常参数不误伤。
func testHasNestedToolCallMarker(t *testing.T) {
	positive := []string{
		`<tool_call name="exec">{}</tool_call>`,
		`run this: </tool_call>`,
		`{"tool_calls":[{"name":"exec"}]}`,
		`<TOOLCALL>x</TOOLCALL>`,
	}
	for _, s := range positive {
		assert.True(t, HasNestedToolCallMarker(s), s)
	}
	negative := []string{
		`{"command":"git status"}`,
		`C:\Users\LIKX\Desktop\skills`,
		`把文件复制到 D:\docs 目录`,
		`search query: tool call runtime`,
	}
	for _, s := range negative {
		assert.False(t, HasNestedToolCallMarker(s), s)
	}
}

// ===== 聚合入口 =====
//
// 具体场景实现为上面的私有函数（不被 go test 直接发现），由下列 6 个按能力域
// 划分的父测试以 t.Run 聚合。改某个能力时只跑对应的父测试即可定位，
// 不必在 20+ 个顶层用例里翻找。

// TestRunnerReActLoop ReAct 主循环：思考分离 / 多轮工具 / 停滞熔断 / 截断重发 / 轮次终止。
func TestRunnerReActLoop(t *testing.T) {
	t.Run("thinking_separate", testRunnerThinkingSeparate)
	t.Run("tool_call_rounds", testRunnerReactToolCall)
	t.Run("stagnation_on_failures", testRunnerStagnation)
	t.Run("truncated_retry", testRunnerTruncatedToolCallRetry)
	t.Run("same_args_stagnation", testRunnerSameArgsStagnation)
	t.Run("stop_after_turn", testRunnerShouldStopAfterTurn)
}

// TestRunnerToolExecution 工具执行：只读并发 / panic 隔离。
func TestRunnerToolExecution(t *testing.T) {
	t.Run("readonly_parallel", testRunnerReadonlyParallel)
	t.Run("panic_recovered", testRunnerToolPanicRecovered)
	t.Run("timing_attribution", testRunnerTimingAttribution)
}

// testRunnerTimingAttribution 分段耗时归因：跑工具的时间算进 ToolsMs，不被等模型吞并。
//
// 归因的意义是回答「这次 run 慢在哪」：工具全都被算进 LLM 耗时的话，
// 长工具链会被误判成模型慢，优化方向直接跑偏。
func testRunnerTimingAttribution(t *testing.T) {
	reg := tool.NewRegistry()
	require.NoError(t, reg.Register(slowTool{}))
	defs := []llm.ToolDefinition{{Name: "slow", Description: "slow", Parameters: map[string]any{"type": "object"}}}

	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{ToolCall: &llm.NormalizedToolCall{ID: "s1", Name: "slow", Arguments: json.RawMessage(`{}`)}},
			{FinishReason: stringPtr("tool_calls")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
			{FinishReason: stringPtr("stop")},
		},
	}}
	cfg := DefaultConfig()
	cfg.ToolCallTimeout = 80_000_000 // 80ms
	res := NewRunner(p, &recordingSink{}, cfg).WithTools(reg, defs).
		RunMessages(context.Background(), "RUN_TM", "SES_TM", "MSG_TM", "mock", []*llm.Message{llm.UserMessage("go")})

	require.NoError(t, res.Err)
	assert.GreaterOrEqual(t, res.Timings.ToolsMs, int64(50), "工具等待应计入工具耗时")
	assert.GreaterOrEqual(t, res.Timings.LLMMs, int64(0))
}


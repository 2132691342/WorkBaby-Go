package harness

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// delegateSink 记录事件，校验 delegate 路径所需字段（Kind / ParentRunID / Agent）。
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

func newDelegateEcho() tool.Tool {
	return delegateEchoTool{}
}

func newDelegateFail() tool.Tool {
	return delegateFailTool{}
}

type delegateEchoTool struct{}

func (delegateEchoTool) Name() string              { return "echo" }
func (delegateEchoTool) Description() string       { return "echo tool" }
func (delegateEchoTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }
func (delegateEchoTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "echo", Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (delegateEchoTool) Execute(context.Context, json.RawMessage) tool.ToolResult {
	return tool.ToolResult{Content: "ok"}
}

type delegateFailTool struct{}

func (delegateFailTool) Name() string              { return "fail" }
func (delegateFailTool) Description() string       { return "always fail" }
func (delegateFailTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }
func (delegateFailTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "fail", Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (delegateFailTool) Execute(context.Context, json.RawMessage) tool.ToolResult {
	return tool.ToolResult{Err: assertErr{"boom"}}
}

type assertErr struct{ msg string }

func (e assertErr) Error() string { return e.msg }

// longProvider 返回长正文（>4000 rune）以验证截断；model 与父 run 一致即可。
type longProvider struct{}

func (longProvider) Name() string           { return "long" }
func (longProvider) Kind() llm.ProviderKind { return "openai" }
func (longProvider) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}
func (longProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (longProvider) Ping(context.Context) error                      { return nil }
func (longProvider) Stream(ctx context.Context, _ *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	body := strings.Repeat("子任务回答片段。", 1500) // 远超 4000 rune 阈值
	out := make(chan llm.StreamChunk, 2)
	go func() {
		defer close(out)
		select {
		case <-ctx.Done():
			return
		default:
		}
		out <- llm.StreamChunk{
			Delta:        llm.Message{Role: llm.RoleAssistant, Content: body},
			FinishReason: stringPtr("stop"),
		}
	}()
	return out, nil
}

// errProvider 触发 Run 失败路径。
type errProvider struct{}

func (errProvider) Name() string           { return "err" }
func (errProvider) Kind() llm.ProviderKind { return "openai" }
func (errProvider) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, assertErr{"llm fail"}
}
func (errProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (errProvider) Ping(context.Context) error                      { return nil }
func (errProvider) Stream(context.Context, *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, assertErr{"stream fail"}
}

// queueProvider 复用 runner_test.go 的 scriptedProvider 不可能（结构差异），
// 因此独立实现：按调用序号返回不同响应，验证子 run 看不到父历史的关键路径。
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
func TestDelegateContextIsolationOnly(t *testing.T) {
	p := &queueProvider{queue: [][]llm.StreamChunk{
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "子任务结果"}},
			{FinishReason: stringPtr("stop")},
		},
	}}
	sink := &delegateSink{}
	r := NewRunner(p, sink, DefaultConfig()).WithTools(tool.NewRegistry(), nil)

	parentHistory := []*llm.Message{
		llm.SystemMessage("父 persona"),
		llm.UserMessage("父 user 1"),
		llm.UserMessage("父 user 2"),
		llm.AssistantMessage("父 assistant", nil),
	}
	// parentHistory 仅用于语义说明——Delegate 不接受父历史参数，故无需传入。
	_ = parentHistory
	summary, err := r.Delegate(context.Background(), "coding", "独立任务描述")
	require.NoError(t, err)
	assert.Contains(t, summary, "子任务结果")

	got := p.requestMessages()
	require.Len(t, got, 2, "子 run 只能拿到 persona + task，看不到父历史 4 条消息")
	assert.Equal(t, llm.RoleSystem, got[0].Role, "首条必须是子 Agent persona（system）")
	assert.Contains(t, got[0].Content, "工程助手", "coding agent persona 应作为 system 注入")
	assert.Equal(t, llm.RoleUser, got[1].Role)
	assert.Equal(t, "独立任务描述", got[1].Content)
}




// TestDelegateBudgetCap 父未传超时、子 run 强约束 3 分钟（编译期常量）且轮次 ≤12。
// 我们只验证常量合理性，避免日后误改阈值。
// ===== helpers =====

// registerTools 简化注册；runner_test 用过类似工具，这里独立保留避免跨测试文件共享类型。
func registerTools(t *testing.T, ts ...tool.Tool) *tool.Registry {
	t.Helper()
	r := tool.NewRegistry()
	for _, x := range ts {
		require.NoError(t, r.Register(x))
	}
	return r
}

package harness

// runner_test.go 的共享测试夹具：mock LLM provider / tool / sink / 错误。
// 把 fixture 抽到这里是为了让 runner_test.go 只放测试函数本身，降低大文件阅读成本。

import (
	"context"
	"encoding/json"
	"sync"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/tool"
)

// mockProvider 单轮响应预设的 chunk 序列（一次性 emit，关闭流）。
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
// 记录最后一次请求，便于断言"模型见到的消息列表"。
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

// recordingSink 记录事件用于断言事件流。
type recordingSink struct {
	mu     sync.Mutex
	events []Event
}

func (r *recordingSink) Emit(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordingSink) kinds() []EventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
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

// 通用错误夹具。
type mockError struct{ msg string }

func (e mockError) Error() string { return e.msg }

var (
	errMockLLM  = mockError{"mock LLM error"}
	errMockFail = mockError{"boom"}
)

func stringPtr(s string) *string { return &s }

// countingApprover / classifiedTool 与具体测试方法签名紧耦合，留在 runner_test.go 同测试附近，
// 不在 helpers 集中（避免不同测试的 mock 互相干扰）。

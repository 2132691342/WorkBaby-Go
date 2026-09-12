package harness

// 失败路径收束：run 必须落在唯一终态；重试只允许发生在建流期（流中途重试会重复输出）。

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/tool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flakyProvider 前 failFirst 次建流返回瞬时错误，之后正常返回。
type flakyProvider struct {
	mu        sync.Mutex
	failFirst int
	calls     int
	chunks    []llm.StreamChunk
}

func (f *flakyProvider) Name() string           { return "flaky" }
func (f *flakyProvider) Kind() llm.ProviderKind { return "openai" }
func (f *flakyProvider) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}
func (f *flakyProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (f *flakyProvider) Ping(context.Context) error                      { return nil }

func (f *flakyProvider) Stream(_ context.Context, _ *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	f.mu.Lock()
	f.calls++
	fail := f.calls <= f.failFirst
	f.mu.Unlock()
	if fail {
		return nil, pkg.New(3003, "429 rate limited", "")
	}
	ch := make(chan llm.StreamChunk, len(f.chunks)+1)
	for _, c := range f.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (f *flakyProvider) streamCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// errStreamProvider 建流成功、流中途报错；统计建流次数以验证不重试。
type errStreamProvider struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (e *errStreamProvider) Name() string           { return "errstream" }
func (e *errStreamProvider) Kind() llm.ProviderKind { return "openai" }
func (e *errStreamProvider) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}
func (e *errStreamProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (e *errStreamProvider) Ping(context.Context) error                      { return nil }

func (e *errStreamProvider) Stream(_ context.Context, _ *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	ch := make(chan llm.StreamChunk, 2)
	ch <- llm.StreamChunk{Delta: llm.Message{Role: llm.RoleAssistant, Content: "半句"}}
	ch <- llm.StreamChunk{Err: e.err}
	close(ch)
	return ch, nil
}

func (e *errStreamProvider) streamCalls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// blockingProvider 建流后挂起直到 ctx 取消，用于验证取消能打断流式等待。
type blockingProvider struct {
	started chan struct{}
}

func (b *blockingProvider) Name() string           { return "blocking" }
func (b *blockingProvider) Kind() llm.ProviderKind { return "openai" }
func (b *blockingProvider) Chat(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}
func (b *blockingProvider) Models(context.Context) ([]llm.ModelInfo, error) { return nil, nil }
func (b *blockingProvider) Ping(context.Context) error                      { return nil }

func (b *blockingProvider) Stream(ctx context.Context, _ *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	return ch, nil
}

// TestRunnerResilience 建流瞬时错误退避重试 / 流中途错误不重试 / 取消打断流式等待。
func TestRunnerResilience(t *testing.T) {
	t.Run("transient_stream_error_retried", testResilienceTransientRetry)
	t.Run("mid_stream_error_not_retried", testResilienceMidStreamNoRetry)
	t.Run("cancel_interrupts_stream", testResilienceCancelInterrupts)
}

// testResilienceTransientRetry 建流期瞬时错误（429）：退避重试后 run 正常收尾，且发出 retry 事件。
func testResilienceTransientRetry(t *testing.T) {
	p := &flakyProvider{
		failFirst: 1,
		chunks: []llm.StreamChunk{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "恢复成功"}},
			{FinishReason: stringPtr("stop")},
			{FinalUsage: &llm.TokenUsage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14}},
		},
	}
	sink := &recordingSink{}
	res := NewRunner(p, sink, DefaultConfig()).
		RunMessages(context.Background(), "RUN_RT", "SESSION_RT", "MSG_RT", "mock", []*llm.Message{llm.UserMessage("hi")})

	require.NoError(t, res.Err)
	assert.Equal(t, ReasonEndTurn, res.Reason)
	assert.Equal(t, "恢复成功", res.Content)
	assert.Equal(t, 2, p.streamCalls(), "瞬时错误应重试一次后成功")
	assert.Contains(t, sink.kinds(), EventRetry, "重试必须对用户可见（前端展示退避提示）")
	assert.Contains(t, sink.kinds(), EventRunDone)
}

// testResilienceMidStreamNoRetry 流中途错误：已有增量推送给前端，原地重试会重复输出 → 不重试，折叠为终态。
func testResilienceMidStreamNoRetry(t *testing.T) {
	p := &errStreamProvider{err: errMockLLM}
	sink := &recordingSink{}
	res := NewRunner(p, sink, DefaultConfig()).
		RunMessages(context.Background(), "RUN_MS", "SESSION_MS", "MSG_MS", "mock", []*llm.Message{llm.UserMessage("hi")})

	assert.Equal(t, ReasonError, res.Reason)
	assert.Equal(t, 1, p.streamCalls(), "流中途错误禁止重试")
	kinds := sink.kinds()
	assert.NotContains(t, kinds, EventRetry)
	assert.Contains(t, kinds, EventError, "失败必须落成 error 事件，不能静默")
	assert.Contains(t, kinds, EventRunDone, "任何退出路径都要发终态（唯一出口）")
}

// testResilienceCancelInterrupts 取消：卡在流式等待的 run 必须能被 ctx 取消并收束为 cancelled。
func testResilienceCancelInterrupts(t *testing.T) {
	p := &blockingProvider{started: make(chan struct{})}
	sink := &recordingSink{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan RunResult, 1)
	go func() {
		done <- NewRunner(p, sink, DefaultConfig()).
			RunMessages(ctx, "RUN_CX", "SESSION_CX", "MSG_CX", "mock", []*llm.Message{llm.UserMessage("hi")})
	}()

	select {
	case <-p.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider 未被调用")
	}
	cancel()

	var res RunResult
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("取消后 run 未收束（goroutine 泄漏或等待不可打断）")
	}
	assert.Equal(t, ReasonCancelled, res.Reason)
	assert.Contains(t, sink.kinds(), EventRunDone)
}

// slowTool 尊重 ctx 的慢工具：用于验证单工具超时被隔离，不拖垮整个 run。
type slowTool struct{}

func (slowTool) Name() string              { return "slow" }
func (slowTool) Description() string       { return "blocks until ctx done" }
func (slowTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }
func (slowTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "slow", Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (slowTool) Execute(ctx context.Context, _ json.RawMessage) tool.ToolResult {
	select {
	case <-ctx.Done():
		return tool.ToolResult{Err: ctx.Err()}
	case <-time.After(10 * time.Second):
		return tool.ToolResult{Content: "too late"}
	}
}

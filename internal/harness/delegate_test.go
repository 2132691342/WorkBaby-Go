package harness

import (
	"context"
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
func TestDelegateContextIsolationOnly(t *testing.T) {
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

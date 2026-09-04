package harness

import (
	"context"
	"testing"

	"WorkBaby/internal/llm"
)

// text_tool_calls_test.go 覆盖 正文形态的 tool call 兜底解析（仅认暴露的工具名）。

func echoDefs() []llm.ToolDefinition {
	return []llm.ToolDefinition{{Name: "echo", Description: "echo", Parameters: map[string]any{"type": "object"}}}
}

// TestParseTextToolCallsFenced ```json 代码块中的调用被解析。
func TestParseTextToolCallsFenced(t *testing.T) {
	content := "我先调用工具：\n```json\n{\"name\":\"echo\",\"arguments\":{\"msg\":\"hello\"}}\n```\n"
	calls := parseTextToolCalls(content, echoDefs())
	if len(calls) != 1 {
		t.Fatalf("want 1 text tool call, got %d", len(calls))
	}
	if calls[0].Name != "echo" || string(calls[0].Arguments) != `{"msg":"hello"}` {
		t.Fatalf("unexpected call: %+v %s", calls[0], calls[0].Arguments)
	}
}



// TestRunnerTextToolCallFallback 端到端：正文工具调用被兜底执行并回填结果。
func TestRunnerTextToolCallFallback(t *testing.T) {
	p := &scriptedProvider{calls: [][]llm.StreamChunk{
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "```json\n{\"name\":\"echo\",\"arguments\":{\"msg\":\"hi\"}}\n```"}},
			{FinishReason: strPtr("stop")},
		},
		{
			{Delta: llm.Message{Role: llm.RoleAssistant, Content: "结果是 echo:hi"}},
			{FinishReason: strPtr("stop")},
		},
	}}
	sink := &recordingSink{}
	r := NewRunner(p, sink, DefaultConfig()).
		WithTools(newEchoRegistry(t), echoDefs())

	res := r.RunMessages(context.Background(), "RUN_TEXT", "SESSION_TEXT", "MSG_TEXT", "mock", nil)
	if res.Err != nil {
		t.Fatalf("run failed: %v", res.Err)
	}
	if got := p.idx; got != 2 {
		t.Fatalf("text tool call should trigger a second turn, got %d calls", got)
	}
	hasToolCallEvent := false
	for _, e := range sink.events {
		if e.Kind == EventToolCall {
			hasToolCallEvent = true
		}
	}
	if !hasToolCallEvent {
		t.Fatalf("expected EventToolCall for text-encoded call")
	}
}

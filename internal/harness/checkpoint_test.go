package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/tool"
)



// TestRunnerResume 两轮工具调用落检查点 → 新 Runner Resume 续跑拿到终答。
func TestRunnerResume(t *testing.T) {
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

	// 检查点文件存在且记录到 turn=1
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


package nodes

import (
	"context"
	"testing"
)

// TestLLMNodeReactBranch 配了 tools 且注入了执行器 → 走 ReAct 分支（不再直连 provider）。
func TestLLMNodeReactBranch(t *testing.T) {
	var got ReactRequest
	n := (&LLMNode{}).WithReactor(func(_ context.Context, req ReactRequest) (ReactResult, error) {
		got = req
		return ReactResult{Text: "done", Turns: 3, StopReason: "completed"}, nil
	})
	out, err := n.Execute(context.Background(), nil, map[string]any{
		"providerID":         "p1",
		"model":              "m1",
		"systemPrompt":       "sys",
		"userPromptTemplate": "do it",
		"tools":              []any{"file_read", " file_write "},
		"maxTurns":           float64(8),
		"temperature":        float64(0.3),
	}, nil)
	if err != nil {
		t.Fatalf("react branch should not fail: %v", err)
	}
	if out["text"] != "done" || out["turns"] != 3 || out["stopReason"] != "completed" {
		t.Fatalf("unexpected output: %v", out)
	}
	if got.ProviderID != "p1" || got.Model != "m1" || got.SystemPrompt != "sys" || got.UserPrompt != "do it" {
		t.Fatalf("request not forwarded: %+v", got)
	}
	if len(got.Tools) != 2 || got.Tools[0] != "file_read" || got.Tools[1] != "file_write" {
		t.Fatalf("tools not parsed: %+v", got.Tools)
	}
	if got.MaxTurns != 8 || got.Temperature == nil || *got.Temperature != 0.3 {
		t.Fatalf("maxTurns/temperature not parsed: %+v", got)
	}
}

// TestLLMNodeCompletionsFallback 未配 tools → 走单次补全路径（需要 llm.Registry）。
func TestLLMNodeCompletionsFallback(t *testing.T) {
	entered := false
	n := (&LLMNode{}).WithReactor(func(_ context.Context, _ ReactRequest) (ReactResult, error) {
		entered = true
		return ReactResult{}, nil
	})
	// reg 为 nil → 补全路径报「未配置 llm.Registry」，证明没有走 ReAct
	_, err := n.Execute(context.Background(), nil, map[string]any{
		"providerID":         "p1",
		"model":              "m1",
		"userPromptTemplate": "hi",
	}, nil)
	if err == nil {
		t.Fatal("expected error from completion path (nil registry)")
	}
	if entered {
		t.Fatal("must not enter react branch when tools is empty")
	}
}

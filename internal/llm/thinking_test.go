package llm

import (
	"reflect"
	"testing"
)

// TestResolveThinkingStyle 显式指定优先于探测；无法识别的指定值回落探测。
func TestResolveThinkingStyle(t *testing.T) {
	if got := ResolveThinkingStyle("adaptive", "https://api.openai.com/v1", "gpt-4o"); got != ThinkingStyleAdaptive {
		t.Fatalf("explicit adaptive overridden: %q", got)
	}
	if got := ResolveThinkingStyle("", "https://api.minimaxi.com/v1", "MiniMax-M3"); got != ThinkingStyleAdaptive {
		t.Fatalf("auto detect failed: %q", got)
	}
	if got := ResolveThinkingStyle("garbage", "https://api.minimaxi.com/v1", "MiniMax-M3"); got != ThinkingStyleAdaptive {
		t.Fatalf("garbage should fall back to detect: %q", got)
	}
	// 未知上游一律 none：不认识的网关不能猜方言，否则下发私有字段即 400
	if got := DetectThinkingStyle("https://my-private-gateway.example.com/v1", "some-model"); got != ThinkingStyleNone {
		t.Fatalf("unknown upstream should default to none, got %q", got)
	}
}

// TestRenderThinking 关键回归：MiniMax 场景下绝不允许出现 thinking.type=enabled（本次线上 400 根因）。
func TestRenderThinking(t *testing.T) {
	on := &ThinkingConfig{Type: "enabled", BudgetTokens: 8192}
	off := &ThinkingConfig{Type: "disabled"}

	cases := []struct {
		name      string
		style     ThinkingStyle
		tc        *ThinkingConfig
		wantField string
		wantVal   any
	}{
		{"adaptive-on", ThinkingStyleAdaptive, on, "thinking", map[string]any{"type": "adaptive"}},
		{"adaptive-off", ThinkingStyleAdaptive, off, "thinking", map[string]any{"type": "disabled"}},
		{"enabled-on", ThinkingStyleEnabled, on, "thinking", map[string]any{"type": "enabled"}},
		{"effort-high", ThinkingStyleReasoningEffort, on, "reasoning_effort", "high"},
		{"qwen-on", ThinkingStyleEnableBool, on, "enable_thinking", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := RenderThinking(c.style, c.tc)
			got, ok := body[c.wantField]
			if !ok || !reflect.DeepEqual(got, c.wantVal) {
				t.Fatalf("RenderThinking(%s) = %v, want %s=%v", c.style, body, c.wantField, c.wantVal)
			}
		})
	}

	// 不支持思考 / 配置为 nil / none 方言 → 一律不下发任何字段
	for _, c := range []struct {
		style ThinkingStyle
		tc    *ThinkingConfig
	}{{ThinkingStyleNone, on}, {ThinkingStyleNone, off}, {ThinkingStyleReasoningEffort, off}, {ThinkingStyleAuto, on}, {"", nil}} {
		if body := RenderThinking(c.style, c.tc); body != nil {
			t.Fatalf("RenderThinking(%s) should emit nothing, got %v", c.style, body)
		}
	}
}

// TestUpstreamError 上游 4xx 报文解析：抽出人话 + 给出可操作建议，不再整段 JSON 直通 UI。
func TestUpstreamError(t *testing.T) {
	raw := []byte(`{"type":"error","error":{"type":"bad_request_error","message":"invalid params, invalid thinking.type: \"enabled\" (allowed: adaptive, disabled) (2013)","http_code":"400"}}`)
	e := ParseUpstreamError(400, raw)
	if e.Message != `invalid params, invalid thinking.type: "enabled" (allowed: adaptive, disabled) (2013)` {
		t.Fatalf("message = %q", e.Message)
	}
	hint := e.Hint()
	if hint == "" {
		t.Fatal("thinking 400 should carry an actionable hint")
	}

	appErr := NewUpstreamError("模型服务返回", 400, raw)
	if appErr.Message == "" || appErr.Details == "" {
		t.Fatalf("appErr = %+v", appErr)
	}
	// 原始 JSON 不得进入用户可见文案
	if len(appErr.Message) > 400 {
		t.Fatalf("message too long for UI: %d", len(appErr.Message))
	}
}

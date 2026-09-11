package harness

import (
	"context"
	"strings"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/tool"
)

// OneShot 会话无关的一次执行参数。
//
// 与聊天 run 的区别：不落库、不发 SSE、不绑定 sessionID，因而工作流 LLM 节点 /
// 子代理 / 评估器 / CLI 都能复用同一套主循环（工具执行、压缩、预算与策略门全共享）。
type OneShot struct {
	RunID         string               // 可选；空 = 自动生成
	Provider      llm.Provider         // 必填
	Model         string               // 必填
	Messages      []*llm.Message       // 必填：system（可选）+ user
	Tools         *tool.Registry       // 可选；nil 或 ToolDefs 为空 = 不带工具（纯补全）
	ToolDefs      []llm.ToolDefinition // 可选：暴露给模型的工具定义
	Config        Config               // 可选；零值走 DefaultConfig
	RequestParams RequestParams        // 可选：采样参数（temperature / thinking）
}

// RunOnce 会话无关地跑一次完整 ReAct 循环，语义与 Runner.RunMessages 一致；
// sessionID / assistantMessageID 传空串，不落库、不发事件。
func RunOnce(ctx context.Context, o OneShot) RunResult {
	if o.Provider == nil {
		return RunResult{StopReason: string(ReasonError), Reason: "provider is nil"}
	}
	cfg := o.Config
	if cfg.MaxTurns <= 0 {
		cfg = DefaultConfig()
	}
	runID := strings.TrimSpace(o.RunID)
	if runID == "" {
		runID = pkg.NewID("run")
	}
	r := NewRunner(o.Provider, NopSink{}, cfg).WithRequestParams(o.RequestParams)
	if o.Tools != nil && len(o.ToolDefs) > 0 {
		r = r.WithTools(o.Tools, o.ToolDefs)
	}
	return r.RunMessages(ctx, runID, "", "", o.Model, o.Messages)
}

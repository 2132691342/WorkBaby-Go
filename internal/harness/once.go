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
// 与聊天 run 的唯一区别：不落库（无 chat_messages）、不发 SSE（NopSink）、不绑定 sessionID。
// 因此「工作流 LLM 节点 / 子代理 / 评估器 / CLI」都能复用同一套主循环，
// 共享工具执行、上下文装配与压缩、预算与停滞熔断、工具策略门等全部能力。
type OneShot struct {
	RunID      string                 // 可选；空 = 自动生成（pkg.NewID）
	Provider   llm.Provider           // 必填
	Model      string                 // 必填
	Messages   []*llm.Message         // 必填：system（可选）+ user
	Tools      *tool.Registry         // 可选；nil 或 ToolDefs 为空 = 不带工具（纯补全）
	ToolDefs   []llm.ToolDefinition   // 可选：暴露给模型的工具定义
	Config     Config                 // 可选：零值走 DefaultConfig
	RequestParams RequestParams       // 可选：采样参数（temperature / thinking）
}

// RunOnce 会话无关地跑一次完整 ReAct 循环。
//
// 语义与 Runner.RunMessages 完全一致（多轮工具 → 回填 → 直到无工具调用或达终止条件），
// 只是不依赖会话与持久化：sessionID / assistantMessageID 传空串。
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

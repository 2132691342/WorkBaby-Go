package service

import (
	"context"
	"strings"

	"WorkBaby/internal/harness"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/llm/registry"
	"WorkBaby/internal/pkg"
	wnodes "WorkBaby/internal/workflow/nodes"
)

// WorkflowReactor 把 harness 的 ReAct 主循环接给工作流 LLM 节点。
//
// 依赖倒置：nodes 包不依赖 harness，由 service 层（可同时依赖两者）装配实现，
// 保持 workflow 为叶子包、不破坏分层门禁。
//
// 语义与聊天 run 一致（多轮工具执行、上下文装配与压缩、预算与停滞熔断），
// 但会话无关：不落 chat_messages、不发 SSE。
type WorkflowReactor struct {
	providers *registry.Registry
	tools     *ToolService
}

// NewWorkflowReactor 构造工作流 ReAct 执行器。
func NewWorkflowReactor(providers *registry.Registry, tools *ToolService) *WorkflowReactor {
	return &WorkflowReactor{providers: providers, tools: tools}
}

// React 实现 wnodes.ReactFunc：跑一次带工具的 ReAct 循环。
func (w *WorkflowReactor) React(ctx context.Context, req wnodes.ReactRequest) (wnodes.ReactResult, error) {
	if w.providers == nil {
		return wnodes.ReactResult{}, pkg.New(9105, "ReAct 未配置 llm.Registry", "")
	}
	prov, err := w.providers.Get(req.ProviderID)
	if err != nil {
		return wnodes.ReactResult{}, pkg.Wrap(9105, "provider 取不到", err)
	}
	if req.Model == "" {
		return wnodes.ReactResult{}, pkg.New(9104, "ReAct 缺少 model", "")
	}

	var msgs []*llm.Message
	if strings.TrimSpace(req.SystemPrompt) != "" {
		msgs = append(msgs, &llm.Message{Role: llm.RoleSystem, Content: req.SystemPrompt})
	}
	msgs = append(msgs, &llm.Message{Role: llm.RoleUser, Content: req.UserPrompt})

	// 工具白名单：只有节点显式声明的工具才暴露给模型（最小权限）
	var defs []llm.ToolDefinition
	if w.tools != nil && len(req.Tools) > 0 {
		defs = w.tools.LLMDefinitionsFiltered(ctx, req.Tools)
	}

	cfg := harness.DefaultConfig()
	if req.MaxTurns > 0 {
		cfg.MaxTurns = req.MaxTurns
	}
	res := harness.RunOnce(ctx, harness.OneShot{
		Provider:      prov,
		Model:         req.Model,
		Messages:      msgs,
		Tools:         w.tools.Registry(),
		ToolDefs:      defs,
		Config:        cfg,
		RequestParams: harness.RequestParams{Temperature: req.Temperature},
	})
	if res.Err != nil {
		return wnodes.ReactResult{}, pkg.Wrap(9105, "ReAct 执行失败", res.Err)
	}
	return wnodes.ReactResult{
		Text:       res.Content,
		Usage:      res.Usage,
		Turns:      len(res.Turns), // 每轮一条用量记录，长度即实际轮数
		StopReason: res.StopReason,
	}, nil
}

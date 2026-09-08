package service

import (
	"context"
	"strings"
	"time"

	"WorkBaby/internal/domain"
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
	usageSink wnodes.UsageSink // 计量回调；nil = 不落 token_usages
}

// NewWorkflowReactor 构造工作流 ReAct 执行器。
func NewWorkflowReactor(providers *registry.Registry, tools *ToolService) *WorkflowReactor {
	return &WorkflowReactor{providers: providers, tools: tools}
}

// WithUsageSink 注入计量回调：每轮 turn 明细落 token_usages（Source=workflow）。
func (w *WorkflowReactor) WithUsageSink(s wnodes.UsageSink) *WorkflowReactor {
	w.usageSink = s
	return w
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
	// 逐轮落 token_usages：保证工作流消耗与聊天同一计量口径
	if w.usageSink != nil {
		for _, t := range res.Turns {
			w.usageSink(req.ExecutionID, req.ProviderID, req.Model, t.Usage)
		}
	}
	return wnodes.ReactResult{
		Text:       res.Content,
		Usage:      res.Usage,
		Turns:      len(res.Turns), // 每轮一条用量记录，长度即实际轮数
		StopReason: res.StopReason,
	}, nil
}

// WorkflowUsageSink 工作流 LLM 计量落库（handler 装配为闭包；repo 不进 nodes 包）。
func WorkflowUsageSink(usages interface {
	BatchCreate(ctx context.Context, rows []domain.TokenUsageDO) error
}) wnodes.UsageSink {
	if usages == nil {
		return nil
	}
	return func(executionID, providerID, model string, usage llm.TokenUsage) {
		row := domain.TokenUsageDO{
			ID:               pkg.NewID("USAGE"),
			RunID:            executionID,
			ProviderID:       providerID,
			Model:            model,
			Source:           domain.UsageSourceWorkflow,
			InputTokens:      usage.InputTokens,
			OutputTokens:     usage.OutputTokens,
			CacheReadTokens:  usage.CacheReadTokens,
			CacheWriteTokens: usage.CacheWriteTokens,
			TotalTokens:      usage.TotalTokens,
			CreatedAt:        time.Now().UnixMilli(),
		}
		if err := usages.BatchCreate(context.Background(), []domain.TokenUsageDO{row}); err != nil {
			pkg.L.Warn("persist workflow usage failed", "executionID", executionID, "err", err.Error())
		}
	}
}

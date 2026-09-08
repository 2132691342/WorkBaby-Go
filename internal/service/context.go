package service

import (
	"context"
	"encoding/json"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/pkg"
)

// defaultContextWindow provider 未声明上下文窗口时的兜底值（与前端 ChatView 的 128000 一致）。
const defaultContextWindow = 128_000

// ContextUsage 会话上下文占用分段快照（/context）。
// 与真实请求同源（buildSystem）；recall 用会话最后一条用户消息作检索代理；
// 历史段优先用末条 assistant 的实测 input_tokens，拿不到按估算并置 Estimated。
func (s *ChatService) ContextUsage(ctx context.Context, sessionID string) (domain.ContextUsageRESP, error) {
	ses, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return domain.ContextUsageRESP{}, err
	}
	resp := domain.ContextUsageRESP{
		SessionID:     ses.ID,
		Model:         ses.Model,
		ContextWindow: s.contextWindow(ctx, ses.ProviderID),
	}
	if resp.ContextWindow <= 0 {
		resp.ContextWindow = defaultContextWindow
	}
	provRow := s.providerDO(ctx, ses.ProviderID)
	resp.ContextBudget = s.contextBudget(ctx, resp.ContextWindow, provRow)

	// history：实测优先，估算兜底（先取 rows，recall 代理要从里面找最后一条用户输入）
	rows, err := s.messages.ListBySession(ctx, ses.ID, 0, 0)
	if err != nil {
		return domain.ContextUsageRESP{}, err
	}
	history, measured := historyTokens(rows)
	if !measured {
		msgs, err := s.toLLMMessages(rows)
		if err != nil {
			return domain.ContextUsageRESP{}, err
		}
		history = harness.EstimateTokens(msgs)
	}
	resp.Estimated = !measured

	// system：与真实请求同一装配入口；recall 用最后一条用户消息作检索代理
	def := harness.Agent(defaultAgentName)
	system := ""
	if sys, _ := s.buildSystem(ctx, ses, "preview", lastUserInput(rows), def); sys != nil {
		system = sys.Content
	}

	// tools：本会话实际暴露的工具定义
	toolDefs := def.FilterTools(s.tools.LLMDefinitions(ctx))
	toolsJSON, err := json.Marshal(toolDefs)
	if err != nil {
		toolsJSON = nil
	}

	// memory 段已并入 system（buildSystem 的记忆能力注入就是真实请求里的 memory
	// 正文；单列会在透视里重复计数）
	resp.Segments = []domain.ContextSegment{
		{Key: "system", Title: "系统提示（含记忆注入）", Tokens: pkg.EstimateTextTokens(system)},
		{Key: "tools", Title: "工具定义", Tokens: pkg.EstimateTextTokens(string(toolsJSON))},
		{Key: "history", Title: "历史消息", Tokens: history},
	}
	for _, seg := range resp.Segments {
		resp.UsedTokens += seg.Tokens
	}
	for i := range resp.Segments {
		resp.Segments[i].Ratio = perMille(resp.Segments[i].Tokens, resp.ContextWindow)
	}
	resp.FreeTokens = resp.ContextWindow - resp.UsedTokens
	if resp.FreeTokens < 0 {
		resp.FreeTokens = 0
	}
	resp.UsedRatio = perMille(resp.UsedTokens, resp.ContextWindow)
	resp.MessageCount = len(rows)
	resp.ToolCount = len(toolDefs)
	return resp, nil
}

// lastUserInput 取最后一条用户消息正文（recall 检索代理）；无则空串。
func lastUserInput(rows []domain.MessageDO) string {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Role == domain.MessageRoleUser && strings.TrimSpace(rows[i].Content) != "" {
			return rows[i].Content
		}
	}
	return ""
}

// contextWindow 取 provider 声明的上下文窗口；provider 缺失/取失败返回 0（调用方兜底）。
func (s *ChatService) contextWindow(ctx context.Context, providerID string) int {
	if s.provRepo == nil || providerID == "" {
		return 0
	}
	row, err := s.provRepo.GetByID(ctx, providerID)
	if err != nil || row == nil {
		return 0
	}
	return row.ContextWindow
}

// historyTokens 取末条 assistant 落库的实测 input_tokens；没有实测值时 measured=false。
func historyTokens(rows []domain.MessageDO) (tokens int, measured bool) {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Role == domain.MessageRoleAssistant && rows[i].InputTokens > 0 {
			return rows[i].InputTokens, true
		}
	}
	return 0, false
}

// perMille 千分比（避免前端浮点误差；分母 <=0 返回 0）。
func perMille(part, total int) int {
	if total <= 0 {
		return 0
	}
	return part * 1000 / total
}

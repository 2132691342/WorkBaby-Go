package service

import (
	"context"
	"encoding/json"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
)

// defaultContextWindow provider 未声明上下文窗口时的兜底值（与前端 ChatView 的 128000 一致）。
const defaultContextWindow = 128_000

// ContextUsage 会话上下文占用分段快照（/context）。
//
// 分段口径与真实请求保持一致：system（人设 + 保留指示） / memory（长期记忆） /
// tools（暴露给 LLM 的工具定义） / history（历史消息）。
// Skill 段不在此列——它按每轮用户输入命中，无请求上下文时无法判定，故不计入。
//
// 历史段优先用末条 assistant 落库的实测 input_tokens；拿不到才按字符近似估算，
// 并把 Estimated 置 true 让前端标注「估算」。
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

	// system：人设 + 保留指示（压缩也压不掉的部分）
	def := harness.Agent(defaultAgentName)
	system := ""
	if msg := def.PersonaSystemMessage(); msg != nil {
		system = msg.Content
	}
	if ins := compactInstructions(ses); ins != "" {
		system += "\n\n" + ins
	}

	// memory：长期记忆（MEMORY.md）
	longTerm := ""
	if s.mem != nil {
		longTerm = s.mem.LongTerm(ctx, ses.ID)
	}

	// tools：本会话实际暴露的工具定义
	toolDefs := def.FilterTools(s.tools.LLMDefinitions(ctx))
	toolsJSON, err := json.Marshal(toolDefs)
	if err != nil {
		toolsJSON = nil
	}

	// history：实测优先，估算兜底
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

	resp.Segments = []domain.ContextSegment{
		{Key: "system", Title: "系统提示", Tokens: runeTokens(system)},
		{Key: "memory", Title: "长期记忆", Tokens: runeTokens(longTerm)},
		{Key: "tools", Title: "工具定义", Tokens: runeTokens(string(toolsJSON))},
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

// runeTokens 字符数 → 估算 token（rune/4，与 harness.EstimateTokens 同口径）。
func runeTokens(s string) int { return len([]rune(s)) / 4 }

// perMille 千分比（避免前端浮点误差；分母 <=0 返回 0）。
func perMille(part, total int) int {
	if total <= 0 {
		return 0
	}
	return part * 1000 / total
}

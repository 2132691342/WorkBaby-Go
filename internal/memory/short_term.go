package memory

import (
	"context"
	"encoding/json"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/repo"
)

// shortTerm 短期记忆：chat_messages 滑动窗口（只取 completed）。
type shortTerm struct {
	msgs *repo.MessageRepo
}

func newShortTerm(msgs *repo.MessageRepo) *shortTerm { return &shortTerm{msgs: msgs} }

// Load 取最近 MaxMessages 条 completed 消息，时间正序。
func (s *shortTerm) Load(ctx context.Context, sessionID string, opts ShortTermOpts) []llm.Message {
	if opts.MaxMessages <= 0 {
		opts.MaxMessages = 50
	}
	rows, err := s.msgs.ListRecentBySession(ctx, sessionID, opts.MaxMessages)
	if err != nil {
		return nil
	}
	// ListRecentBySession 返回 seq DESC；反转恢复时间正序
	out := make([]llm.Message, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		m := rows[i]
		if m.Status != domain.MessageStatusCompleted {
			continue // pending/failed/cancelled 不进上下文
		}
		lm := llm.Message{Role: llm.RoleType(m.Role), Content: m.Content, Thinking: m.Thinking}
		if m.Role == domain.MessageRoleTool {
			lm.ToolCallID = m.ToolCallID
		}
		if m.Role == domain.MessageRoleAssistant && m.ToolCalls != "" {
			var calls []llm.ToolCall
			if json.Unmarshal([]byte(m.ToolCalls), &calls) == nil {
				lm.ToolCalls = calls
			}
		}
		out = append(out, lm)
	}
	return out
}

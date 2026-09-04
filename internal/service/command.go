package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// BuiltinCommands 内置斜杠命令表（命令面板）。
//
// 单一真相源：前端启动拉一次 GET /api/v1/chat/commands 渲染面板。
// ClientOnly 命令的执行逻辑在前端（切模型/开面板/导出），后端不重复实现。
func BuiltinCommands() []domain.SlashCommand {
	return []domain.SlashCommand{
		{Name: "help", Desc: "查看全部命令说明", Group: "system", ClientOnly: true},
		{Name: "new", Desc: "新建会话", Group: "session", ClientOnly: true},
		{Name: "clear", Desc: "清空当前会话消息", Group: "session", ClientOnly: true},
		{Name: "compact", Args: "[保留指示]", Desc: "压缩历史上下文，可附带压缩后必须保留的要点", Group: "session"},
		{Name: "export", Desc: "导出当前会话为 Markdown", Group: "session", ClientOnly: true},
		{Name: "model", Args: "<模型名>", Desc: "切换当前会话模型", Group: "model", ClientOnly: true},
		{Name: "agent", Args: "<default|coding|research|writer>", Desc: "切换当前 Agent", Group: "agent", ClientOnly: true},
		{Name: "trust", Args: "<default|auto-edit|yolo>", Desc: "切换工具权限模式", Group: "agent", ClientOnly: true},
		{Name: "tasks", Desc: "打开后台任务中心", Group: "system", ClientOnly: true},
	}
}

// CompactSession 确定性压缩历史（/compact 的后端实现）。
//
// 策略与 harness.MicroCompressor 一致但作用于落库消息：清掉较早轮次的推理文本、
// 折叠工具结果正文，保留最近 keep 条完整不动。无需 LLM、幂等、可反复执行。
//
// 保留指示（req.Instructions）不落进消息表——它写进会话元数据，由上下文装配期
// 作为常驻 system 段注入（见 executeAgent）。这样指示不受任何历史折叠影响，
// 也不会因插入消息而打乱会话 seq。
//
// 单条更新失败只计数不中断（部分成功仍返回可用结果），整体才可观测：
// 失败数与释放量都回到出参，前端按 Failed>0 提示「部分压缩失败」。
func (s *ChatService) CompactSession(ctx context.Context, sessionID string, req domain.CompactREQ) (domain.CompactResultRESP, error) {
	ses, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return domain.CompactResultRESP{}, err
	}
	rows, err := s.messages.ListBySession(ctx, sessionID, 0, compactScanLimit)
	if err != nil {
		return domain.CompactResultRESP{}, err
	}
	keep := req.KeepRecent
	if keep <= 0 {
		keep = compactKeepRecent
	}
	out := domain.CompactResultRESP{SessionID: sessionID, KeptRecent: keep, TotalBefore: len(rows)}

	// 保留指示先钉住：即使无需压缩也要生效（用户可能只想钉一条要点）
	if ins := strings.TrimSpace(req.Instructions); ins != "" {
		out.Pinned = s.pinCompactInstructions(ctx, ses, ins)
	}
	if len(rows) <= keep {
		return out, nil
	}

	cut := len(rows) - keep
	now := time.Now().UnixMilli()
	for i := 0; i < cut; i++ {
		row := rows[i]
		fields := map[string]any{}
		switch row.Role {
		case domain.MessageRoleTool:
			if row.Content == "" || isFolded(row.Content) {
				continue
			}
			out.FreedChars += len(row.Content)
			fields["content"] = foldToolResult(len(row.Content))
		case domain.MessageRoleAssistant:
			if row.Thinking == "" {
				continue
			}
			out.FreedChars += len(row.Thinking)
			fields["thinking"] = ""
		default:
			continue
		}
		fields["updated_at"] = now
		if err := s.messages.UpdateStatus(ctx, row.ID, fields); err != nil {
			out.Failed++
			pkg.L.Warn("compact message failed", "sessionID", sessionID, "messageID", row.ID, "err", err.Error())
			continue
		}
		out.Compacted++
	}
	out.FreedTokens = estimateTokensFromChars(out.FreedChars)
	return out, nil
}

// pinCompactInstructions 把保留指示写进会话元数据（后续 run 装配期注入 system 段）。
func (s *ChatService) pinCompactInstructions(ctx context.Context, ses *domain.ChatSessionDO, ins string) bool {
	meta := readSessionMeta(ses)
	meta[sessionMetaKeyCompactInstructions] = ins
	if err := s.writeSessionMeta(ctx, ses, meta); err != nil {
		pkg.L.Warn("pin compact instructions failed", "sessionID", ses.ID, "err", err.Error())
		return false
	}
	return true
}

// estimateTokensFromChars 字符数 → 估算 token（rune/4 近似，与 harness.EstimateTokens 同口径）。
func estimateTokensFromChars(chars int) int { return chars / 4 }

// 压缩参数：扫描上限与保留窗口。
const (
	compactScanLimit  = 1000
	compactKeepRecent = 20
)

// isFolded 该工具结果是否已折叠（幂等：重复压缩不重复计数）。
func isFolded(content string) bool {
	return strings.Contains(content, "[已折叠]")
}

// foldToolResult 折叠占位文本（保留原始体积信息，便于回看规模）。
func foldToolResult(n int) string {
	return "[已折叠] 工具结果正文已压缩，原 " + strconv.Itoa(n) + " 字符。"
}

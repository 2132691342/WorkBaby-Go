package api

import (
	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/service"
)

// ListMessages 增量分页（afterSeq=0 取该会话全部）。
func (h *Handler) ListMessages(sessionID string, afterSeq int64, limit int) (domain.MessageListRESP, error) {
	r, err := h.chatSvc.ListMessages(h.ctx, sessionID, afterSeq, limit)
	if err != nil {
		return domain.MessageListRESP{}, err
	}
	return *r, nil
}

// SendStream 立即返回 runID/消息 ID；流式事件经 harness → event.Bus → api 层 → chat:* 推前端。
func (h *Handler) SendStream(req domain.SendStreamREQ) (domain.SendStreamResult, error) {
	if req.SessionID == "" {
		return domain.SendStreamResult{}, domain.ErrSessionInvalid
	}
	params := harness.RequestParams{Temperature: req.Temperature, Thinking: llm.ThinkingFromEffort(req.ThinkingEffort)}
	r, err := h.chatSvc.SendStream(h.ctx, req.SessionID, req.Content, params)
	if err != nil {
		return domain.SendStreamResult{}, err
	}
	return *r, nil
}

// ResumeChat 从检查点续跑：同一 runID 恢复，前端以返回的 run_id 重挂 SSE。
func (h *Handler) ResumeChat(runID string) (domain.SendStreamResult, error) {
	if runID == "" {
		return domain.SendStreamResult{}, domain.ErrSessionInvalid
	}
	r, err := h.chatSvc.ResumeRun(h.ctx, runID)
	if err != nil {
		return domain.SendStreamResult{}, err
	}
	return *r, nil
}

// ExportRunEvents run 事件 JSONL 导出（回放/测试/无头消费）。
func (h *Handler) ExportRunEvents(runID string) (string, error) {
	bs, err := h.eventLog.Export(runID)
	if err != nil {
		return "", pkg.New(5007, "run 事件导出不可用", runID)
	}
	return string(bs), nil
}

// SteerSession run 进行中插入一条用户指令（steering / follow-up 注入缝）。
// 会话当前没有活动 run 时返回 5010，前端退回普通发送。
func (h *Handler) SteerSession(sessionID string, req domain.SteerREQ) (domain.SteerResultRESP, error) {
	r, err := h.chatSvc.QueueSteer(h.ctx, sessionID, req.Content)
	if err != nil {
		return domain.SteerResultRESP{}, err
	}
	return *r, nil
}

// CancelStream 中断会话正在跑的 run（前端「停止」按钮调用）。幂等。
func (h *Handler) CancelStream(sessionID string) error {
	return h.chatSvc.CancelStream(sessionID)
}

// ListChatCommands 内置斜杠命令元数据（前端命令面板的数据源）。
func (h *Handler) ListChatCommands() domain.CommandListRESP {
	items := service.BuiltinCommands()
	return domain.CommandListRESP{Items: items, Total: len(items)}
}

// CompactSession 压缩会话历史上下文（/compact 命令的后端实现）。
//
// req.Instructions 是「保留指示」：非空会钉进会话元数据并以 system 段常驻注入
// （不受历史折叠影响）；req.KeepRecent 覆盖默认保留窗口（<=0 用后端默认）。
func (h *Handler) CompactSession(sessionID string, req domain.CompactREQ) (domain.CompactResultRESP, error) {
	return h.chatSvc.CompactSession(h.ctx, sessionID, req)
}

// SearchSessions 跨会话快速检索（/resume）。
//
// req.Scope：all / title / content（空 = all）；req.Limit 上限 50。
func (h *Handler) SearchSessions(req domain.SessionSearchREQ) (*domain.SessionListRESP, error) {
	return h.chatSvc.SearchSessions(h.ctx, req)
}

// ContextUsage 会话上下文占用分段快照（/context 侧栏环）。
func (h *Handler) ContextUsage(sessionID string) (domain.ContextUsageRESP, error) {
	return h.chatSvc.ContextUsage(h.ctx, sessionID)
}

// GetSessionTodos 返回会话计划快照（前端进度卡首次渲染 / 刷新后恢复用）。
// 运行中的增量由 chat:todo 事件推送，与此处同源（同一 SessionTodoStore）。
func (h *Handler) GetSessionTodos(sessionID string) domain.TodoStateRESP {
	if h.todoStore == nil {
		return domain.TodoStateRESP{SessionID: sessionID}
	}
	return h.todoStore.State(sessionID)
}

// ClearMessages 清空某会话全部消息。
func (h *Handler) ClearMessages(sessionID string) error {
	return h.chatSvc.ClearMessages(h.ctx, sessionID)
}

// DeleteMessage 删除单条消息。
func (h *Handler) DeleteMessage(sessionID string, messageID string) error {
	return h.chatSvc.DeleteMessage(h.ctx, sessionID, messageID)
}

// TruncateMessages 从指定消息起（含）截断，用于「从此处重新生成」。
func (h *Handler) TruncateMessages(sessionID string, req domain.TruncateMessagesREQ) error {
	return h.chatSvc.TruncateMessages(h.ctx, sessionID, req.MessageID)
}

// ForkSession 从指定消息处分叉为新会话。
func (h *Handler) ForkSession(sessionID string, req domain.ForkSessionREQ) (domain.ChatSessionRESP, error) {
	r, err := h.chatSvc.ForkSession(h.ctx, sessionID, req.MessageID, req.Name)
	if err != nil {
		return domain.ChatSessionRESP{}, err
	}
	return *r, nil
}

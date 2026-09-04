package api

import "WorkBaby/internal/domain"

// SubmitTask 提交后台任务（立即返回，任务在 worker pool 里排队执行）。
func (h *Handler) SubmitTask(req domain.TaskSubmitREQ) (domain.TaskRESP, error) {
	t, err := h.taskSvc.Submit(req)
	if err != nil {
		return domain.TaskRESP{}, err
	}
	return toTaskRESPView(t), nil
}

// ListTasks 任务列表（最新在前）；sessionId 为空返回全部。
func (h *Handler) ListTasks(sessionID string, limit int) domain.TaskListRESP {
	return h.taskSvc.List(sessionID, limit)
}

// GetTask 单条任务详情。
func (h *Handler) GetTask(id string) (domain.TaskRESP, error) {
	return h.taskSvc.Get(id)
}

// CancelTask 取消排队/运行中的任务。
func (h *Handler) CancelTask(id string) error {
	return h.taskSvc.Cancel(id)
}

// toTaskRESPView 任务 DO → 出参（handler 层不暴露 service 内部结构）。
func toTaskRESPView(t *domain.TaskDO) domain.TaskRESP {
	return domain.TaskRESP{
		ID:         t.ID,
		SessionID:  t.SessionID,
		Agent:      t.Agent,
		Prompt:     t.Prompt,
		State:      t.State,
		RunID:      t.RunID,
		Result:     t.Result,
		Error:      t.Error,
		CreatedAt:  t.CreatedAt,
		StartedAt:  t.StartedAt,
		FinishedAt: t.FinishedAt,
	}
}

package api

import (
	"WorkBaby/internal/domain"
)

// ListCronJobs 定时任务列表。
func (h *Handler) ListCronJobs() ([]domain.CronJobRESP, error) {
	return h.cronSvc.List(h.ctx)
}

// CreateCronJob 新建定时任务。
func (h *Handler) CreateCronJob(req domain.CronJobREQ) (domain.CronJobRESP, error) {
	return h.cronSvc.Create(h.ctx, req)
}

// UpdateCronJob 更新定时任务（POST /cron/jobs/update/{id}）。
func (h *Handler) UpdateCronJob(id string, req domain.CronJobREQ) (domain.CronJobRESP, error) {
	return h.cronSvc.Update(h.ctx, id, req)
}

// DeleteCronJob 删除定时任务（POST /cron/jobs/delete/{id}）。
func (h *Handler) DeleteCronJob(id string) (map[string]any, error) {
	if err := h.cronSvc.Delete(h.ctx, id); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "deleted": true}, nil
}

// TriggerCronJob 立即触发一次（POST /cron/jobs/trigger/{id}）。
func (h *Handler) TriggerCronJob(id string) (map[string]any, error) {
	if err := h.cronSvc.Trigger(h.ctx, id); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "triggered": true}, nil
}

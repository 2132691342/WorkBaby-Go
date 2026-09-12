package repo

import (
	"context"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// CronJobRunLogRepo 单次执行日志（重试轨迹 + 错误上下文）。
type CronJobRunLogRepo struct{ db *gorm.DB }

// NewCronJobRunLogRepo 构造。
func NewCronJobRunLogRepo(db *gorm.DB) *CronJobRunLogRepo { return &CronJobRunLogRepo{db: db} }

// Create 写入一条日志。
func (r *CronJobRunLogRepo) Create(ctx context.Context, row *domain.CronJobRunLogDO) error {
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return pkg.Wrap(2016, "create cron run log failed", err)
	}
	return nil
}

// Update 结束时间与状态。
func (r *CronJobRunLogRepo) Update(ctx context.Context, id string, status string, finishedAt, durationMs int64, errMsg string) error {
	updates := map[string]any{
		"status":       status,
		"finished_at":  finishedAt,
		"duration_ms":  durationMs,
		"error":        errMsg,
	}
	if err := r.db.WithContext(ctx).Model(&domain.CronJobRunLogDO{}).
		Where("id = ?", id).Updates(updates).Error; err != nil {
		return pkg.Wrap(2016, "update cron run log failed", err)
	}
	return nil
}

// ListByJob 任务历史日志（最新在前，limit 上限 200）。
func (r *CronJobRunLogRepo) ListByJob(ctx context.Context, jobID string, limit int) ([]domain.CronJobRunLogDO, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []domain.CronJobRunLogDO
	if err := r.db.WithContext(ctx).
		Where("job_id = ?", jobID).
		Order("started_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2016, "list cron run logs failed", err)
	}
	return rows, nil
}

// DeleteByJob 任务删除级联清理。
func (r *CronJobRunLogRepo) DeleteByJob(ctx context.Context, jobID string) error {
	if err := r.db.WithContext(ctx).Where("job_id = ?", jobID).Delete(&domain.CronJobRunLogDO{}).Error; err != nil {
		return pkg.Wrap(2016, "delete cron run logs failed", err)
	}
	return nil
}

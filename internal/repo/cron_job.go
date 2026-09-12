package repo

import (
	"context"
	"errors"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// CronJobRepo cron_jobs 表 CRUD（软删）。
type CronJobRepo struct{ db *gorm.DB }

// NewCronJobRepo 构造仓储。
func NewCronJobRepo(db *gorm.DB) *CronJobRepo { return &CronJobRepo{db: db} }

// List 全部（含停用，按创建时间倒序）。
func (r *CronJobRepo) List(ctx context.Context) ([]domain.CronJobDO, error) {
	var rows []domain.CronJobDO
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2070, "list cron jobs failed", err)
	}
	return rows, nil
}

// ListEnabled 启用的任务（调度器启动时加载）。
func (r *CronJobRepo) ListEnabled(ctx context.Context) ([]domain.CronJobDO, error) {
	var rows []domain.CronJobDO
	if err := r.db.WithContext(ctx).Where("enabled = ?", true).Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2070, "list enabled cron jobs failed", err)
	}
	return rows, nil
}

// GetByID 按 ID 查找；找不到返回 ErrCronJobNotFound。
func (r *CronJobRepo) GetByID(ctx context.Context, id string) (*domain.CronJobDO, error) {
	var row domain.CronJobDO
	if err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrCronJobNotFound
		}
		return nil, pkg.Wrap(2070, "get cron job failed", err)
	}
	return &row, nil
}

// Create 新建。
func (r *CronJobRepo) Create(ctx context.Context, row *domain.CronJobDO) error {
	if row.ID == "" {
		row.ID = pkg.NewID(domain.IDCron)
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return pkg.Wrap(2071, "create cron job failed", err)
	}
	return nil
}

// Update 保存（Save 按主键更新全部字段）。
func (r *CronJobRepo) Update(ctx context.Context, row *domain.CronJobDO) error {
	if err := r.db.WithContext(ctx).Save(row).Error; err != nil {
		return pkg.Wrap(2071, "update cron job failed", err)
	}
	return nil
}

// Delete 软删。
func (r *CronJobRepo) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Delete(&domain.CronJobDO{}, "id = ?", id).Error; err != nil {
		return pkg.Wrap(2072, "delete cron job failed", err)
	}
	return nil
}

// UpdateRunState 记录一次执行结果（lastRunAt/lastStatus）。
func (r *CronJobRepo) UpdateRunState(ctx context.Context, id string, lastRunAt int64, status string) error {
	if err := r.db.WithContext(ctx).Model(&domain.CronJobDO{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_run_at": lastRunAt,
			"last_status": status,
			"updated_at":  lastRunAt,
		}).Error; err != nil {
		return pkg.Wrap(2071, "update cron run state failed", err)
	}
	return nil
}

// IncrementRunCount 累计执行次数（failed=true 同步累加 fail_count）。
func (r *CronJobRepo) IncrementRunCount(ctx context.Context, id string, failed bool) error {
	updates := map[string]any{"run_count": gorm.Expr("run_count + 1"), "updated_at": time.Now().UnixMilli()}
	if failed {
		updates["fail_count"] = gorm.Expr("fail_count + 1")
	}
	if err := r.db.WithContext(ctx).Model(&domain.CronJobDO{}).
		Where("id = ?", id).Updates(updates).Error; err != nil {
		return pkg.Wrap(2071, "increment cron run count failed", err)
	}
	return nil
}

// UpdateNextRunAt 更新下次执行时间（调度注册/刷新时）。
func (r *CronJobRepo) UpdateNextRunAt(ctx context.Context, id string, nextRunAt int64) error {
	if err := r.db.WithContext(ctx).Model(&domain.CronJobDO{}).
		Where("id = ?", id).
		Updates(map[string]any{"next_run_at": nextRunAt}).Error; err != nil {
		return pkg.Wrap(2071, "update cron next run failed", err)
	}
	return nil
}

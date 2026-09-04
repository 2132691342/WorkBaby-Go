package repo

import (
	"context"
	"errors"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// MemoryProcedureRepo memory_procedures 表（程序记忆）。
type MemoryProcedureRepo struct{ db *gorm.DB }

func NewMemoryProcedureRepo(db *gorm.DB) *MemoryProcedureRepo { return &MemoryProcedureRepo{db: db} }

// List 取全部程序（最近使用优先）。
func (r *MemoryProcedureRepo) List(ctx context.Context, limit int) ([]domain.MemoryProcedureDO, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var rows []domain.MemoryProcedureDO
	if err := r.db.WithContext(ctx).Order("last_used_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(6061, "list memory procedures failed", err)
	}
	return rows, nil
}

// GetByName 按名取（同名视为同一程序，覆盖更新）。
func (r *MemoryProcedureRepo) GetByName(ctx context.Context, name string) (*domain.MemoryProcedureDO, error) {
	var row domain.MemoryProcedureDO
	err := r.db.WithContext(ctx).Where("name = ?", name).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, pkg.Wrap(6061, "get memory procedure failed", err)
	}
	return &row, nil
}

// Upsert 新增或覆盖。
func (r *MemoryProcedureRepo) Upsert(ctx context.Context, row *domain.MemoryProcedureDO) error {
	old, err := r.GetByName(ctx, row.Name)
	if err != nil {
		return err
	}
	if old != nil {
		row.ID = old.ID
		fields := map[string]any{
			"steps":         row.Steps,
			"success_count": row.SuccessCount,
			"failure_count": row.FailureCount,
			"last_used_at":  row.LastUsedAt,
			"updated_at":    row.UpdatedAt,
		}
		if err := r.db.WithContext(ctx).Model(&domain.MemoryProcedureDO{}).
			Where("id = ?", row.ID).Updates(fields).Error; err != nil {
			return pkg.Wrap(6060, "update memory procedure failed", err)
		}
		return nil
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return pkg.Wrap(6060, "insert memory procedure failed", err)
	}
	return nil
}

// Delete 删除单条。
func (r *MemoryProcedureRepo) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Where("id = ?", id).
		Delete(&domain.MemoryProcedureDO{}).Error; err != nil {
		return pkg.Wrap(6062, "delete memory procedure failed", err)
	}
	return nil
}

// Count 总数。
func (r *MemoryProcedureRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&domain.MemoryProcedureDO{}).Count(&n).Error; err != nil {
		return 0, pkg.Wrap(6061, "count memory procedures failed", err)
	}
	return n, nil
}

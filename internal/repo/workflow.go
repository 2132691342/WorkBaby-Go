package repo

import (
	"context"
	"errors"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// WorkflowRepo workflows 表 CRUD。
type WorkflowRepo struct{ db *gorm.DB }

// NewWorkflowRepo 构造仓储。
func NewWorkflowRepo(db *gorm.DB) *WorkflowRepo { return &WorkflowRepo{db: db} }

// List 全部（含 disabled，按更新时间倒序）。
func (r *WorkflowRepo) List(ctx context.Context) ([]domain.WorkflowDO, error) {
	var rows []domain.WorkflowDO
	if err := r.db.WithContext(ctx).Order("updated_at DESC").Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2090, "list workflows failed", err)
	}
	return rows, nil
}

// GetByID 按 ID 查找；找不到返回 ErrWorkflowNotFound。
func (r *WorkflowRepo) GetByID(ctx context.Context, id string) (*domain.WorkflowDO, error) {
	var row domain.WorkflowDO
	if err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrWorkflowNotFound
		}
		return nil, pkg.Wrap(2090, "get workflow failed", err)
	}
	return &row, nil
}

// Create 新建。
func (r *WorkflowRepo) Create(ctx context.Context, row *domain.WorkflowDO) error {
	if row.ID == "" {
		row.ID = pkg.NewID(domain.IDWorkflow)
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return pkg.Wrap(2091, "create workflow failed", err)
	}
	return nil
}

// Update 保存（Save 同时按主键更新所有字段）。
func (r *WorkflowRepo) Update(ctx context.Context, row *domain.WorkflowDO) error {
	if err := r.db.WithContext(ctx).Save(row).Error; err != nil {
		return pkg.Wrap(2091, "update workflow failed", err)
	}
	return nil
}

// Delete 软删。
func (r *WorkflowRepo) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Delete(&domain.WorkflowDO{}, "id = ?", id).Error; err != nil {
		return pkg.Wrap(2092, "delete workflow failed", err)
	}
	return nil
}

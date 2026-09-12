package repo

import (
	"context"
	"errors"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// InboxRepo inbox_items 表（回写收件箱）。
type InboxRepo struct{ db *gorm.DB }

// NewInboxRepo 构造。
func NewInboxRepo(db *gorm.DB) *InboxRepo { return &InboxRepo{db: db} }

// Create 写入一条候选。
func (r *InboxRepo) Create(ctx context.Context, row *domain.InboxItemDO) error {
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return pkg.Wrap(6104, "create inbox item failed", err)
	}
	return nil
}

// GetByID 取单条；不存在返回 domain.ErrInboxNotFound。
func (r *InboxRepo) GetByID(ctx context.Context, id string) (*domain.InboxItemDO, error) {
	var row domain.InboxItemDO
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrInboxNotFound
		}
		return nil, pkg.Wrap(6104, "get inbox item failed", err)
	}
	return &row, nil
}

// ListByStatus 按状态列出（status 空 = 全部；最新在前）。
func (r *InboxRepo) ListByStatus(ctx context.Context, status domain.InboxStatus, limit int) ([]domain.InboxItemDO, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := r.db.WithContext(ctx).Model(&domain.InboxItemDO{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var rows []domain.InboxItemDO
	if err := q.Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(6104, "list inbox items failed", err)
	}
	return rows, nil
}

// SetStatus 更新评审状态。
func (r *InboxRepo) SetStatus(ctx context.Context, id string, status domain.InboxStatus) error {
	res := r.db.WithContext(ctx).Model(&domain.InboxItemDO{}).
		Where("id = ?", id).Update("status", status)
	if res.Error != nil {
		return pkg.Wrap(6104, "update inbox status failed", res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrInboxNotFound
	}
	return nil
}

// CountByStatus 统计某状态条目数。
func (r *InboxRepo) CountByStatus(ctx context.Context, status domain.InboxStatus) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&domain.InboxItemDO{}).
		Where("status = ?", status).Count(&n).Error; err != nil {
		return 0, pkg.Wrap(6104, "count inbox items failed", err)
	}
	return n, nil
}

// FindByKindTitle 按 kind + title 查重（任意状态）；不存在返回 nil。
func (r *InboxRepo) FindByKindTitle(ctx context.Context, kind domain.InboxKind, title string) (*domain.InboxItemDO, error) {
	var row domain.InboxItemDO
	err := r.db.WithContext(ctx).Where("kind = ? AND title = ?", kind, title).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, pkg.Wrap(6104, "find inbox item failed", err)
	}
	return &row, nil
}

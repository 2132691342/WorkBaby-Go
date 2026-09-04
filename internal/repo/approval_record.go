package repo

import (
	"context"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// ApprovalRecordRepo 审批/补充输入记录持久化。
type ApprovalRecordRepo struct{ db *gorm.DB }

// NewApprovalRecordRepo 构造。
func NewApprovalRecordRepo(db *gorm.DB) *ApprovalRecordRepo { return &ApprovalRecordRepo{db: db} }

// Create 落未决记录。
func (r *ApprovalRecordRepo) Create(ctx context.Context, row *domain.ApprovalRecordDO) error {
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return pkg.Wrap(2016, "create approval record failed", err)
	}
	return nil
}

// SetStatus 更新终态（带用户回复，input 类）。
func (r *ApprovalRecordRepo) SetStatus(ctx context.Context, id, status, answer string) error {
	updates := map[string]any{"status": status, "decided_at": time.Now().UnixMilli()}
	if answer != "" {
		updates["answer"] = answer
	}
	if err := r.db.WithContext(ctx).Model(&domain.ApprovalRecordDO{}).
		Where("id = ?", id).Updates(updates).Error; err != nil {
		return pkg.Wrap(2016, "update approval record failed", err)
	}
	return nil
}

// ListPending 未决记录（启动排空/前端恢复视图）。
func (r *ApprovalRecordRepo) ListPending(ctx context.Context) ([]domain.ApprovalRecordDO, error) {
	var rows []domain.ApprovalRecordDO
	if err := r.db.WithContext(ctx).
		Where("status = ?", domain.ApprovalStatusPending).
		Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2016, "list pending approvals failed", err)
	}
	return rows, nil
}

// DeleteBySession 会话删除级联清理。
func (r *ApprovalRecordRepo) DeleteBySession(ctx context.Context, sessionID string) error {
	if err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Delete(&domain.ApprovalRecordDO{}).Error; err != nil {
		return pkg.Wrap(2016, "delete approval records failed", err)
	}
	return nil
}

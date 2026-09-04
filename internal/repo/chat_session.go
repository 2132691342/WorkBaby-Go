package repo

import (
	"context"
	"errors"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// ChatSessionRepo chat_sessions 表 CRUD。
type ChatSessionRepo struct{ db *gorm.DB }

func NewChatSessionRepo(db *gorm.DB) *ChatSessionRepo { return &ChatSessionRepo{db: db} }

func (r *ChatSessionRepo) Create(ctx context.Context, s *domain.ChatSessionDO) error {
	if err := r.db.WithContext(ctx).Create(s).Error; err != nil {
		return pkg.Wrap(2040, "create session failed", err)
	}
	return nil
}

func (r *ChatSessionRepo) Update(ctx context.Context, s *domain.ChatSessionDO) error {
	if err := r.db.WithContext(ctx).Save(s).Error; err != nil {
		return pkg.Wrap(2041, "update session failed", err)
	}
	return nil
}

// SoftDelete 软删（deleted_at=now）；不返回 error（业务层不区分）。
func (r *ChatSessionRepo) SoftDelete(ctx context.Context, id string, atMs int64) error {
	if err := r.db.WithContext(ctx).Model(&domain.ChatSessionDO{}).
		Where("id = ?", id).
		Update("deleted_at", atMs).Error; err != nil {
		return pkg.Wrap(2042, "soft delete session failed", err)
	}
	return nil
}

func (r *ChatSessionRepo) GetByID(ctx context.Context, id string) (*domain.ChatSessionDO, error) {
	var s domain.ChatSessionDO
	if err := r.db.WithContext(ctx).Where("deleted_at = ?", 0).First(&s, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrSessionNotFound
		}
		return nil, pkg.Wrap(2043, "get session failed", err)
	}
	return &s, nil
}

// ListByUser 列出某用户未删除的会话，最近活跃优先。
func (r *ChatSessionRepo) ListByUser(ctx context.Context, userID string) ([]domain.ChatSessionDO, error) {
	var ss []domain.ChatSessionDO
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND deleted_at = ?", userID, 0).
		Order("last_message_at DESC, created_at DESC").
		Find(&ss).Error; err != nil {
		return nil, pkg.Wrap(2044, "list sessions failed", err)
	}
	return ss, nil
}

// CountByUser 某用户未删除会话总数（分页 total）。
func (r *ChatSessionRepo) CountByUser(ctx context.Context, userID string) (int, error) {
	var n int64
	if err := r.db.WithContext(ctx).
		Model(&domain.ChatSessionDO{}).
		Where("user_id = ? AND deleted_at = ?", userID, 0).
		Count(&n).Error; err != nil {
		return 0, pkg.Wrap(2044, "count sessions failed", err)
	}
	return int(n), nil
}

// ListByUserPage 分页列出某用户未删除的会话（page 从 1 起）。
func (r *ChatSessionRepo) ListByUserPage(ctx context.Context, userID string, page, pageSize int) ([]domain.ChatSessionDO, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var ss []domain.ChatSessionDO
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND deleted_at = ?", userID, 0).
		Order("last_message_at DESC, created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&ss).Error; err != nil {
		return nil, pkg.Wrap(2044, "list sessions failed", err)
	}
	return ss, nil
}

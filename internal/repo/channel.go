package repo

import (
	"context"
	"errors"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// ChannelConfigRepo channel_configs 表 CRUD（软删）。
type ChannelConfigRepo struct{ db *gorm.DB }

// NewChannelConfigRepo 构造仓储。
func NewChannelConfigRepo(db *gorm.DB) *ChannelConfigRepo { return &ChannelConfigRepo{db: db} }

// List 全部（含停用，按更新时间倒序）。
func (r *ChannelConfigRepo) List(ctx context.Context) ([]domain.ChannelConfigDO, error) {
	var rows []domain.ChannelConfigDO
	if err := r.db.WithContext(ctx).Order("updated_at DESC").Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2080, "list channels failed", err)
	}
	return rows, nil
}

// GetByID 按 ID 查找；找不到返回 ErrChannelNotFound。
func (r *ChannelConfigRepo) GetByID(ctx context.Context, id string) (*domain.ChannelConfigDO, error) {
	var row domain.ChannelConfigDO
	if err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrChannelNotFound
		}
		return nil, pkg.Wrap(2080, "get channel failed", err)
	}
	return &row, nil
}

// Create 新建。
func (r *ChannelConfigRepo) Create(ctx context.Context, row *domain.ChannelConfigDO) error {
	if row.ID == "" {
		row.ID = pkg.NewID(domain.IDChannel)
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return pkg.Wrap(2081, "create channel failed", err)
	}
	return nil
}

// Update 保存（Save 按主键更新全部字段）。
func (r *ChannelConfigRepo) Update(ctx context.Context, row *domain.ChannelConfigDO) error {
	if err := r.db.WithContext(ctx).Save(row).Error; err != nil {
		return pkg.Wrap(2081, "update channel failed", err)
	}
	return nil
}

// Delete 软删。
func (r *ChannelConfigRepo) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Delete(&domain.ChannelConfigDO{}, "id = ?", id).Error; err != nil {
		return pkg.Wrap(2082, "delete channel failed", err)
	}
	return nil
}

// ChannelMessageLogRepo channel_message_logs 表 CRUD。
type ChannelMessageLogRepo struct{ db *gorm.DB }

// NewChannelMessageLogRepo 构造仓储。
func NewChannelMessageLogRepo(db *gorm.DB) *ChannelMessageLogRepo {
	return &ChannelMessageLogRepo{db: db}
}

// Create 写日志（pending/failed/success 三态由 service 更新）。
func (r *ChannelMessageLogRepo) Create(ctx context.Context, row *domain.ChannelMessageLogDO) error {
	if row.ID == "" {
		row.ID = pkg.NewID(domain.IDChannelLog)
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return pkg.Wrap(2083, "create channel log failed", err)
	}
	return nil
}

// UpdateStatus 更新发送状态（status / errorMessage）。
func (r *ChannelMessageLogRepo) UpdateStatus(ctx context.Context, id, status, errMsg string) error {
	if err := r.db.WithContext(ctx).Model(&domain.ChannelMessageLogDO{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": status, "error_message": errMsg}).Error; err != nil {
		return pkg.Wrap(2083, "update channel log failed", err)
	}
	return nil
}

// ListByChannel 按通道取最近日志（倒序，limit 封顶 200）。
func (r *ChannelMessageLogRepo) ListByChannel(ctx context.Context, channelID string, limit int) ([]domain.ChannelMessageLogDO, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var rows []domain.ChannelMessageLogDO
	if err := r.db.WithContext(ctx).
		Where("channel_id = ?", channelID).
		Order("created_at DESC").
		Limit(limit).Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2084, "list channel logs failed", err)
	}
	return rows, nil
}

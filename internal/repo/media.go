package repo

import (
	"context"
	"errors"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// MediaPresetRepo media_presets 表 CRUD（软删）。
type MediaPresetRepo struct{ db *gorm.DB }

// NewMediaPresetRepo 构造仓储。
func NewMediaPresetRepo(db *gorm.DB) *MediaPresetRepo { return &MediaPresetRepo{db: db} }

// List 全部（可按 kind 过滤，按创建时间倒序）。
func (r *MediaPresetRepo) List(ctx context.Context, kind string) ([]domain.MediaPresetDO, error) {
	q := r.db.WithContext(ctx)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	var rows []domain.MediaPresetDO
	if err := q.Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2095, "list media presets failed", err)
	}
	return rows, nil
}

// GetByID 按 ID 查找；找不到返回 ErrMediaPresetNotFound。
func (r *MediaPresetRepo) GetByID(ctx context.Context, id string) (*domain.MediaPresetDO, error) {
	var row domain.MediaPresetDO
	if err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrMediaPresetNotFound
		}
		return nil, pkg.Wrap(2095, "get media preset failed", err)
	}
	return &row, nil
}

// Create 新建。
func (r *MediaPresetRepo) Create(ctx context.Context, row *domain.MediaPresetDO) error {
	if row.ID == "" {
		row.ID = pkg.NewID(domain.IDMediaPreset)
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return pkg.Wrap(2096, "create media preset failed", err)
	}
	return nil
}

// Update 保存。
func (r *MediaPresetRepo) Update(ctx context.Context, row *domain.MediaPresetDO) error {
	if err := r.db.WithContext(ctx).Save(row).Error; err != nil {
		return pkg.Wrap(2096, "update media preset failed", err)
	}
	return nil
}

// Delete 软删。
func (r *MediaPresetRepo) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Delete(&domain.MediaPresetDO{}, "id = ?", id).Error; err != nil {
		return pkg.Wrap(2097, "delete media preset failed", err)
	}
	return nil
}

// ClearDefault 清空某 kind 的 isDefault（激活新默认前调用）。
func (r *MediaPresetRepo) ClearDefault(ctx context.Context, kind domain.MediaKind) error {
	if err := r.db.WithContext(ctx).Model(&domain.MediaPresetDO{}).
		Where("kind = ?", kind).Update("is_default", false).Error; err != nil {
		return pkg.Wrap(2096, "clear media preset default failed", err)
	}
	return nil
}

// MediaArtifactRepo media_artifacts 表 CRUD。
type MediaArtifactRepo struct{ db *gorm.DB }

// NewMediaArtifactRepo 构造仓储。
func NewMediaArtifactRepo(db *gorm.DB) *MediaArtifactRepo { return &MediaArtifactRepo{db: db} }

// List 最近产物（倒序，limit 封顶 200）。
func (r *MediaArtifactRepo) List(ctx context.Context, limit int) ([]domain.MediaArtifactDO, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	var rows []domain.MediaArtifactDO
	if err := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, pkg.Wrap(2098, "list media artifacts failed", err)
	}
	return rows, nil
}

// GetByID 按 ID 查找；找不到返回 ErrMediaNotFound。
func (r *MediaArtifactRepo) GetByID(ctx context.Context, id string) (*domain.MediaArtifactDO, error) {
	var row domain.MediaArtifactDO
	if err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrMediaNotFound
		}
		return nil, pkg.Wrap(2098, "get media artifact failed", err)
	}
	return &row, nil
}

// Create 新建。
func (r *MediaArtifactRepo) Create(ctx context.Context, row *domain.MediaArtifactDO) error {
	if row.ID == "" {
		row.ID = pkg.NewID(domain.IDMedia)
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return pkg.Wrap(2099, "create media artifact failed", err)
	}
	return nil
}

// Delete 物理删除行（产物文件由 service 级联清理）。
func (r *MediaArtifactRepo) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Delete(&domain.MediaArtifactDO{}, "id = ?", id).Error; err != nil {
		return pkg.Wrap(2097, "delete media artifact failed", err)
	}
	return nil
}

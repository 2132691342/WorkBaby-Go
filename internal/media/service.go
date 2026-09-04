package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
)

// Service 媒体编排：预设 CRUD + 生成落盘落库 + 产物查询删除。
type Service struct {
	presetRepo   *repo.MediaPresetRepo
	artifactRepo *repo.MediaArtifactRepo
	gen          Generator
	mediaDir     string // {home}/media
}

// NewService 构造；gen 为生成器（v1 离线占位），mediaDir 为产物根目录。
func NewService(presetRepo *repo.MediaPresetRepo, artifactRepo *repo.MediaArtifactRepo, gen Generator, mediaDir string) *Service {
	return &Service{presetRepo: presetRepo, artifactRepo: artifactRepo, gen: gen, mediaDir: mediaDir}
}

// ListPresets 列出预设（可按 kind 过滤）。
func (s *Service) ListPresets(ctx context.Context, kind string) ([]domain.MediaPresetRESP, error) {
	rows, err := s.presetRepo.List(ctx, kind)
	if err != nil {
		return nil, err
	}
	out := make([]domain.MediaPresetRESP, 0, len(rows))
	for i := range rows {
		out = append(out, toPresetRESP(&rows[i]))
	}
	return out, nil
}

// CreatePreset 新建预设。
func (s *Service) CreatePreset(ctx context.Context, req domain.MediaPresetREQ) (domain.MediaPresetRESP, error) {
	if req.Name == "" || req.Kind == "" || req.Backend == "" {
		return domain.MediaPresetRESP{}, pkg.New(9000, "name/kind/backend required", "")
	}
	row := &domain.MediaPresetDO{
		Name:       req.Name,
		Kind:       req.Kind,
		Backend:    req.Backend,
		Model:      req.Model,
		ParamsJSON: req.ParamsJSON,
		IsDefault:  req.IsDefault,
		Enabled:    enabledOf(req.Enabled),
	}
	if row.IsDefault {
		if err := s.presetRepo.ClearDefault(ctx, row.Kind); err != nil {
			return domain.MediaPresetRESP{}, err
		}
	}
	if err := s.presetRepo.Create(ctx, row); err != nil {
		return domain.MediaPresetRESP{}, err
	}
	return toPresetRESP(row), nil
}

// UpdatePreset 更新预设。
func (s *Service) UpdatePreset(ctx context.Context, id string, req domain.MediaPresetREQ) (domain.MediaPresetRESP, error) {
	row, err := s.presetRepo.GetByID(ctx, id)
	if err != nil {
		return domain.MediaPresetRESP{}, err
	}
	if req.Name != "" {
		row.Name = req.Name
	}
	if req.Kind != "" {
		row.Kind = req.Kind
	}
	if req.Backend != "" {
		row.Backend = req.Backend
	}
	if req.Model != "" {
		row.Model = req.Model
	}
	if req.ParamsJSON != "" {
		row.ParamsJSON = req.ParamsJSON
	}
	if req.IsDefault {
		if err := s.presetRepo.ClearDefault(ctx, row.Kind); err != nil {
			return domain.MediaPresetRESP{}, err
		}
		row.IsDefault = true
	}
	if req.Enabled != nil {
		row.Enabled = *req.Enabled
	}
	if err := s.presetRepo.Update(ctx, row); err != nil {
		return domain.MediaPresetRESP{}, err
	}
	return toPresetRESP(row), nil
}

// DeletePreset 删除预设。
func (s *Service) DeletePreset(ctx context.Context, id string) error {
	if _, err := s.presetRepo.GetByID(ctx, id); err != nil {
		return err
	}
	return s.presetRepo.Delete(ctx, id)
}

// ActivatePreset 设为 kind 默认（同 kind 其他预设清除默认）。
func (s *Service) ActivatePreset(ctx context.Context, id string) (domain.MediaPresetRESP, error) {
	row, err := s.presetRepo.GetByID(ctx, id)
	if err != nil {
		return domain.MediaPresetRESP{}, err
	}
	if err := s.presetRepo.ClearDefault(ctx, row.Kind); err != nil {
		return domain.MediaPresetRESP{}, err
	}
	row.IsDefault = true
	if err := s.presetRepo.Update(ctx, row); err != nil {
		return domain.MediaPresetRESP{}, err
	}
	return toPresetRESP(row), nil
}

// Generate 生成媒体产物：生成 → 落盘 {home}/media/{YYYY-MM}/{id}.{ext} → 落库。
func (s *Service) Generate(ctx context.Context, req domain.MediaGenerateREQ) (domain.MediaArtifactRESP, error) {
	if req.Prompt == "" {
		return domain.MediaArtifactRESP{}, pkg.New(9000, "prompt required", "")
	}
	params := map[string]any{"kind": string(req.Kind)}
	if req.PresetID != "" {
		preset, err := s.presetRepo.GetByID(ctx, req.PresetID)
		if err != nil {
			return domain.MediaArtifactRESP{}, err
		}
		if preset.ParamsJSON != "" {
			var extra map[string]any
			if json.Unmarshal([]byte(preset.ParamsJSON), &extra) == nil {
				for k, v := range extra {
					params[k] = v
				}
			}
		}
	}
	gen, err := s.gen.Generate(ctx, req.Prompt, params)
	if err != nil {
		return domain.MediaArtifactRESP{}, err
	}
	id := pkg.NewID(domain.IDMedia)
	relDir := time.Now().Format("2006-01")
	dir := filepath.Join(s.mediaDir, relDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return domain.MediaArtifactRESP{}, pkg.Wrap(9006, "mkdir media dir failed", err)
	}
	filePath := filepath.Join(dir, id+"."+gen.Ext)
	if err := os.WriteFile(filePath, gen.Bytes, 0o644); err != nil {
		return domain.MediaArtifactRESP{}, pkg.Wrap(9006, "write media file failed", err)
	}
	row := &domain.MediaArtifactDO{
		ID:       id,
		Kind:     req.Kind,
		PresetID: req.PresetID,
		Prompt:   req.Prompt,
		FilePath: filePath,
		MimeType: gen.MimeType,
		Width:    gen.Width,
		Height:   gen.Height,
		FileSize: int64(len(gen.Bytes)),
	}
	if err := s.artifactRepo.Create(ctx, row); err != nil {
		return domain.MediaArtifactRESP{}, err
	}
	return toArtifactRESP(row), nil
}

// ListArtifacts 最近产物。
func (s *Service) ListArtifacts(ctx context.Context, limit int) ([]domain.MediaArtifactRESP, error) {
	rows, err := s.artifactRepo.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]domain.MediaArtifactRESP, 0, len(rows))
	for i := range rows {
		out = append(out, toArtifactRESP(&rows[i]))
	}
	return out, nil
}

// FindArtifact 按 ID 查产物（AssetServer 预览用）。
func (s *Service) FindArtifact(ctx context.Context, id string) (*domain.MediaArtifactDO, error) {
	return s.artifactRepo.GetByID(ctx, id)
}

// DeleteArtifact 删除产物（级联删磁盘文件）。
func (s *Service) DeleteArtifact(ctx context.Context, id string) error {
	row, err := s.artifactRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if row.FilePath != "" {
		_ = os.Remove(row.FilePath)
	}
	return s.artifactRepo.Delete(ctx, id)
}

func toPresetRESP(d *domain.MediaPresetDO) domain.MediaPresetRESP {
	return domain.MediaPresetRESP{
		ID: d.ID, Name: d.Name, Kind: d.Kind, Backend: d.Backend,
		Model: d.Model, ParamsJSON: d.ParamsJSON, IsDefault: d.IsDefault,
		Enabled: d.Enabled, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

// toArtifactRESP 产物出参；url 经 AssetServer /files/media/{id} 暴露（main.go Handler 拦截）。
func toArtifactRESP(d *domain.MediaArtifactDO) domain.MediaArtifactRESP {
	return domain.MediaArtifactRESP{
		ID:          d.ID,
		Kind:        d.Kind,
		PresetID:    d.PresetID,
		Prompt:      d.Prompt,
		PreviewURL:  "/files/media/" + d.ID,
		DownloadURL: "/files/media/" + d.ID + "?dl=1",
		MimeType:    d.MimeType,
		Width:       d.Width,
		Height:      d.Height,
		FileSize:    d.FileSize,
		CreatedAt:   d.CreatedAt,
	}
}

func enabledOf(v *bool) bool {
	if v == nil {
		return true
	}
	return *v
}

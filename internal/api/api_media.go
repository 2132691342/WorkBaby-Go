package api

import (
	"WorkBaby/internal/domain"
)

// ListMediaPresets 媒体预设列表（kind 可选过滤）。
func (h *Handler) ListMediaPresets(kind string) ([]domain.MediaPresetRESP, error) {
	return h.mediaSvc.ListPresets(h.ctx, kind)
}

// CreateMediaPreset 新建预设。
func (h *Handler) CreateMediaPreset(req domain.MediaPresetREQ) (domain.MediaPresetRESP, error) {
	return h.mediaSvc.CreatePreset(h.ctx, req)
}

// UpdateMediaPreset 更新预设（POST /media/presets/update/{id}）。
func (h *Handler) UpdateMediaPreset(id string, req domain.MediaPresetREQ) (domain.MediaPresetRESP, error) {
	return h.mediaSvc.UpdatePreset(h.ctx, id, req)
}

// DeleteMediaPreset 删除预设（POST /media/presets/delete/{id}）。
func (h *Handler) DeleteMediaPreset(id string) (map[string]any, error) {
	if err := h.mediaSvc.DeletePreset(h.ctx, id); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "deleted": true}, nil
}

// ActivateMediaPreset 激活预设为默认（POST /media/presets/activate/{id}）。
func (h *Handler) ActivateMediaPreset(id string) (domain.MediaPresetRESP, error) {
	return h.mediaSvc.ActivatePreset(h.ctx, id)
}

// GenerateMedia 生成媒体产物。
func (h *Handler) GenerateMedia(req domain.MediaGenerateREQ) (domain.MediaArtifactRESP, error) {
	return h.mediaSvc.Generate(h.ctx, req)
}

// ListMediaArtifacts 最近产物。
func (h *Handler) ListMediaArtifacts(limit int) ([]domain.MediaArtifactRESP, error) {
	return h.mediaSvc.ListArtifacts(h.ctx, limit)
}

// DeleteMediaArtifact 删除产物（POST /media/artifacts/delete/{id}）。
func (h *Handler) DeleteMediaArtifact(id string) (map[string]any, error) {
	if err := h.mediaSvc.DeleteArtifact(h.ctx, id); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "deleted": true}, nil
}

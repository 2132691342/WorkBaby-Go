package api

import (
	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// 桌宠窗口尺寸（需容纳 pet-card ~216px + 悬浮余量）。
const (
	petWidth  = 260
	petHeight = 300
)

// 主窗口最小尺寸（与 main.go options.MinWidth/MinHeight 保持一致，退出桌宠态时还原）。
const (
	mainMinWidth  = 960
	mainMinHeight = 640
)

// GetPetConfig 桌宠配置。
func (h *Handler) GetPetConfig() (domain.PetConfigRESP, error) {
	return h.petSvc.GetConfig(h.ctx)
}

// UpdatePetConfig 保存桌宠配置（POST /pet/config/update）。
func (h *Handler) UpdatePetConfig(req domain.PetConfigREQ) (domain.PetConfigRESP, error) {
	return h.petSvc.UpdateConfig(h.ctx, req)
}

// ListPetSprites 桌宠 sprite 列表。
func (h *Handler) ListPetSprites() ([]domain.PetSpriteRESP, error) {
	return h.petSvc.ListSprites(h.ctx)
}

// CreatePetSprite 注册内置/占位 sprite。
func (h *Handler) CreatePetSprite(req domain.PetSpriteREQ) (domain.PetSpriteRESP, error) {
	return h.petSvc.CreateSprite(h.ctx, req)
}

// DeletePetSprite 删除 sprite（内置拒绝）。
func (h *Handler) DeletePetSprite(id string) (map[string]any, error) {
	if err := h.petSvc.DeleteSprite(h.ctx, id); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "deleted": true}, nil
}

// UploadPetSprite 上传本地图片文件为 sprite（name + 本地路径，前端经文件对话框选路径）。
func (h *Handler) UploadPetSprite(name string, srcPath string) (domain.PetSpriteRESP, error) {
	return h.petSvc.UploadSprite(h.ctx, name, srcPath)
}

// GetPetState 当前桌宠状态。
func (h *Handler) GetPetState() (string, error) {
	if h.petCtrl == nil {
		return "idle", nil
	}
	return string(h.petCtrl.Current()), nil
}

// PetMode 返回当前是否桌宠形态（server/router 用）。
func (h *Handler) PetMode() bool { return h.petMode }

// PetToggleMode 主窗口 ⇄ 桌宠形态切换（位置记忆）。
func (h *Handler) PetToggleMode() (map[string]any, error) {
	if h.ctx == nil {
		return nil, pkg.New(9405, "window not ready", "")
	}
	if h.petMode {
		// 退出桌宠态：还原尺寸/位置/最小尺寸/置顶，恢复普通窗口行为
		h.petX, h.petY = wruntime.WindowGetPosition(h.ctx)
		wruntime.WindowSetSize(h.ctx, h.mainW, h.mainH)
		wruntime.WindowSetPosition(h.ctx, h.mainX, h.mainY)
		wruntime.WindowSetMinSize(h.ctx, mainMinWidth, mainMinHeight)
		wruntime.WindowSetAlwaysOnTop(h.ctx, false)
		h.petMode = false
		wruntime.EventsEmit(h.ctx, "pet:hide", nil)
	} else {
		h.mainX, h.mainY = wruntime.WindowGetPosition(h.ctx)
		h.mainW, h.mainH = wruntime.WindowGetSize(h.ctx)
		wruntime.WindowSetSize(h.ctx, petWidth, petHeight)
		wruntime.WindowSetPosition(h.ctx, h.petX, h.petY)
		wruntime.WindowSetMinSize(h.ctx, petWidth, petHeight)
		wruntime.WindowSetAlwaysOnTop(h.ctx, true)
		h.petMode = true
		wruntime.EventsEmit(h.ctx, "pet:show", nil)
	}
	return map[string]any{"mode": petModeName(h.petMode)}, nil
}

// PetMove 桌宠拖拽：相对位移。
func (h *Handler) PetMove(dx, dy int) (map[string]any, error) {
	if h.ctx == nil {
		return nil, pkg.New(9405, "window not ready", "")
	}
	x, y := wruntime.WindowGetPosition(h.ctx)
	nx, ny := x+dx, y+dy
	wruntime.WindowSetPosition(h.ctx, nx, ny)
	if h.petMode {
		h.petX, h.petY = nx, ny
	}
	return map[string]any{"x": nx, "y": ny}, nil
}

func petModeName(petMode bool) string {
	if petMode {
		return "pet"
	}
	return "main"
}

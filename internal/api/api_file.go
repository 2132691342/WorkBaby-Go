package api

import (
	"net/url"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// SearchFiles 按文件名模糊搜索。
func (h *Handler) SearchFiles(q string) ([]domain.FileRESP, error) {
	return h.fileSvc.Search(h.ctx, q)
}

// ListFiles 托管目录全部文件。
func (h *Handler) ListFiles() ([]domain.FileRESP, error) {
	return h.fileSvc.ListFiles(h.ctx)
}

// UploadFile 上传本地文件（srcPath 由前端文件对话框选路径；复制到托管目录）。
func (h *Handler) UploadFile(name, srcPath, sessionID, folderID string) (domain.FileRESP, error) {
	if srcPath == "" {
		return domain.FileRESP{}, pkg.New(1203, "source path required", "")
	}
	return h.fileSvc.Upload(h.ctx, name, srcPath, sessionID, folderID)
}

// DeleteFile 删除文件（磁盘 + 记录）。
func (h *Handler) DeleteFile(id string) (map[string]any, error) {
	if err := h.fileSvc.Delete(h.ctx, id); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "deleted": true}, nil
}

// GetFilePreviewURL 生成预览 URL（背景图等场景；AssetServer /files/files/{id}）。
func (h *Handler) GetFilePreviewURL(id string) (map[string]any, error) {
	if _, err := h.fileSvc.Get(h.ctx, id); err != nil {
		return nil, err
	}
	return map[string]any{"url": "/files/files/" + id}, nil
}

// ListWorkspaceFiles 会话工作区文件面板列表。
func (h *Handler) ListWorkspaceFiles(sessionID string) ([]domain.WorkspaceFileItem, error) {
	return h.workspaceSvc.ListFiles(h.ctx, sessionID)
}

// ReadWorkspaceFile 读取工作区单文件内容（沙箱校验；返回 base64 便于绑定跨边界传输）。
func (h *Handler) ReadWorkspaceFile(sessionID, path string) (string, error) {
	bs, _, err := h.workspaceSvc.ReadFile(h.ctx, sessionID, path)
	if err != nil {
		return "", err
	}
	return url.PathEscape(string(bs)), nil
}

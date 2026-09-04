package api

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"

	"WorkBaby/internal/media"
	"WorkBaby/internal/pet"
	"WorkBaby/internal/service"
)

// FileServer 服务本地受管文件（main.go AssetServer 转发 /files/**）：
// /files/media/{id} → 媒体产物；/files/sprites/{id} → 桌宠 sprite；
// /files/files/{id} → 托管文件（files 表）；/files/workspace/{sessionId}?path= → 会话工作区文件。
// 均按 id/sessionId+path 查表或沙箱校验，不暴露任意路径。
//
// 刻意不挂在 Handler 上：Handler 是 Wails 绑定类型，http.ResponseWriter / http.Request
// 一旦出现在绑定方法签名里，Wails 会把 net/http 类型闭包（含 multipart）生成进 models.ts，
// 产出 `FileHeader[]` 这类非法 TS 表达式，导致前端类型检查失败。
type FileServer struct {
	ctx          context.Context
	fileSvc      *service.FileService
	workspaceSvc *service.WorkspaceService
	mediaSvc     *media.Service
	petSvc       *pet.Service
}

func (f *FileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/files/media/"):
		f.serveMedia(w, r, strings.TrimPrefix(r.URL.Path, "/files/media/"))
	case strings.HasPrefix(r.URL.Path, "/files/sprites/"):
		f.serveSprite(w, r, strings.TrimPrefix(r.URL.Path, "/files/sprites/"))
	case strings.HasPrefix(r.URL.Path, "/files/files/"):
		f.serveManagedFile(w, r, strings.TrimPrefix(r.URL.Path, "/files/files/"))
	case strings.HasPrefix(r.URL.Path, "/files/workspace/"):
		f.serveWorkspaceFile(w, r, strings.TrimPrefix(r.URL.Path, "/files/workspace/"))
	default:
		http.NotFound(w, r)
	}
}

func (f *FileServer) serveManagedFile(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" || strings.Contains(id, "/") || f.fileSvc == nil || f.ctx == nil {
		http.NotFound(w, r)
		return
	}
	path, err := f.fileSvc.DiskPath(f.ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}

func (f *FileServer) serveWorkspaceFile(w http.ResponseWriter, r *http.Request, sessionID string) {
	if sessionID == "" || strings.Contains(sessionID, "/") || f.workspaceSvc == nil || f.ctx == nil {
		http.NotFound(w, r)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	bs, contentType, err := f.workspaceSvc.ReadFile(f.ctx, sessionID, path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if r.URL.Query().Get("dl") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(path)+`"`)
	}
	_, _ = w.Write(bs)
}

func (f *FileServer) serveMedia(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" || strings.Contains(id, "/") || f.mediaSvc == nil || f.ctx == nil {
		http.NotFound(w, r)
		return
	}
	row, err := f.mediaSvc.FindArtifact(f.ctx, id)
	if err != nil || row.FilePath == "" {
		http.NotFound(w, r)
		return
	}
	if r.URL.Query().Get("dl") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+id+`"`)
	}
	http.ServeFile(w, r, row.FilePath)
}

func (f *FileServer) serveSprite(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" || strings.Contains(id, "/") || f.petSvc == nil || f.ctx == nil {
		http.NotFound(w, r)
		return
	}
	row, err := f.petSvc.FindSprite(f.ctx, id)
	if err != nil || row.FilePath == "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, row.FilePath)
}

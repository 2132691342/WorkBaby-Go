package service

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// hiddenWorkspacePrefixes 工作区文件面板隐藏的内部目录。
var hiddenWorkspacePrefixes = []string{"local/", ".workbaby/", ".index/"}

// WorkspaceService 会话工作区文件面板：列目录 + 读取（沙箱）+ 内容类型推断。
type WorkspaceService struct {
	workspacesRoot string // {home}/workspaces
}

func NewWorkspaceService(workspacesRoot string) *WorkspaceService {
	return &WorkspaceService{workspacesRoot: workspacesRoot}
}

// Dir 返回某会话的工作区根目录。
func (s *WorkspaceService) Dir(sessionID string) string {
	return filepath.Join(s.workspacesRoot, sessionID)
}

// ListFiles 递归列出工作区全部文件（相对路径排序；过滤内部隐藏目录）。
func (s *WorkspaceService) ListFiles(_ context.Context, sessionID string) ([]domain.WorkspaceFileItem, error) {
	root := s.Dir(sessionID)
	if _, err := os.Stat(root); err != nil {
		return []domain.WorkspaceFileItem{}, nil // 目录不存在 = 空工作区
	}
	var out []domain.WorkspaceFileItem
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 单项失败跳过，不阻断整个面板
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isHiddenWorkspacePath(rel) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		out = append(out, domain.WorkspaceFileItem{
			Path:       rel,
			Name:       d.Name(),
			Ext:        extWithDot(d.Name()),
			Kind:       workspaceKind(d.Name()),
			Size:       info.Size(),
			URL:        "/files/workspace/" + sessionID + "?path=" + rel,
			ModifiedAt: info.ModTime().UnixMilli(),
		})
		return nil
	})
	if err != nil {
		return nil, pkg.Wrap(1210, "list workspace files failed", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// ReadFile 读取工作区单文件内容（路径沙箱防穿越）。
func (s *WorkspaceService) ReadFile(_ context.Context, sessionID, path string) ([]byte, string, error) {
	root := s.Dir(sessionID)
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := pkg.Within(root, full); err != nil {
		return nil, "", err
	}
	bs, err := os.ReadFile(full)
	if err != nil {
		return nil, "", pkg.Wrap(1211, "read workspace file failed", err)
	}
	return bs, workspaceContentType(path), nil
}

// workspaceKind 按扩展名分类（对齐前端 WorkspaceFile.kind）。
func workspaceKind(name string) string {
	switch extWithDot(name) {
	case ".html", ".htm":
		return "html"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return "image"
	case ".pdf":
		return "pdf"
	case ".md", ".txt", ".json", ".csv", ".yaml", ".yml", ".xml",
		".js", ".ts", ".java", ".py", ".go", ".rs", ".css", ".log":
		return "text"
	case ".mp4", ".mov", ".webm":
		return "video"
	default:
		return "file"
	}
}

// workspaceContentType 按扩展名推断 Content-Type（预览/内联渲染）。
func workspaceContentType(path string) string {
	switch extWithDot(path) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".md", ".txt", ".json", ".csv", ".yaml", ".yml", ".xml",
		".js", ".ts", ".java", ".py", ".go", ".rs", ".css", ".log":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// extWithDot 返回含点小写扩展名（如 ".html"）。
func extWithDot(name string) string {
	return strings.ToLower(filepath.Ext(name))
}

// isHiddenWorkspacePath 过滤内部隐藏目录前缀。
func isHiddenWorkspacePath(rel string) bool {
	for _, p := range hiddenWorkspacePrefixes {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

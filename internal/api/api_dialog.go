package api

import (
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// OpenFileDialog 单选文件，返回选中绝对路径；用户取消返回空串。
//
// Wails v2 生成的 JS runtime 不含 dialog 绑定（runtime.js / runtime.d.ts 均无），
// 故在此暴露——api 层是唯一允许 import wails runtime 的层。
// 用扁平参数而非 struct 入参：Wails 为 struct 生成的 TS 是带 convertValues 的 class，
// 前端无法直接用对象字面量传参。
func (h *Handler) OpenFileDialog(title, pattern string) (string, error) {
	opts := wruntime.OpenDialogOptions{Title: title}
	if pattern != "" {
		opts.Filters = []wruntime.FileFilter{{DisplayName: pattern, Pattern: pattern}}
	}
	return wruntime.OpenFileDialog(h.ctx, opts)
}

// OpenDirectoryDialog 选目录，返回绝对路径；用户取消返回空串。
// 供会话工作区绑定（WorkspacePickerDialog）调原生壳，避免让用户手敲绝对路径。
func (h *Handler) OpenDirectoryDialog(title, defaultPath string) (string, error) {
	return wruntime.OpenDirectoryDialog(h.ctx, wruntime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: defaultPath,
	})
}

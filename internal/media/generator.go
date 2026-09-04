// Package media 提供媒体生成：统一 Generator 接口 + 产物落盘落库 + 预设管理。
// v1 为离线占位生成（image/video/audio/model3d/vfx）；不依赖 Wails runtime。
package media

import (
	"context"

	"WorkBaby/internal/domain"
)

// Generator 媒体生成器；按 kind 注册。
type Generator interface {
	// Kind 返回支持的模态。
	Kind() domain.MediaKind
	// Generate 生成媒体字节（prompt + 参数）；生成失败返回带 9001 的错误。
	Generate(ctx context.Context, prompt string, params map[string]any) (*Generated, error)
}

// Generated 生成结果（字节 + 元数据）。
type Generated struct {
	Bytes    []byte
	MimeType string
	Ext      string
	Width    *int
	Height   *int
}

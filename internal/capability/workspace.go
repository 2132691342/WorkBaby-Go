package capability

import (
	"context"
	"strings"

	"WorkBaby/internal/harness"
	"WorkBaby/internal/tool"
)

// workspaceCap 注入会话绑定的工作目录，让「当前项目 / 这个目录」类指令有明确落点。
type workspaceCap struct{}

// NewWorkspace 构造工作区能力。
func NewWorkspace() Capability { return &workspaceCap{} }

func (c *workspaceCap) ID() string { return "workspace" }

func (c *workspaceCap) Preload(_ context.Context, p *PreloadCtx) ([]harness.ContextPiece, error) {
	if p.Session == nil {
		return nil, nil
	}
	wp := strings.TrimSpace(p.Session.WorkspacePath)
	if wp == "" {
		return nil, nil
	}
	return []harness.ContextPiece{{
		Key:   "workspace",
		Title: "当前工作目录",
		Body: "本会话绑定的本地工作目录：" + wp + "\n" +
			"file_read / file_write / file_list / doc_reader / archive 等文件工具与 exec 命令默认在此目录下工作，相对路径均基于此目录解析。" +
			"用户提到「当前项目 / 当前工作区 / 这个目录」时即指此处。",
	}}, nil
}

func (c *workspaceCap) Tools() []tool.Tool { return nil }

func (c *workspaceCap) Capture(_ context.Context, _ *CaptureCtx) error { return nil }

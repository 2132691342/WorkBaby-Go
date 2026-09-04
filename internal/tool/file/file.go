// Package file 提供文件读写/列目录工具；强制工作区根白名单 + 敏感目标屏蔽。
package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"WorkBaby/internal/pkg"
	"WorkBaby/internal/tool"
)

// maxWriteBytes file_write 单次写入上限。
const maxWriteBytes = 2 << 20 // 2MB

// 敏感目录/文件（无论是否在工作区内都拒绝）。
var sensitiveSegments = []string{
	".git", ".env", "config.yaml", string(filepath.Separator) + "db" + string(filepath.Separator),
}

// safePath 基于 workspaceRoot 校验并绝对化路径。
//
// 规则：相对路径拼到 root；绝对路径必须在 root 内（防 ../ 穿越）；路径段命中敏感目标拒绝。
func safePath(workspaceRoot, path string) (string, error) {
	if path == "" {
		return "", tool.ErrPathEscape
	}
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", pkg.Wrap(4007, "abs root failed", err)
	}
	candidate := path
	if !filepath.IsAbs(path) {
		candidate = filepath.Join(root, path)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", pkg.Wrap(4007, "abs path failed", err)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", tool.ErrPathEscape
	}
	clean := filepath.Clean(abs)
	for _, seg := range strings.Split(clean, string(filepath.Separator)) {
		lower := strings.ToLower(seg)
		for _, sensitive := range sensitiveSegments {
			if strings.Trim(strings.ToLower(sensitive), string(filepath.Separator)) == lower {
				return "", pkg.New(4007, "path hits sensitive target", abs)
			}
		}
	}
	return abs, nil
}

// ===== file_read =====

// ReadTool 读文件。
type ReadTool struct{ root string }

// NewRead 构造 file_read。
func NewRead(root string) *ReadTool { return &ReadTool{root: root} }

func (t *ReadTool) Name() string              { return "file_read" }
func (t *ReadTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }
func (t *ReadTool) Description() string {
	return "读取工作区内文本文件，返回内容。"
}

func (t *ReadTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["path"],
			"properties": {
				"path": {"type": "string", "description": "工作区内文件路径（绝对或相对）"},
				"maxLines": {"type": "integer", "description": "最多返回行数，缺省不限"}
			}
		}`),
	}
}

type readReq struct {
	Path     string `json:"path"`
	MaxLines int    `json:"maxLines"`
}

func (t *ReadTool) Execute(_ context.Context, args json.RawMessage) tool.ToolResult {
	var req readReq
	if err := json.Unmarshal(args, &req); err != nil {
		return tool.ToolResult{Err: pkg.Wrap(4004, "file_read args parse failed", err)}
	}
	p, err := safePath(t.root, req.Path)
	if err != nil {
		return tool.ToolResult{Err: err}
	}
	bs, err := os.ReadFile(p)
	if err != nil {
		return tool.ToolResult{Err: pkg.Wrap(4007, "read file failed", err)}
	}
	content := string(bs)
	if req.MaxLines > 0 {
		lines := strings.Split(content, "\n")
		if len(lines) > req.MaxLines {
			content = strings.Join(lines[:req.MaxLines], "\n") + "\n... (truncated)"
		}
	}
	return tool.ToolResult{Content: content}
}

// ===== file_write =====

// Recorder 记录一次写操作的变更（装配方注入； 文件变更与回滚点）。
//
// 变更追踪是旁路能力：写入成功即返回，记录失败/未注入都不影响工具语义。
// session/run 身份由装配方从 ctx 解析——file 包不感知 harness。
type Recorder interface {
	RecordWrite(ctx context.Context, path string, existed bool, before, after []byte)
}

// WriteTool 写文件。
type WriteTool struct {
	root     string
	recorder Recorder
}

// NewWrite 构造 file_write。
func NewWrite(root string) *WriteTool { return &WriteTool{root: root} }

// WithRecorder 注入变更记录器（写前备份 + diff 登记）。
func (t *WriteTool) WithRecorder(r Recorder) *WriteTool {
	t.recorder = r
	return t
}

func (t *WriteTool) Name() string              { return "file_write" }
func (t *WriteTool) RiskLevel() tool.RiskLevel { return tool.RiskWriteLocal }
func (t *WriteTool) Description() string {
	return "写入/覆盖工作区内文本文件；单次最大 2MB。"
}

func (t *WriteTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["path", "content"],
			"properties": {
				"path": {"type": "string", "description": "工作区内文件路径"},
				"content": {"type": "string", "description": "文件内容"},
				"append": {"type": "boolean", "description": "true=追加，缺省覆盖"}
			}
		}`),
	}
}

type writeReq struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Append  bool   `json:"append"`
}

func (t *WriteTool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	var req writeReq
	if err := json.Unmarshal(args, &req); err != nil {
		return tool.ToolResult{Err: pkg.Wrap(4004, "file_write args parse failed", err)}
	}
	if len([]byte(req.Content)) > maxWriteBytes {
		return tool.ToolResult{Err: pkg.New(4008, "file_write content too large", "")}
	}
	p, err := safePath(t.root, req.Path)
	if err != nil {
		return tool.ToolResult{Err: err}
	}
	// 写前读原内容：供变更追踪生成快照与 diff（仅在注入 recorder 时读，避免无谓 IO）
	var before []byte
	existed := false
	if t.recorder != nil {
		if bs, rerr := os.ReadFile(p); rerr == nil {
			before = bs
			existed = true
		}
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if req.Append {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := os.OpenFile(p, flag, 0o644)
	if err != nil {
		return tool.ToolResult{Err: pkg.Wrap(4007, "open file failed", err)}
	}
	if _, err := f.WriteString(req.Content); err != nil {
		_ = f.Close()
		return tool.ToolResult{Err: pkg.Wrap(4007, "write file failed", err)}
	}
	_ = f.Close()
	if t.recorder != nil {
		after, _ := os.ReadFile(p)
		t.recorder.RecordWrite(ctx, p, existed, before, after)
	}
	return tool.ToolResult{Content: "written", Meta: map[string]string{"path": p}}
}

// ===== file_list =====

// ListTool 列目录。
type ListTool struct{ root string }

// NewList 构造 file_list。
func NewList(root string) *ListTool { return &ListTool{root: root} }

func (t *ListTool) Name() string              { return "file_list" }
func (t *ListTool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }
func (t *ListTool) Description() string {
	return "列工作区目录下的条目（文件名 + 目录标记）。"
}

func (t *ListTool) Schema() tool.ToolSchema {
	return tool.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "工作区目录路径，缺省根目录"},
				"pattern": {"type": "string", "description": "文件通配模式，如 *.go"}
			}
		}`),
	}
}

type listReq struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
}

func (t *ListTool) Execute(_ context.Context, args json.RawMessage) tool.ToolResult {
	var req listReq
	if err := json.Unmarshal(args, &req); err != nil {
		return tool.ToolResult{Err: pkg.Wrap(4004, "file_list args parse failed", err)}
	}
	if req.Path == "" {
		req.Path = "."
	}
	p, err := safePath(t.root, req.Path)
	if err != nil {
		return tool.ToolResult{Err: err}
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return tool.ToolResult{Err: pkg.Wrap(4007, "list dir failed", err)}
	}
	var sb strings.Builder
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		if req.Pattern != "" {
			ok, _ := filepath.Match(req.Pattern, e.Name())
			if !ok {
				continue
			}
		}
		sb.WriteString(name)
		sb.WriteString("\n")
	}
	return tool.ToolResult{Content: strings.TrimRight(sb.String(), "\n")}
}

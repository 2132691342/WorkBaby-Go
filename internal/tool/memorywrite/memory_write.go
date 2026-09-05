// Package memorywrite 提供 memory_write 工具：模型主动把需要长期保留的信息写入记忆。
//
// 与自动形成（run 后沉淀）互补：自动形成覆盖「这段对话值得记住什么」，
// 本工具覆盖「用户明确要求记住什么」「模型推断出的稳定偏好」。
package memorywrite

import (
	"context"
	"encoding/json"

	"WorkBaby/internal/memory"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/tool"
)

// Writer 记忆写入契约（memory.Service 实现）。
type Writer interface {
	AppendLongTerm(ctx context.Context, sessionID, delta string) error
	WriteFact(ctx context.Context, p memory.FactProposal) (string, error)
}

// Tool 记忆写入工具。
type Tool struct {
	w         Writer
	sessionOf func(ctx context.Context) string
}

// New 构造记忆写入工具；sessionOf 从 run ctx 解析当前会话（经函数注入，避免依赖 harness）。
func New(w Writer, sessionOf func(ctx context.Context) string) *Tool {
	return &Tool{w: w, sessionOf: sessionOf}
}

func (t *Tool) Name() string              { return "memory_write" }
func (t *Tool) RiskLevel() tool.RiskLevel { return tool.RiskWriteLocal }

func (t *Tool) Meta() tool.ToolMeta {
	return tool.ToolMeta{MaxResultChars: 500, UIHint: "memory"}
}

func (t *Tool) Description() string {
	return "把需要长期保留的信息写入记忆。用户明确要求「记住」，或你推断出稳定偏好/项目约定时使用。"
}

func (t *Tool) Schema() tool.ToolSchema {
	return tool.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: json.RawMessage(`{
			"type": "object",
			"required": ["content"],
			"properties": {
				"kind": {"type": "string", "enum": ["long_term", "fact"], "description": "long_term=追加到本会话长期记忆；fact=记为一条跨会话事实（可被其他会话召回）"},
				"content": {"type": "string", "description": "要记住的内容，一句话说清，不要长篇大论"},
				"subject": {"type": "string", "description": "kind=fact 时的归属对象，如「用户」「WorkBaby 项目」"},
				"key": {"type": "string", "description": "kind=fact 时的键，如「时区」「代码风格」"}
			}
		}`),
	}
}

type writeReq struct {
	Kind    string `json:"kind"`
	Content string `json:"content"`
	Subject string `json:"subject"`
	Key     string `json:"key"`
}

const maxContentRunes = 2000

func (t *Tool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	var req writeReq
	if err := json.Unmarshal(args, &req); err != nil {
		return tool.ToolResult{Err: pkg.Wrap(4010, "memory_write 参数解析失败", err)}
	}
	req.Content = pkg.TruncateRunes(req.Content, maxContentRunes)
	if req.Content == "" {
		return tool.ToolResult{Err: pkg.New(4011, "memory_write 内容不能为空", "")}
	}
	if req.Kind == "fact" {
		return t.writeFact(ctx, req)
	}
	return t.appendLongTerm(ctx, req.Content)
}

func (t *Tool) appendLongTerm(ctx context.Context, content string) tool.ToolResult {
	sessionID := ""
	if t.sessionOf != nil {
		sessionID = t.sessionOf(ctx)
	}
	if err := t.w.AppendLongTerm(ctx, sessionID, content); err != nil {
		return tool.ToolResult{Err: pkg.Wrap(4012, "写入长期记忆失败", err)}
	}
	return tool.ToolResult{Content: "已记入本会话长期记忆。"}
}

func (t *Tool) writeFact(ctx context.Context, req writeReq) tool.ToolResult {
	key := req.Key
	if key == "" {
		key = "note"
	}
	subject := req.Subject
	if subject == "" {
		subject = "user"
	}
	id, err := t.w.WriteFact(ctx, memory.FactProposal{
		Subject:    subject,
		Key:        key,
		Value:      req.Content,
		Confidence: 1.0,
	})
	if err != nil {
		return tool.ToolResult{Err: pkg.Wrap(4012, "写入事实记忆失败", err)}
	}
	return tool.ToolResult{
		Content: "已记为跨会话事实：" + subject + "." + key,
		Data:    map[string]any{"fact_id": id, "subject": subject, "key": key},
	}
}

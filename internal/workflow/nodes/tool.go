package nodes

import (
	"context"
	"encoding/json"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/tool"
)

// ToolNode 调用注册工具（tool.Registry）。
//
// cfg 必须包含 toolName；args 是 JSON 字符串/对象（按 Tool.Schema().Parameters 校验）。
//
// 依赖注入 tool.Registry；执行上下文从 upstream 取（上游 tool 节点可作为参数提供）。
type ToolNode struct {
	reg *tool.Registry
}

// NewToolNode 构造；自动注册 schema。
func NewToolNode(reg *tool.Registry) *ToolNode {
	n := &ToolNode{reg: reg}
	Register(n)
	return n
}

// Type 实现 Node 接口。
func (n *ToolNode) Type() domain.WorkflowNodeType { return domain.WorkflowNodeTool }

// Schema 实现 Node 接口。
func (n *ToolNode) Schema() Schema {
	return Schema{
		Type:     domain.WorkflowNodeTool,
		Required: []string{"toolName"},
		Optional: []string{"args", "argsTemplate"},
	}
}

// Execute：参数从 cfg.args / cfg.argsTemplate 解析（已 RenderConfigMap）。
//
// 若 args 是 string，按 JSON 解析；若是 map，序列化为 JSON RawMessage 给 Tool.Execute。
func (n *ToolNode) Execute(ctx context.Context, inputs map[string]any, cfg map[string]any, upstream map[string]any) (map[string]any, error) {
	if n.reg == nil {
		return nil, pkg.New(9105, "Tool 节点未配置 registry", "")
	}
	name, _ := cfg["toolName"].(string)
	if name == "" {
		return nil, pkg.New(9104, "Tool 节点缺少 cfg.toolName", "")
	}
	t, ok := n.reg.Get(name)
	if !ok {
		return nil, pkg.New(9105, "tool not found: "+name, "")
	}

	argsRaw, err := resolveToolArgs(cfg, upstream, t)
	if err != nil {
		return nil, err
	}
	if err := tool.ValidateArgs(t.Schema().Parameters, argsRaw); err != nil {
		return nil, pkg.Wrap(9105, "tool args invalid", err)
	}

	res := t.Execute(ctx, argsRaw)
	if res.Err != nil {
		return nil, pkg.Wrap(9105, "tool execution failed", res.Err)
	}
	return map[string]any{
		"content": res.Content,
		"data":    res.Data,
		"meta":    res.Meta,
	}, nil
}

// resolveToolArgs 从 cfg 解析 args；支持 string(JSON) / map / argsTemplate。
func resolveToolArgs(cfg map[string]any, upstream map[string]any, t tool.Tool) (json.RawMessage, error) {
	if a, ok := cfg["args"]; ok {
		switch x := a.(type) {
		case string:
			if strings.TrimSpace(x) == "" {
				return json.RawMessage("{}"), nil
			}
			return json.RawMessage(x), nil
		case map[string]any:
			b, err := json.Marshal(x)
			if err != nil {
				return nil, pkg.Wrap(9105, "marshal tool args failed", err)
			}
			return b, nil
		}
	}
	if tpl, ok := cfg["argsTemplate"].(string); ok && tpl != "" {
		// 上游整图作为 inputs 透传，简单拼接
		b, _ := json.Marshal(upstream)
		return b, nil
	}
	return json.RawMessage("{}"), nil
}

package nodes

import (
	"context"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
)

// HumanInputResolver 让 HumanInputNode 不依赖 service/executor；由 executor 注入。
//
// executor 持有 resolver；resolver 实现阻塞等待用户从前端提交。
type HumanInputResolver interface {
	// Resolve 阻塞直到收到用户输入或超时/取消；返回值是用户输入的字符串。
	// prompt 是 cfg.prompt 渲染后的文本；hintKey 用于让前端知道是哪个工作流的哪个节点（executionId + nodeId）。
	Resolve(ctx context.Context, executionID, nodeID, prompt string, ttlSeconds int) (string, error)
}

// HumanInputNode 阻塞等用户输入 → {value}。
//
// cfg 必填：prompt；可选：ttlSeconds（默认 24h = 86400）。
type HumanInputNode struct {
	resolver HumanInputResolver
}

// NewHumanInputNode 构造；自动注册 schema。
func NewHumanInputNode(r HumanInputResolver) *HumanInputNode {
	n := &HumanInputNode{resolver: r}
	Register(n)
	return n
}

// Type 实现 Node 接口。
func (n *HumanInputNode) Type() domain.WorkflowNodeType { return domain.WorkflowNodeHumanInput }

// Schema 实现 Node 接口。
func (n *HumanInputNode) Schema() Schema {
	return Schema{
		Type:     domain.WorkflowNodeHumanInput,
		Required: []string{"prompt"},
		Optional: []string{"ttlSeconds"},
	}
}

// Execute：渲染后的 prompt → resolver.Resolve；超时返回 9106。
func (n *HumanInputNode) Execute(ctx context.Context, inputs map[string]any, cfg map[string]any, upstream map[string]any) (map[string]any, error) {
	if n.resolver == nil {
		return nil, pkg.New(9105, "HumanInput 节点未配置 resolver", "")
	}
	prompt, _ := cfg["prompt"].(string)
	if prompt == "" {
		return nil, pkg.New(9104, "HumanInput 节点缺少 cfg.prompt", "")
	}
	ttl := 86400
	if t, ok := cfg["ttlSeconds"].(float64); ok && t > 0 {
		ttl = int(t)
	}
	execID, _ := inputs["__executionId__"].(string)
	nodeID, _ := inputs["__nodeId__"].(string)
	value, err := n.resolver.Resolve(ctx, execID, nodeID, prompt, ttl)
	if err != nil {
		return nil, err
	}
	return map[string]any{"value": value}, nil
}

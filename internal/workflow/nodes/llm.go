package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/llm/registry"
	"WorkBaby/internal/pkg"
)

// ReactRequest 一次「带工具的 LLM 执行」请求。
type ReactRequest struct {
	ProviderID   string
	Model        string
	SystemPrompt string
	UserPrompt   string
	Tools        []string // 工具名白名单；空 = 不带工具
	MaxTurns     int      // <=0 = 执行方默认
	Temperature  *float64
	ExecutionID  string // 所属工作流执行 ID（计量落库关联用；可为空）
}

// ReactResult 一次「带工具的 LLM 执行」结果。
type ReactResult struct {
	Text       string
	Usage      llm.TokenUsage
	Turns      int
	StopReason string
}

// ReactFunc 让 LLM 节点具备多轮工具（ReAct）能力。
//
// 由装配方注入实现（通常是 harness.RunOnce 的薄封装），
// 避免 nodes 包直接依赖 harness —— 保持 workflow 为叶子包。
type ReactFunc func(ctx context.Context, req ReactRequest) (ReactResult, error)

// LLMNode 调用 LLM 生成文本。
//
// cfg 必填 providerID/model；可选 systemPrompt / userPromptTemplate / temperature；
// 可选 tools（工具名数组）+ maxTurns：配置后走 ReAct（模型可多轮调用工具），
// 未配置或装配方未提供 ReactFunc 时退化为单次补全。
// 用户提示词优先 upstream.userPrompt，其次 cfg.userPromptTemplate。
type LLMNode struct {
	reg       *registry.Registry
	react     ReactFunc
	usageSink UsageSink
}

// UsageSink 记录一次 LLM 调用的用量（计量通道）；nil = 不记录。
// 补全路径直接回调；ReAct 路径由执行器（WorkflowReactor）按 turn 明细自行落库。
type UsageSink func(executionID, providerID, model string, usage llm.TokenUsage)

// NewLLMNode 构造；注入 *registry.Registry；自动注册 schema。
func NewLLMNode(reg *registry.Registry) *LLMNode {
	n := &LLMNode{reg: reg}
	Register(n)
	return n
}

// WithReactor 注入 ReAct 执行器；注入后 cfg.tools 非空即走多轮工具执行。
func (n *LLMNode) WithReactor(f ReactFunc) *LLMNode {
	if f != nil {
		n.react = f
	}
	return n
}

// WithUsageSink 注入计量回调；未注入时 LLM 调用不落 token_usages（总消耗统计会偏低）。
func (n *LLMNode) WithUsageSink(s UsageSink) *LLMNode {
	if s != nil {
		n.usageSink = s
	}
	return n
}

// Type 实现 Node 接口。
func (n *LLMNode) Type() domain.WorkflowNodeType { return domain.WorkflowNodeLLM }

// Schema 实现 Node 接口。
func (n *LLMNode) Schema() Schema {
	return Schema{
		Type:     domain.WorkflowNodeLLM,
		Required: []string{"providerID", "model"},
		Optional: []string{"systemPrompt", "userPromptTemplate", "temperature", "tools", "maxTurns"},
	}
}

// Execute 非流式调用 LLM，输出 {text, usage, providerId}。
func (n *LLMNode) Execute(ctx context.Context, inputs map[string]any, cfg map[string]any, upstream map[string]any) (map[string]any, error) {
	providerID, _ := cfg["providerID"].(string)
	if providerID == "" {
		return nil, pkg.New(9104, "LLM 节点缺少 cfg.providerId", "")
	}
	userPrompt := pickString(upstream, "userPrompt")
	if userPrompt == "" {
		if t, ok := cfg["userPromptTemplate"].(string); ok {
			userPrompt = t
		} else if c, ok := inputs["content"].(string); ok {
			userPrompt = c
		}
	}
	if userPrompt == "" {
		return nil, pkg.New(9105, "LLM 节点缺少用户 prompt（upstream.userPrompt / cfg.userPromptTemplate / inputs.content）", "")
	}

	systemPrompt, _ := cfg["systemPrompt"].(string)
	model, _ := cfg["model"].(string)
	if model == "" {
		return nil, pkg.New(9104, "LLM 节点缺少 cfg.model", "")
	}

	// 配了 tools 且装配方提供了执行器 → 走 ReAct（模型可多轮调用工具）
	executionID, _ := inputs["__executionId__"].(string)
	if toolNames := pickStrings(cfg, "tools"); len(toolNames) > 0 && n.react != nil {
		var temp *float64
		if t, ok := cfg["temperature"].(float64); ok {
			temp = &t
		}
		res, rerr := n.react(ctx, ReactRequest{
			ProviderID:   providerID,
			Model:        model,
			SystemPrompt: systemPrompt,
			UserPrompt:   userPrompt,
			Tools:        toolNames,
			MaxTurns:     pickInt(cfg, "maxTurns"),
			Temperature:  temp,
			ExecutionID:  executionID,
		})
		if rerr != nil {
			return nil, pkg.Wrap(9105, "LLM 节点 ReAct 执行失败", rerr)
		}
		return map[string]any{
			"text":       res.Text,
			"usage":      res.Usage,
			"providerID": providerID,
			"turns":      res.Turns,
			"stopReason": res.StopReason,
		}, nil
	}

	// 补全路径才需要本地 registry（ReAct 路径由执行器自行取 provider）
	if n.reg == nil {
		return nil, pkg.New(9105, "LLM 节点未配置 llm.Registry", "")
	}
	prov, err := n.reg.Get(providerID)
	if err != nil {
		return nil, pkg.Wrap(9105, "provider 取不到", err)
	}

	var msgs []*llm.Message
	if systemPrompt != "" {
		msgs = append(msgs, &llm.Message{Role: llm.RoleSystem, Content: systemPrompt})
	}
	msgs = append(msgs, &llm.Message{Role: llm.RoleUser, Content: userPrompt})

	req := &llm.ChatRequest{Model: model, Messages: msgs, User: domain.LocalUserID}
	if t, ok := cfg["temperature"].(float64); ok {
		req.Temperature = &t
	}

	stream, err := prov.Stream(ctx, req)
	if err != nil {
		return nil, pkg.Wrap(9105, "LLM 调用失败", err)
	}
	var text strings.Builder
	var usage llm.TokenUsage
	for chunk := range stream {
		if chunk.Err != nil {
			return nil, pkg.Wrap(9105, "LLM 流式错误", chunk.Err)
		}
		text.WriteString(chunk.Delta.Content)
		text.WriteString(chunk.Delta.Thinking)
		if chunk.FinalUsage != nil {
			usage = *chunk.FinalUsage
		}
	}
	out := map[string]any{"text": text.String(), "usage": usage, "providerID": providerID}
	// 补全路径同样计量：不落库的话工作流 LLM 节点的 token 消耗会完全不可见
	if n.usageSink != nil {
		n.usageSink(executionID, providerID, model, usage)
	}
	return out, nil
}

// pickString 从 map[string]any 拿字符串（nil-safe）。
func pickString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// pickStrings 取字符串数组：支持 []any（模板渲染产物）与逗号分隔字符串。
func pickStrings(m map[string]any, key string) []string {
	raw, ok := m[key]
	if !ok || raw == nil {
		return nil
	}
	var out []string
	switch v := raw.(type) {
	case []any:
		for _, it := range v {
			if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case []string:
		out = v
	case string:
		for _, s := range strings.Split(v, ",") {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

// pickInt 取整数：模板渲染后可能是 float64 或 string。
func pickInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return 0
}

// jsonString 把任意值序列化为 JSON 字符串（落库 inputs/outputs 用）。
func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

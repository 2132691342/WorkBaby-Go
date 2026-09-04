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

// LLMNode 调用 LLM 生成文本（流式拼全量后输出）。
// cfg 必填 providerID/model；可选 systemPrompt / userPromptTemplate / temperature；
// 用户提示词优先 upstream.userPrompt，其次 cfg.userPromptTemplate。
type LLMNode struct {
	reg *registry.Registry
}

// NewLLMNode 构造；注入 *registry.Registry；自动注册 schema。
func NewLLMNode(reg *registry.Registry) *LLMNode {
	n := &LLMNode{reg: reg}
	Register(n)
	return n
}

// Type 实现 Node 接口。
func (n *LLMNode) Type() domain.WorkflowNodeType { return domain.WorkflowNodeLLM }

// Schema 实现 Node 接口。
func (n *LLMNode) Schema() Schema {
	return Schema{
		Type:     domain.WorkflowNodeLLM,
		Required: []string{"providerID", "model"},
		Optional: []string{"systemPrompt", "userPromptTemplate", "temperature"},
	}
}

// Execute 非流式调用 LLM，输出 {text, usage, providerId}。
func (n *LLMNode) Execute(ctx context.Context, inputs map[string]any, cfg map[string]any, upstream map[string]any) (map[string]any, error) {
	if n.reg == nil {
		return nil, pkg.New(9105, "LLM 节点未配置 llm.Registry", "")
	}
	providerID, _ := cfg["providerID"].(string)
	if providerID == "" {
		return nil, pkg.New(9104, "LLM 节点缺少 cfg.providerId", "")
	}
	prov, err := n.reg.Get(providerID)
	if err != nil {
		return nil, pkg.Wrap(9105, "provider 取不到", err)
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

// jsonString 把任意值序列化为 JSON 字符串（落库 inputs/outputs 用）。
func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Package sessionvar 提供「会话变量」工具：模型可读写跨轮次的结构化状态。
// 变量与对话历史分离，不受上下文压缩影响，适合记住目标对象、用户偏好等短状态。
// 三层作用域：user（跨会话）/ session（会话内，默认）/ temp（run 内，不落库）。
package sessionvar

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/tool"
)

// Store 会话变量存取（由装配方注入）；写操作返回最新清单，省一次往返。
type Store interface {
	List(ctx context.Context, sessionID string) ([]domain.SessionVarItem, error)
	Set(ctx context.Context, sessionID string, scope domain.SessionVarScope, key, value string) ([]domain.SessionVarItem, error)
	Delete(ctx context.Context, sessionID string, scope domain.SessionVarScope, key string) ([]domain.SessionVarItem, error)
	SetTemp(runID, key, value string) ([]domain.SessionVarItem, error)
	ListTemp(runID string) []domain.SessionVarItem
}

// Tool 会话变量工具（readonly 风险级：只动会话内状态，不触碰文件与网络）。
type Tool struct {
	store Store
	sess  func(ctx context.Context) string
	run   func(ctx context.Context) string
}

// New 构造；store/sess 为 nil 时返回配置缺失错误（模型可见反馈）；run 为 nil 时 temp 作用域不可用。
func New(store Store, sess func(ctx context.Context) string, run func(ctx context.Context) string) *Tool {
	return &Tool{store: store, sess: sess, run: run}
}

var paramsSchema = json.RawMessage(`{
	"type": "object",
	"required": ["action"],
	"properties": {
		"action": {"type": "string", "enum": ["set", "get", "list", "delete"]},
		"key":    {"type": "string"},
		"value":  {"type": "string"},
		"scope":  {"type": "string", "enum": ["user", "session", "temp"], "description": "user=跨会话长期偏好；session=当前会话（默认）；temp=本轮临时"}
	}
}`)

func (t *Tool) Name() string { return "session_var" }

// Description 触发面覆盖「记住 / 别忘 / 记一下」这类口语意图。
func (t *Tool) Description() string {
	return "读写结构化状态变量（跨轮次保留、不受上下文压缩影响）。动作：set(key,value) / get(key) / list / delete(key)。" +
		"scope 三档：user=跨会话长期偏好与约定（如称呼、常用目录），session=当前会话状态（默认），temp=本轮临时草稿。" +
		"用户要求「记住」某个持久短状态时用 scope=user 或 session，不要依赖对话记忆。"
}

// Schema 实现 tool.Tool。
func (t *Tool) Schema() tool.ToolSchema {
	return tool.ToolSchema{Name: "session_var", Description: t.Description(), Parameters: paramsSchema}
}

// RiskLevel 实现 tool.Tool。
func (t *Tool) RiskLevel() tool.RiskLevel { return tool.RiskReadOnly }

// Execute 执行动作并返回最新变量清单（文本 + 结构化 Data）。
func (t *Tool) Execute(ctx context.Context, args json.RawMessage) tool.ToolResult {
	fail := func(msg string, err error) tool.ToolResult {
		return tool.ToolResult{Content: "session_var: " + msg, Err: err}
	}
	if t.store == nil || t.sess == nil {
		return fail("tool not configured", fmt.Errorf("session_var: tool not configured"))
	}
	sessionID := t.sess(ctx)
	if sessionID == "" {
		return fail("no active session", fmt.Errorf("session_var: no active session"))
	}

	var req struct {
		Action string `json:"action"`
		Key    string `json:"key"`
		Value  string `json:"value"`
		Scope  string `json:"scope"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return fail("invalid args: "+err.Error(), err)
	}
	key := strings.TrimSpace(req.Key)
	scope := domain.NormalizeSessionVarScope(req.Scope)
	temp := scope == domain.SessionVarScopeTemp

	var items []domain.SessionVarItem
	var err error
	switch req.Action {
	case "set":
		if key == "" {
			return fail("key required for set", fmt.Errorf("session_var: key required"))
		}
		if temp {
			if t.run == nil || t.run(ctx) == "" {
				return fail("temp scope requires an active run", fmt.Errorf("session_var: no run context"))
			}
			if items, err = t.store.SetTemp(t.run(ctx), key, req.Value); err != nil {
				return fail("set failed", err)
			}
		} else if items, err = t.store.Set(ctx, sessionID, scope, key, req.Value); err != nil {
			return fail("set failed", err)
		}
	case "delete":
		if key == "" {
			return fail("key required for delete", fmt.Errorf("session_var: key required"))
		}
		if temp {
			return fail("temp variables are cleared automatically at run end", fmt.Errorf("session_var: temp not deletable"))
		}
		if items, err = t.store.Delete(ctx, sessionID, scope, key); err != nil {
			return fail("delete failed", err)
		}
	case "get":
		if key == "" {
			return fail("key required for get", fmt.Errorf("session_var: key required"))
		}
		if items, err = t.all(ctx, sessionID); err != nil {
			return fail("load failed", err)
		}
		for _, it := range items {
			if it.Key == key {
				return tool.ToolResult{
					Content: fmt.Sprintf("%s = %s", it.Key, it.Value),
					Data:    map[string]any{"session_var": map[string]any{"key": it.Key, "value": it.Value, "scope": string(it.Scope)}},
				}
			}
		}
		return fail("key not found: "+key, fmt.Errorf("session_var: key not found"))
	case "list":
		if items, err = t.all(ctx, sessionID); err != nil {
			return fail("load failed", err)
		}
	case "":
		return fail("action required", fmt.Errorf("session_var: action required"))
	default:
		return fail("unknown action "+req.Action, fmt.Errorf("session_var: unknown action"))
	}
	return render(items)
}

// all 返回会话级 + 用户级 + 本轮临时态的合并清单（temp 仅 list/get 可见）。
func (t *Tool) all(ctx context.Context, sessionID string) ([]domain.SessionVarItem, error) {
	items, err := t.store.List(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if t.run != nil {
		items = append(items, t.store.ListTemp(t.run(ctx))...)
	}
	return items, nil
}

// render 组装文本与结构化快照。
func render(items []domain.SessionVarItem) tool.ToolResult {
	var sb strings.Builder
	sb.WriteString("当前结构化变量：\n")
	if len(items) == 0 {
		sb.WriteString("(空)\n")
	}
	for _, it := range items {
		sb.WriteString("- [" + string(it.Scope) + "] " + it.Key + " = " + it.Value + "\n")
	}
	state := domain.SessionVarRESP{Items: items}
	return tool.ToolResult{Content: sb.String(), Data: map[string]any{"session_vars": state}}
}

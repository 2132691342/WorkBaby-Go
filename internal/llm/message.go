// Package llm 提供 LLM Provider 抽象、流式协议适配、tool call 归一化、thinking 分离。
//
// Message 与 TokenUsage 自实现。
//
// 边界：llm/ 不 import harness / agent / api / service / wails；
// Provider 实例由 Registry 按 domain.AiProviderDO 配置构建。
package llm

// RoleType 消息角色。
type RoleType string

const (
	RoleSystem    RoleType = "system"
	RoleUser      RoleType = "user"
	RoleAssistant RoleType = "assistant"
	RoleTool      RoleType = "tool"
)

// Message 跨边界统一消息：
//   - Content：模型可见正文（用户输入/助手输出）
//   - Thinking：推理文本（Anthropic thinking / DeepSeek reasoning_content / GLM thinking），不混入 Content
//   - ToolCalls：助手消息的工具调用
//   - ToolCallID / ToolName：仅 role==tool 时使用，回填 LLM 的工具结果
//   - Name：可选显示名
type Message struct {
	Role       RoleType   `json:"role"`
	Content    string     `json:"content"`
	Thinking   string     `json:"thinking,omitempty"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string     `json:"toolCallID,omitempty"`
	ToolName   string     `json:"toolName,omitempty"`
	Name       string     `json:"name,omitempty"`
	// Seq 源消息在 chat_messages 中的序列号（用于 checkpoint 增量切片与 Resume 拼接）；
	// 不参与上游协议，仅作为持久化侧的去重锚点。
	Seq int64 `json:"seq,omitempty"`
}

// ToolCall 模型返回的工具调用；只在 assistant 消息出现。
type ToolCall struct {
	ID       string       `json:"id"` // tool call 唯一 ID（OpenAI 形态）；Anthropic 用同字段
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall 工具调用的元数据 + 原始 JSON 参数（不展开，便于前端展示）。
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 字符串
}

// TokenUsage token 统计。
type TokenUsage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int `json:"cacheWriteTokens,omitempty"`
	TotalTokens      int `json:"totalTokens"`
}

// 构造器。
func SystemMessage(content string) *Message { return &Message{Role: RoleSystem, Content: content} }
func UserMessage(content string) *Message   { return &Message{Role: RoleUser, Content: content} }
func AssistantMessage(content string, calls []ToolCall) *Message {
	return &Message{Role: RoleAssistant, Content: content, ToolCalls: calls}
}
func ToolMessage(toolCallID, name, content string) *Message {
	return &Message{Role: RoleTool, Content: content, ToolCallID: toolCallID, ToolName: name}
}

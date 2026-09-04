// Package harness 是 WorkBaby 的 Agent 内核（自研轻量循环；不依赖 wails / api / service）。
package harness

// EventKind 事件命名空间：agent.{run|turn|tool}.{phase}，api 层按前缀映射到 chat:*。
type EventKind string

const (
	EventRunStart     EventKind = "agent.run.start"
	EventTurnStart    EventKind = "agent.turn.start"
	EventTurnDelta    EventKind = "agent.turn.delta"    // 正文增量
	EventTurnThinking EventKind = "agent.turn.thinking" // 推理增量
	EventTurnEnd      EventKind = "agent.turn.end"
	EventToolCall     EventKind = "agent.tool.call"   // 模型决定调用工具（参数已闭合）
	EventToolStart    EventKind = "agent.tool.start"  // 工具开始执行
	EventToolResult   EventKind = "agent.tool.result" // 工具执行完成
	EventCheckpoint   EventKind = "agent.checkpoint"  // 检查点已写入
	EventRunDone      EventKind = "agent.run.done"
	EventError        EventKind = "agent.error"
	EventRetry        EventKind = "agent.retry" // 建流瞬时错误退避重试（限流/5xx/超时）
)

// Event 跨边界事件载荷。JSON 字段一律 snake_case（CLAUDE.md §2.5）。
type Event struct {
	Kind        EventKind `json:"kind"`
	RunID       string    `json:"run_id"`
	ParentRunID string    `json:"parent_run_id,omitempty"` // 子 Agent 委派：指向发起方 run
	SessionID   string    `json:"session_id"`
	Turn        int       `json:"turn"`
	Agent       string    `json:"agent,omitempty"` // 产生该事件的 Agent 名（子 Agent 事件用于区分来源）
	Payload     any       `json:"payload,omitempty"`
}

// RunStartPayload 起始事件。
type RunStartPayload struct {
	UserMessage string `json:"user_message,omitempty"`
	Model       string `json:"model"`
	ProviderID  string `json:"provider_id"`
}

// TurnDeltaPayload 增量文本。
type TurnDeltaPayload struct {
	Kind string `json:"kind"` // "content" | "thinking"
	Text string `json:"text"`
}

// ToolCallPayload 工具调用相关事件载荷。
type ToolCallPayload struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 字符串
}

// ToolResultPayload 工具执行结果事件载荷。
// Data 携带工具的结构化产出（如 todo 计划快照），供前端渲染专用面板而非解析文本。
// Refused = true 表示审批拒绝（非故障）：前端展示「已拒绝」态而非错误态。
type ToolResultPayload struct {
	ToolCallID string            `json:"tool_call_id"`
	Name       string            `json:"name"`
	Content    string            `json:"content"`
	Err        string            `json:"error,omitempty"`
	DurationMs int64             `json:"duration_ms"`
	Meta       map[string]string `json:"meta,omitempty"`
	Data       map[string]any    `json:"data,omitempty"`
	Refused    bool              `json:"refused,omitempty"`
}

// UsagePayload run 终止/轮次结束事件的 token 用量。
// CacheRead 与 CacheWrite 均为 InputTokens 的拆解维度（同一批 prompt token 的去向，不与 Input 叠加）。
type UsagePayload struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CacheRead    int `json:"cache_read_tokens,omitempty"`
	CacheWrite   int `json:"cache_creation_tokens,omitempty"`
	Total        int `json:"total_tokens"`
}

// RunDonePayload 终止事件载荷。
type RunDonePayload struct {
	Reason     string       `json:"reason"`
	StopReason string       `json:"stop_reason,omitempty"`
	MessageID  string       `json:"message_id,omitempty"`
	Usage      UsagePayload `json:"usage"`
}

// ErrorPayload 失败事件。
type ErrorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RetryPayload 建流瞬时错误退避重试事件（前端展示「限流，N 秒后自动重试」）。
type RetryPayload struct {
	Attempt  int    `json:"attempt"`
	DelayMs  int64  `json:"delay_ms"`
	Reason   string `json:"reason"`
}

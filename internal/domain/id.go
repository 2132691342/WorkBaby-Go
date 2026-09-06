// Package domain 是业务聚合根：每个聚合根一个文件，含 DO/DTO/REQ/VO/RESP/枚举/常量/错误变量。
//
// 严格约束：
//   - 禁止 import 任何上层（service / repo / api / 能力域 / wails runtime）；仅可 import pkg；
//   - 跨边界 struct 时间字段一律 int64 毫秒；
//   - 枚举/常量在文件顶部集中声明；错误变量以 Err* 命名。
package domain

// ID 前缀常量（ULID 前缀 SCREAMING_SNAKE）；上限 16 个字符，列存 64。
const (
	IDProvider       = "PROVIDER"
	IDSession        = "SESSION"
	IDMessage        = "MESSAGE"
	IDMessageBlock   = "BLOCK"
	IDSystemSetting  = "SYS"
	IDMemoryEpisode  = "MEM_EPISODE"
	IDMemoryFact     = "MEM_FACT"
	IDMemoryProc     = "MEM_PROC"
	IDSkill          = "SKILL"
	IDMcpServer      = "MCP"
	IDKnowledgeDoc   = "DOC"
	IDKnowledgeChunk = "CHUNK"
	IDWorkflow       = "WORKFLOW"
	IDWorkflowExec   = "WF_EXEC"
	IDWorkflowNode   = "WF_NODE"
	IDChannel        = "CHANNEL"
	IDChannelLog     = "CH_LOG"
	IDCron           = "CRON"
	IDSprite         = "SPRITE"
	IDFolder         = "FOLDER"
	IDFile           = "FILE"
)

// LocalUserID 本机单用户固定 ID；与 doc 17 auth §1 一致。
const LocalUserID = "local"

// DefaultTenant 默认租户；MVP 单租户。
const DefaultTenant = "default"

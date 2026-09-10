package domain

// AgentCheckpointDO Agent 运行检查点。
//
// 替代 JSONL 文件：跨进程重启可恢复，前端可列「可恢复的运行」。
// 语义与 harness.Checkpoint 一致但按 DO 平铺（复杂字段 JSON 文本化，
// 转换在 service 层 adapter 完成，repo 只做无业务语义的读写）。
//
// 增量字段（v2）：
//   - LastSeq：本轮起始消息 seq（MessagesJSON 只存增量）；Resume 时按 LastSeq 切分历史回放。
//   - MessagesJSON 含义从「完整对话」改为「自 LastSeq 起本轮新增/原状消息」，30 轮 run 约
//     从 30× 全量 → 1×（system + 工具元数据）+ 增量，磁盘占用降一个数量级。
type AgentCheckpointDO struct {
	ID                 string `gorm:"primaryKey;size:64"                    json:"id"`
	SessionID          string `gorm:"size:64;index:idx_cp_session"          json:"session_id"`
	RunID              string `gorm:"size:64;index:idx_cp_session;uniqueIndex:uk_cp_run_turn,priority:1" json:"run_id"`
	Turn               int    `gorm:"uniqueIndex:uk_cp_run_turn,priority:2" json:"turn"`
	AssistantMessageID string `gorm:"size:64;index"                        json:"assistant_message_id"`
	LastSeq            int64  `gorm:"default:0"                            json:"last_seq"`       // 增量起点；旧行 0 = 全量（含首条）
	MessagesJSON       string `gorm:"type:text"                             json:"messages_json"` // 增量消息
	StateJSON          string `gorm:"type:text"                             json:"state_json"`    // harness.RunState
	UsageJSON          string `gorm:"type:text"                             json:"usage_json"`    // llm.TokenUsage
	StepsJSON          string `gorm:"type:text"                             json:"steps_json"`    // map[string]harness.StepRecord（幂等恢复；v1 新增列，旧行空串）
	Content            string `gorm:"type:text"                             json:"content"`
	Thinking           string `gorm:"type:text"                             json:"thinking"`
	CreatedAt          int64  `gorm:"autoCreateTime:milli"                  json:"created_at"`
}

// TableName 固定表名。
func (AgentCheckpointDO) TableName() string { return "agent_checkpoints" }

package domain

// ApprovalRecordDO 审批/补充输入请求的持久化记录（暂停态可恢复）。
//
// 审批不再是纯内存事件：run 因 ask/input_required 暂停时落 pending 行，
// 进程重启后未决请求仍可见、可决策；决策后更新终态（approved/denied/timeout/cancelled/answered）。
type ApprovalRecordDO struct {
	ID        string `gorm:"primaryKey;size:64"             json:"id"`
	RunID     string `gorm:"size:64;index:idx_apr_run"      json:"run_id"`
	SessionID string `gorm:"size:64;index:idx_apr_session"  json:"session_id"`
	Kind      string `gorm:"size:16;index"                  json:"kind"`   // approval | input
	Command   string `gorm:"type:text"                      json:"command"` // 待审批命令 / 补充输入问题
	Risk      string `gorm:"size:32"                        json:"risk"`
	Status    string `gorm:"size:16;index:idx_apr_status"   json:"status"` // pending/approved/denied/timeout/cancelled/answered
	Answer    string `gorm:"type:text"                      json:"answer"` // input 类用户回复
	ExpiresAt int64  `gorm:"index"                          json:"expires_at"`
	CreatedAt int64  `gorm:"autoCreateTime:milli"           json:"created_at"`
	DecidedAt int64  `gorm:"default:0"                      json:"decided_at"`
}

// TableName 固定表名。
func (ApprovalRecordDO) TableName() string { return "approval_records" }

// 审批记录状态枚举。
const (
	ApprovalStatusPending   = "pending"
	ApprovalStatusApproved  = "approved"
	ApprovalStatusDenied    = "denied"
	ApprovalStatusTimeout   = "timeout"
	ApprovalStatusCancelled = "cancelled"
	ApprovalStatusAnswered  = "answered"
)

// 审批记录类别。
const (
	ApprovalKindApproval = "approval"
	ApprovalKindInput    = "input"
)

// AnswerInputREQ 补充输入回复入参。
type AnswerInputREQ struct {
	Answer string `json:"answer"`
}

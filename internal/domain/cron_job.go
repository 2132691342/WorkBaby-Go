package domain

import "WorkBaby/internal/pkg"

// CronJobAction 定时任务动作类型。
type CronJobAction string

const (
	ActionRunWorkflow CronJobAction = "run_workflow"
	// ActionRunSkill / ActionSendChannel 预留（v2）；装配方注册对应 handler 即可用。
)

// CronJobDO 定时任务（cron_jobs 表）。
type CronJobDO struct {
	ID         string        `gorm:"primaryKey;size:64" json:"id"`
	Name       string        `gorm:"size:128" json:"name"`
	Spec       string        `gorm:"size:64" json:"spec"` // Cron 表达式（5/6 段）
	Action     CronJobAction `gorm:"size:32" json:"action"`
	ActionArgs string        `gorm:"type:text" json:"action_args"`
	Enabled    bool          `gorm:"default:true" json:"enabled"`
	LastRunAt  *int64        `json:"last_run_at"`
	LastStatus string        `gorm:"size:16" json:"last_status"`
	NextRunAt  *int64        `json:"next_run_at"`
	CreatedAt  int64         `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt  int64         `gorm:"autoUpdateTime:milli" json:"updated_at"`
	DeletedAt  int64         `gorm:"default:0;index" json:"deleted_at"` // 软删；unix milli
}

// TableName 固定表名。
func (CronJobDO) TableName() string { return "cron_jobs" }

// CronJobREQ 创建/更新请求（前端契约：schedule + workflowId → run_workflow 动作）。
type CronJobREQ struct {
	Name       string `json:"name"`
	Schedule   string `json:"schedule"`
	WorkflowID string `json:"workflow_id"`
	Enabled    *bool  `json:"enabled"`
}

// CronJobRESP 出参（对齐前端 CronJob；schedule 即 spec，workflowId 从 actionArgs 提取）。
type CronJobRESP struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Schedule   string        `json:"schedule"`
	WorkflowID string        `json:"workflow_id"`
	Enabled    bool          `json:"enabled"`
	LastRunAt  *int64        `json:"last_run_at"`
	LastStatus string        `json:"last_status"`
	NextRunAt  *int64        `json:"next_run_at"`
	Action     CronJobAction `json:"action"`
	CreatedAt  int64         `json:"created_at"`
	UpdatedAt  int64         `json:"updated_at"`
}

// RunWorkflowArgs run_workflow 动作的 actionArgs 载荷。
type RunWorkflowArgs struct {
	WorkflowID string `json:"workflow_id"`
}

// 错误变量；段位 9200-9204。
var (
	ErrCronInvalidSpec   = pkg.New(9201, "cron expression invalid", "")
	ErrCronJobNotFound   = pkg.New(9204, "cron job not found", "")
	ErrCronUnknownAction = pkg.New(9202, "cron action type unknown", "")
)

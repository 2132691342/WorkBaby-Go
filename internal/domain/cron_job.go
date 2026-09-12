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
	// MisfirePolicy 错过触发处理：skip = 丢弃本次，run_once = 补跑一次。
	MisfirePolicy string `gorm:"size:16;default:'skip'" json:"misfire_policy"`
	// MaxRetries 单次执行失败重试上限；0 = 不重试。错误指数退避后重试。
	MaxRetries int `gorm:"default:0" json:"max_retries"`
	// RetryDelaySec 首次重试延迟（后续 ×2 指数退避，封顶 600s）。
	RetryDelaySec int    `gorm:"default:30" json:"retry_delay_sec"`
	LastRunAt    *int64 `json:"last_run_at"`
	LastStatus   string `gorm:"size:16" json:"last_status"` // success/failed/running
	NextRunAt    *int64 `json:"next_run_at"`
	RunCount     int    `gorm:"default:0" json:"run_count"`   // 累计执行次数
	FailCount    int    `gorm:"default:0" json:"fail_count"`  // 累计失败次数
	CreatedAt    int64  `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt    int64  `gorm:"autoUpdateTime:milli" json:"updated_at"`
	DeletedAt    int64  `gorm:"default:0;index" json:"deleted_at"` // 软删；unix milli
}

// CronJobRunLogDO 单次执行明细（cron_run_logs 表）：重试轨迹 + 错误上下文。
type CronJobRunLogDO struct {
	ID         string `gorm:"primaryKey;size:64" json:"id"`
	JobID      string `gorm:"size:64;index" json:"job_id"`
	Attempt    int    `gorm:"default:1" json:"attempt"`      // 第几次尝试（1..MaxRetries+1）
	Status     string `gorm:"size:16" json:"status"`         // success/failed/skipped
	StartedAt  int64  `gorm:"autoCreateTime:milli" json:"started_at"`
	FinishedAt int64  `gorm:"default:0" json:"finished_at"`
	DurationMs int64  `gorm:"default:0" json:"duration_ms"`
	Error      string `gorm:"type:text" json:"error"` // 失败错误（截断 2000 字）
}

// TableName 固定表名。
func (CronJobRunLogDO) TableName() string { return "cron_run_logs" }

// TableName 固定表名。
func (CronJobDO) TableName() string { return "cron_jobs" }

// CronJobREQ 创建/更新请求（前端契约：schedule + workflowId → run_workflow 动作）。
type CronJobREQ struct {
	Name           string `json:"name"`
	Schedule       string `json:"schedule"`
	WorkflowID     string `json:"workflow_id"`
	Enabled        *bool  `json:"enabled"`
	MisfirePolicy  string `json:"misfire_policy,omitempty"` // skip / run_once
	MaxRetries     *int   `json:"max_retries,omitempty"`
	RetryDelaySec  *int   `json:"retry_delay_sec,omitempty"`
}

// CronJobRESP 出参（对齐前端 CronJob；schedule 即 spec，workflowId 从 actionArgs 提取）。
type CronJobRESP struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Schedule      string        `json:"schedule"`
	WorkflowID    string        `json:"workflow_id"`
	Enabled       bool          `json:"enabled"`
	MisfirePolicy string        `json:"misfire_policy"`
	MaxRetries    int           `json:"max_retries"`
	RetryDelaySec int           `json:"retry_delay_sec"`
	LastRunAt     *int64        `json:"last_run_at"`
	LastStatus    string        `json:"last_status"`
	NextRunAt     *int64        `json:"next_run_at"`
	RunCount      int           `json:"run_count"`
	FailCount     int           `json:"fail_count"`
	Action        CronJobAction `json:"action"`
	CreatedAt     int64         `json:"created_at"`
	UpdatedAt     int64         `json:"updated_at"`
}

// CronJobRunLogRESP 单次执行日志出参（前端任务中心/审计面板）。
type CronJobRunLogRESP struct {
	ID         string `json:"id"`
	JobID      string `json:"job_id"`
	Attempt    int    `json:"attempt"`
	Status     string `json:"status"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error"`
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

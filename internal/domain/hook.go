// Package domain 本文件：UserHook 聚合根（user_hooks 表）——用户自定义生命周期钩子。
// 事件：run_start / before_tool / after_tool / run_end；exit 非 0 仅记录不阻断。
package domain

import (
	"encoding/json"

	"gorm.io/gorm"

	"WorkBaby/internal/pkg"
)

// Hook 事件枚举。
const (
	HookEventRunStart   = "run_start"
	HookEventBeforeTool = "before_tool"
	HookEventAfterTool  = "after_tool"
	HookEventRunEnd     = "run_end"
)

// HookEvents 全部合法事件（设置页下拉与后端校验共用）。
var HookEvents = []string{HookEventRunStart, HookEventBeforeTool, HookEventAfterTool, HookEventRunEnd}

// UserHookDO 用户钩子持久化实体（user_hooks 表是唯一真相源）。
type UserHookDO struct {
	ID        string         `gorm:"primaryKey;size:64" json:"id"`
	Name      string         `gorm:"size:128" json:"name"`            // 展示名
	Event     string         `gorm:"size:32;index" json:"event"`      // 触发事件
	Matcher   string         `gorm:"size:256" json:"matcher"`         // 工具名匹配（空=全部；逗号分隔多值）
	Command   string         `gorm:"size:1024" json:"command"`        // 子进程命令行（经 cmd /c 解析）
	TimeoutMs int            `gorm:"default:10000" json:"timeout_ms"` // 超时（毫秒），上限 60000
	Enabled   bool           `gorm:"default:true" json:"enabled"`     // 启用开关
	Sort      int            `gorm:"default:0" json:"sort"`           // 执行顺序（小者先）
	CreatedAt int64          `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt int64          `gorm:"autoUpdateTime:milli" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 固定表名。
func (UserHookDO) TableName() string { return "user_hooks" }

// UserHookRESP 出参。
type UserHookRESP struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Event     string `json:"event"`
	Matcher   string `json:"matcher"`
	Command   string `json:"command"`
	TimeoutMs int    `json:"timeout_ms"`
	Enabled   bool   `json:"enabled"`
	Sort      int    `json:"sort"`
}

// UserHookREQ 创建/更新入参（按 ID upsert，ID 空 = 新建）。
type UserHookREQ struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Event     string `json:"event"`
	Matcher   string `json:"matcher"`
	Command   string `json:"command"`
	TimeoutMs int    `json:"timeout_ms"`
	Enabled   *bool  `json:"enabled"` // nil = 保留现值
	Sort      *int   `json:"sort"`
}

// HookPayload 子进程 stdin 载荷（协议 v1：stdin JSON in / stdout JSON out）。
type HookPayload struct {
	Event     string          `json:"event"`                // 触发事件
	SessionID string          `json:"session_id,omitempty"` // 会话
	RunID     string          `json:"run_id,omitempty"`     // 运行
	Tool      string          `json:"tool,omitempty"`       // 工具名（before/after_tool）
	Params    json.RawMessage `json:"params,omitempty"`     // 工具参数（before_tool）
	Outcome   string          `json:"outcome,omitempty"`    // 工具结果态 ok / error / refused（after_tool）
	Reason    string          `json:"reason,omitempty"`     // run 终止原因（run_end）
}

// HookDecision 子进程 stdout 决策（仅 before_tool 的 deny 生效为拦截）。
type HookDecision struct {
	Decision string `json:"decision"` // deny / allow（空=allow）
	Reason   string `json:"reason"`   // deny 时回填给模型的拒绝理由
}

// HookTestRESP 设置页试跑结果。
type HookTestRESP struct {
	Decision   string `json:"decision"`
	Reason     string `json:"reason"`
	Err        string `json:"err"`
	DurationMs int64  `json:"duration_ms"`
}

// 包级错误变量；错误码段位 8600（UserHook 聚合根）。
var (
	ErrHookNotFound   = pkg.New(8601, "user hook not found", "")
	ErrHookInvalid    = pkg.New(8602, "user hook invalid", "")
	ErrHookTestFailed = pkg.New(8603, "user hook test run failed", "")
)

// Package domain 是业务聚合根：每个聚合根一个文件，含 DO/DTO/REQ/VO/RESP/枚举/常量/错误变量。
//
// 本文件：Inbox 回写收件箱聚合根——run 终局抽取的记忆/技能候选，人工评审后合入正式库。
package domain

import (
	"encoding/json"

	"gorm.io/gorm"

	"WorkBaby/internal/pkg"
)

// InboxKind 候选类型。
type InboxKind string

const (
	InboxKindFact      InboxKind = "fact"      // 语义记忆（用户偏好 / 事实）
	InboxKindProcedure InboxKind = "procedure" // 程序记忆（可复用工具链）
	InboxKindSkill     InboxKind = "skill"     // 技能草稿
)

// InboxStatus 评审状态。
type InboxStatus string

const (
	InboxStatusPending  InboxStatus = "pending"  // 待审
	InboxStatusApproved InboxStatus = "approved" // 已合入
	InboxStatusRejected InboxStatus = "rejected" // 已拒绝
)

// InboxSource 候选来源：llm = run 终局模型抽取（需人审）；deterministic = 规则抽取。
type InboxSource string

const (
	InboxSourceLLM           InboxSource = "llm"
	InboxSourceDeterministic InboxSource = "deterministic"
)

// InboxItemDO 回写收件箱条目（inbox_items 表）。
type InboxItemDO struct {
	ID         string         `gorm:"primaryKey;size:64" json:"id"`
	Kind       InboxKind      `gorm:"size:16;index" json:"kind"`
	Title      string         `gorm:"size:256" json:"title"`
	Summary    string         `gorm:"size:512" json:"summary"`
	Payload    string         `gorm:"type:text" json:"-"` // JSON：按 kind 解释
	Status     InboxStatus    `gorm:"size:16;index" json:"status"`
	Source     InboxSource    `gorm:"size:16" json:"source"`
	Confidence float64        `gorm:"default:0" json:"confidence"`
	SessionID  string         `gorm:"size:64;index" json:"session_id"`
	RunID      string         `gorm:"size:64" json:"run_id"`
	CreatedAt  int64          `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt  int64          `gorm:"autoUpdateTime:milli" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 固定表名。
func (InboxItemDO) TableName() string { return "inbox_items" }

// InboxFactPayload 语义记忆候选载荷。
type InboxFactPayload struct {
	Subject    string  `json:"subject"`
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence,omitempty"`
}

// InboxProcedurePayload 程序记忆候选载荷。
type InboxProcedurePayload struct {
	Name  string   `json:"name"`
	Steps []string `json:"steps"`
}

// InboxSkillPayload 技能草稿候选载荷。
type InboxSkillPayload struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	WhenToUse    string   `json:"when_to_use"`
	Body         string   `json:"body"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
}

// InboxCandidate 抽取器产出的候选（未落库）。
type InboxCandidate struct {
	Kind       InboxKind
	Title      string
	Summary    string
	Payload    any
	Source     InboxSource
	Confidence float64
}

// InboxItemRESP 收件箱条目出参。
type InboxItemRESP struct {
	ID         string          `json:"id"`
	Kind       InboxKind       `json:"kind"`
	Title      string          `json:"title"`
	Summary    string          `json:"summary"`
	Payload    json.RawMessage `json:"payload"`
	Status     InboxStatus     `json:"status"`
	Source     InboxSource     `json:"source"`
	Confidence float64         `json:"confidence"`
	SessionID  string          `json:"session_id"`
	RunID      string          `json:"run_id"`
	CreatedAt  int64           `json:"created_at"`
	UpdatedAt  int64           `json:"updated_at"`
}

// InboxStatsRESP 收件箱概览。
type InboxStatsRESP struct {
	Pending  int64 `json:"pending"`
	Approved int64 `json:"approved"`
	Rejected int64 `json:"rejected"`
}

// 包级错误变量；错误码段位 6100。
var (
	ErrInboxNotFound    = pkg.New(6101, "收件箱条目不存在", "")
	ErrInboxNotPending  = pkg.New(6102, "条目已被处理", "")
	ErrInboxApplyFailed = pkg.New(6103, "条目合入失败", "")
)

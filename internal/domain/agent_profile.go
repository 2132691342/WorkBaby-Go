// Package domain 是业务聚合根：每个聚合根一个文件，含 DO/DTO/REQ/VO/RESP/枚举/常量/错误变量。
//
// 本文件：AgentProfile 聚合根：用户自定义子智能体（人设 + 工具策略 + 预算），
// 与内置 Agent（default/coding/research/writer）同构，供 delegate_task 按名委派与会话切换。
package domain

import (
	"gorm.io/gorm"

	"WorkBaby/internal/pkg"
)

// AgentProfileDO 自定义子智能体持久化实体（agent_profiles 表是唯一真相源）。
type AgentProfileDO struct {
	ID           string         `gorm:"primaryKey;size:64" json:"id"`
	Name         string         `gorm:"size:64;uniqueIndex" json:"name"` // kebab-case，唯一；与内置名互斥
	Description  string         `gorm:"size:512" json:"description"`
	SystemPrompt string         `gorm:"type:text" json:"system_prompt"` // 人设 system 段；空 = 不注入
	ToolsAllow   string         `gorm:"type:text" json:"-"`             // JSON 数组（glob）；空 = 全部可见工具
	ToolsDeny    string         `gorm:"type:text" json:"-"`             // JSON 数组（glob）
	MemoryEnable bool           `gorm:"default:false" json:"memory_enable"`
	MaxTurns     int            `gorm:"default:0" json:"max_turns"` // <=0 用委派默认
	Enabled      bool           `gorm:"default:true" json:"enabled"`
	CreatedAt    int64          `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt    int64          `gorm:"autoUpdateTime:milli" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 固定表名。
func (AgentProfileDO) TableName() string { return "agent_profiles" }

// AgentProfileRESP 出参（设置页展示）。
type AgentProfileRESP struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	ToolsAllow   []string `json:"tools_allow,omitempty"`
	ToolsDeny    []string `json:"tools_deny,omitempty"`
	MemoryEnable bool     `json:"memory_enable"`
	MaxTurns     int      `json:"max_turns"`
	Enabled      bool     `json:"enabled"`
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `json:"updated_at"`
}

// AgentProfileREQ 创建/更新入参；name 创建后不可改（更新按 name 定位）。
type AgentProfileREQ struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt"`
	ToolsAllow   []string `json:"tools_allow"`
	ToolsDeny    []string `json:"tools_deny"`
	MemoryEnable bool     `json:"memory_enable"`
	MaxTurns     int      `json:"max_turns"`
	Enabled      *bool    `json:"enabled"`
}

// 包级错误变量；错误码段位 8200（AgentProfile 聚合根）。
var (
	ErrAgentProfileNotFound = pkg.New(8201, "agent profile not found", "")
	ErrAgentProfileBuiltin  = pkg.New(8202, "builtin agent name is reserved", "")
	ErrAgentProfileInvalid  = pkg.New(8203, "agent profile invalid", "")
)

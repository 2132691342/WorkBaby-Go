// Package domain 是业务聚合根：每个聚合根一个文件，含 DO/DTO/REQ/VO/RESP/枚举/常量/错误变量。
//
// 本文件：Memory 三层记忆聚合根（episodic / semantic / procedure）。
package domain

import (
	"gorm.io/gorm"

	"WorkBaby/internal/pkg"
)

// MemoryKind 记忆类型。
type MemoryKind string

const (
	MemoryKindEpisodic   MemoryKind = "episodic"
	MemoryKindSemantic   MemoryKind = "semantic"
	MemoryKindProcedural MemoryKind = "procedural"
)

// MemoryEpisodeDO 情景记忆：一次对话的压缩摘要 + 快照。
type MemoryEpisodeDO struct {
	ID         string         `gorm:"primaryKey;size:64" json:"id"`
	SessionID  string         `gorm:"size:64;index" json:"session_id"`
	Summary    string         `gorm:"type:text" json:"summary"`
	Transcript string         `gorm:"type:text" json:"transcript"` // JSON：消息列表
	FilePath   string         `gorm:"size:512" json:"file_path"`   // 快照文件位置
	Score      float64        `gorm:"default:0" json:"score"`      // 形成策略打分
	Triggers   string         `gorm:"type:text" json:"-"`          // JSON：触发关键词
	CreatedAt  int64          `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt  int64          `gorm:"autoUpdateTime:milli" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 固定表名。
func (MemoryEpisodeDO) TableName() string { return "memory_episodes" }

// MemoryFactDO 语义记忆：用户偏好 / 事实（v2 预留，先建表不实现写入）。
type MemoryFactDO struct {
	ID         string         `gorm:"primaryKey;size:64" json:"id"`
	Subject    string         `gorm:"size:256;index:idx_fact_subject_key" json:"subject"` // "user" / "project:xxx"
	Key        string         `gorm:"size:256;index:idx_fact_subject_key" json:"key"`
	Value      string         `gorm:"type:text" json:"value"`
	Confidence float64        `gorm:"default:1" json:"confidence"`
	Source     string         `gorm:"size:64" json:"source"` // 来源 episode id
	CreatedAt  int64          `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt  int64          `gorm:"autoUpdateTime:milli" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 固定表名。
func (MemoryFactDO) TableName() string { return "memory_facts" }

// MemoryProcedureDO 程序记忆：验证过的操作步骤（v2 预留）。
type MemoryProcedureDO struct {
	ID           string         `gorm:"primaryKey;size:64" json:"id"`
	Name         string         `gorm:"size:256;index" json:"name"`
	Steps        string         `gorm:"type:text" json:"-"` // JSON：步骤列表
	SuccessCount int            `gorm:"default:0" json:"success_count"`
	FailureCount int            `gorm:"default:0" json:"failure_count"`
	LastUsedAt   int64          `gorm:"index" json:"last_used_at"`
	CreatedAt    int64          `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt    int64          `gorm:"autoUpdateTime:milli" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 固定表名。
func (MemoryProcedureDO) TableName() string { return "memory_procedures" }

// MemoryEpisodeRESP 情景记忆出参（设置页/记忆面板展示）。
type MemoryEpisodeRESP struct {
	ID        string  `json:"id"`
	SessionID string  `json:"session_id"`
	Summary   string  `json:"summary"`
	Score     float64 `json:"score"`
	Triggers  string  `json:"triggers,omitempty"`
	CreatedAt int64   `json:"created_at"`
}

// MemoryFactRESP 语义记忆出参。
type MemoryFactRESP struct {
	ID         string  `json:"id"`
	Subject    string  `json:"subject"`
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source,omitempty"`
	CreatedAt  int64   `json:"created_at"`
	UpdatedAt  int64   `json:"updated_at"`
}

// MemoryProcedureRESP 程序记忆出参。
type MemoryProcedureRESP struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Steps        []string `json:"steps"`
	SuccessCount int      `json:"success_count"`
	FailureCount int      `json:"failure_count"`
	LastUsedAt   int64    `json:"last_used_at"`
	CreatedAt    int64    `json:"created_at"`
}

// MemoryEpisodeREQ 情景记忆写入入参（记忆面板手动记录）。
type MemoryEpisodeREQ struct {
	SessionID  string   `json:"session_id"`
	Summary    string   `json:"summary"`
	Transcript string   `json:"transcript"`
	Tags       []string `json:"tags"`
}

// MemoryFactREQ 语义记忆写入入参。
type MemoryFactREQ struct {
	Subject    string  `json:"subject"`
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence,omitempty"`
}

// MemoryProcedureREQ 程序记忆写入入参。
type MemoryProcedureREQ struct {
	Name  string   `json:"name"`
	Steps []string `json:"steps"`
}

// MemoryStatsRESP 记忆面板概览统计。
type MemoryStatsRESP struct {
	Episodes   int64 `json:"episodes"`
	Facts      int64 `json:"facts"`
	Procedures int64 `json:"procedures"`
}

// MemoryRecallRESP 统一召回命中。
type MemoryRecallRESP struct {
	Kind    string  `json:"kind"` // episodic / semantic / procedural
	Score   float64 `json:"score"`
	Source  string  `json:"source"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
}

// 包级错误变量；错误码段位 6000。
var (
	ErrMemoryWrite  = pkg.New(6001, "memory write failed", "")
	ErrMemoryIO     = pkg.New(6002, "long-term memory io error", "")
	ErrMemoryEval   = pkg.New(6003, "formation policy evaluate failed", "")
	ErrMemoryRecall = pkg.New(6004, "memory recall failed", "")
	ErrMemoryClean  = pkg.New(6005, "memory cleanup failed", "")
)

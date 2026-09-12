package domain

import "WorkBaby/internal/pkg"

// SessionVariableDO 会话变量：跨轮次保存的结构化状态（key 在会话内唯一）。
//
// 与「会话计划（todo）」的分工：plan 描述待办步骤与进度，变量承载任务上下文
// （当前处理对象、用户偏好、约定的短标识等）。变量以 system 段注入，模型可读写。
type SessionVariableDO struct {
	SessionID string `gorm:"primaryKey;size:64"  json:"session_id"`
	Key       string `gorm:"primaryKey;size:128" json:"key"`
	Value     string `gorm:"type:text"           json:"value"`
	UpdatedAt int64  `gorm:"autoUpdateTime:milli" json:"updated_at"`
}

// TableName 固定表名。
func (SessionVariableDO) TableName() string { return "session_variables" }

// SessionVarScope 变量作用域（三层 State）。
type SessionVarScope string

const (
	// SessionVarScopeUser 用户级：跨会话生效，落库 session_id 用 SessionVarUserScope 哨兵。
	SessionVarScopeUser SessionVarScope = "user"
	// SessionVarScopeSession 会话级：默认，仅当前会话可见。
	SessionVarScopeSession SessionVarScope = "session"
	// SessionVarScopeTemp run 级临时态：不落库，run 结束即失效。
	SessionVarScopeTemp SessionVarScope = "temp"
)

// SessionVarUserScope 用户级变量的持久化落点（session_id 哨兵，与真实会话 ID 不可能冲突）。
const SessionVarUserScope = "__user__"

// NormalizeSessionVarScope 归一化作用域入参；空值时默认会话级。
func NormalizeSessionVarScope(raw string) SessionVarScope {
	switch SessionVarScope(raw) {
	case SessionVarScopeUser:
		return SessionVarScopeUser
	case SessionVarScopeTemp:
		return SessionVarScopeTemp
	default:
		return SessionVarScopeSession
	}
}

// SessionVarItem 会话变量的视图形态。
type SessionVarItem struct {
	Key   string          `json:"key"`
	Value string          `json:"value"`
	Scope SessionVarScope `json:"scope"`
}

// SessionVarRESP 会话变量清单。
type SessionVarRESP struct {
	SessionID string           `json:"session_id"`
	Items     []SessionVarItem `json:"items"`
}

// SessionVarREQ 写入/删除单个变量的入参；scope 空值按会话级处理。
type SessionVarREQ struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Scope string `json:"scope,omitempty"`
}

// 变量规模约束：内容会进 system 段，失控长度或数量会挤占上下文预算。
const (
	SessionVarKeyMaxLen   = 128
	SessionVarValueMaxLen = 2000
	SessionVarCountMax    = 50
)

var (
	// ErrSessionVarInvalid 变量 key 为空 / 超长 / 数量超限。
	ErrSessionVarInvalid = pkg.New(5015, "会话变量不合法", "")
	// ErrSessionVarNotFound 删除或读取一个不存在的变量。
	ErrSessionVarNotFound = pkg.New(5016, "会话变量不存在", "")
)

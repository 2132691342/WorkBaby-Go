package domain

import "WorkBaby/internal/pkg"

// ChannelType 通知通道类型。
type ChannelType string

const (
	ChannelTypeEmail   ChannelType = "EMAIL"
	ChannelTypeWebhook ChannelType = "WEBHOOK"
	ChannelTypeConsole ChannelType = "CONSOLE" // 兼容历史配置；发送即写应用日志
)

// ChannelStatus 通道生命周期状态。
type ChannelStatus string

const (
	ChannelStatusStopped ChannelStatus = "stopped"
	ChannelStatusRunning ChannelStatus = "running"
)

// ChannelConfigDO 通知通道配置（channel_configs 表）。
type ChannelConfigDO struct {
	ID           string        `gorm:"primaryKey;size:64" json:"id"`
	UserID       string        `gorm:"size:64;index"      json:"user_id"`
	ChannelType  ChannelType   `gorm:"size:32;not null"   json:"channel_type"`
	Enabled      bool          `gorm:"not null"           json:"enabled"`
	ConfigJSON   string        `gorm:"type:text"          json:"config_json"`
	Status       ChannelStatus `gorm:"size:16"            json:"status"`
	LastError    string        `gorm:"type:text"          json:"last_error"`
	LastActiveAt int64         `gorm:"default:0"          json:"last_active_at"`
	CreatedAt    int64         `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt    int64         `gorm:"autoUpdateTime:milli" json:"updated_at"`
	DeletedAt    int64         `gorm:"default:0;index"    json:"deleted_at"` // 软删；unix milli
}

// TableName 固定表名。
func (ChannelConfigDO) TableName() string { return "channel_configs" }

// ChannelConfigREQ 创建/更新请求。configJson 由前端组装；emailTo/webhookUrl 兜底补进 configJson。
type ChannelConfigREQ struct {
	ChannelType      ChannelType `json:"channel_type"`
	Enabled          *bool       `json:"enabled"`
	ConfigJSON       string      `json:"config_json"`
	WebhookURL       string      `json:"webhook_url"`
	EmailTo          string      `json:"email_to"`
	EmailDisplayName string      `json:"email_display_name"`
}

// ChannelConfigRESP 出参（对齐前端 ChannelConfig）。
type ChannelConfigRESP struct {
	ID           string        `json:"id"`
	UserID       string        `json:"user_id"`
	ChannelType  ChannelType   `json:"channel_type"`
	Enabled      bool          `json:"enabled"`
	ConfigJSON   string        `json:"config_json"`
	WebhookURL   string        `json:"webhook_url"`
	Status       ChannelStatus `json:"status"`
	LastError    string        `json:"last_error"`
	LastActiveAt int64         `json:"last_active_at"`
	CreatedAt    int64         `json:"created_at"`
	UpdatedAt    int64         `json:"updated_at"`
}

// ChannelMessageLogDO 通道消息日志（channel_message_logs 表）。
type ChannelMessageLogDO struct {
	ID             string `gorm:"primaryKey;size:64" json:"id"`
	ChannelID      string `gorm:"size:64;index"      json:"channel_id"`
	Direction      string `gorm:"size:16"            json:"direction"`
	MessageType    string `gorm:"size:32"            json:"message_type"`
	UserExternalID string `gorm:"size:128"           json:"user_external_id"`
	SessionID      string `gorm:"size:64"            json:"session_id"`
	ContentSummary string `gorm:"type:text"          json:"content_summary"`
	Status         string `gorm:"size:16"            json:"status"`
	ErrorMessage   string `gorm:"type:text"          json:"error_message"`
	CreatedAt      int64  `gorm:"autoCreateTime:milli" json:"created_at"`
}

// TableName 固定表名。
func (ChannelMessageLogDO) TableName() string { return "channel_message_logs" }

// ChannelMessageLogRESP 出参（对齐前端 ChannelMessageLog）。
type ChannelMessageLogRESP struct {
	ID             string `json:"id"`
	ChannelID      string `json:"channel_id"`
	Direction      string `json:"direction"`
	MessageType    string `json:"message_type"`
	UserExternalID string `json:"user_external_id"`
	SessionID      string `json:"session_id"`
	ContentSummary string `json:"content_summary"`
	Status         string `json:"status"`
	ErrorMessage   string `json:"error_message"`
	CreatedAt      int64  `json:"created_at"`
}

// 错误变量；段位 9300-9306。
var (
	ErrChannelNotFound      = pkg.New(9304, "channel not found or disabled", "")
	ErrChannelNotRegistered = pkg.New(9304, "channel implementation not registered", "")
	ErrChannelInvalidConfig = pkg.New(9305, "channel config invalid", "")
)

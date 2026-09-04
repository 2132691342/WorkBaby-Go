package domain

import "WorkBaby/internal/pkg"

// ProviderKind LLM 协议族；OpenAI 兼容路径覆盖 DeepSeek/Moonshot/智谱/GLM/Ollama 兼容层。
type ProviderKind string

const (
	ProviderKindOpenAI    ProviderKind = "openai"
	ProviderKindAnthropic ProviderKind = "anthropic"
	ProviderKindOllama    ProviderKind = "ollama"
)

// 已知 ProviderKind 列表（用于设置面板 quick-add）。
var AllProviderKinds = []ProviderKind{
	ProviderKindOpenAI,
	ProviderKindAnthropic,
	ProviderKindOllama,
}

// ProviderKindMeta 单个 kind 的展示元数据（前端 Settings 下拉渲染用）。
//
// 关键约束：每加一种 kind，必须在 internal/llm/<kind>/ 实现 Provider 接口，
// 并在 registry.go buildOne 加 case —— 否则前端暴露的选项会让用户保存后
// 立刻撞上 3020「unknown provider kind」（CLAUDE.md §8 反模式「臆造 API 特性」）。
type ProviderKindMeta struct {
	Kind             ProviderKind `json:"kind"`
	Label            string       `json:"label"`             // 用户可见名（前端可 i18n 覆盖）
	BaseURLDefault   string       `json:"base_url_default"`  // 留空 = 走客户端内置默认；非空时新建表单自动填一次
	ModelPlaceholder string       `json:"model_placeholder"` // 前端输入框 placeholder（仅展示，不真填）
}

// ProviderKindsResp 所有已实现 ProviderKind 的元数据列表（前端从后端拉，避免错配）。
type ProviderKindsRESP []ProviderKindMeta

// AllProviderKindMetas 集中维护——新加 ProviderKind 时这里也必须加。
// 一旦在 domain 加新 ProviderKind 但忘了在这里登记或忘了实现 Provider，
// 前端会立刻"看不见这个 kind"，比"前端看得见后端报错"安全得多。
var AllProviderKindMetas = []ProviderKindMeta{
	{Kind: ProviderKindOpenAI, Label: "OpenAI 协议（GPT / DeepSeek / GLM / Kimi 等兼容）", BaseURLDefault: "", ModelPlaceholder: "gpt-4o"},
	{Kind: ProviderKindAnthropic, Label: "Anthropic 协议（Claude）", BaseURLDefault: "", ModelPlaceholder: "claude-sonnet-4-5"},
	{Kind: ProviderKindOllama, Label: "Ollama（本地）", BaseURLDefault: "http://localhost:11434", ModelPlaceholder: "llama3"},
}

// ProviderTier 模型档位（路由优先级）。
type ProviderTier string

const (
	ProviderTierPrimary ProviderTier = "primary"
	ProviderTierBackup  ProviderTier = "backup"
)

// AiProviderDO LLM Provider 持久化实体；apiKey 用 AES-GCM 加密落库（pkg/crypto.go）。
type AiProviderDO struct {
	ID               string       `gorm:"primaryKey;size:64"               json:"id"`
	Name             string       `gorm:"size:128"                          json:"name"`
	Kind             ProviderKind `gorm:"size:32"                          json:"kind"`
	APIKey           string       `gorm:"type:text"                         json:"-"` // 内部字段，不外露
	BaseURL          string       `gorm:"size:512"                          json:"base_url"`
	Model            string       `gorm:"size:128"                          json:"model"`
	Alias            string       `gorm:"size:128"                          json:"alias"`
	Tier             ProviderTier `gorm:"size:16"                          json:"tier"`
	Enabled          bool         `gorm:"default:true"                      json:"enabled"`
	ContextWindow    int          `gorm:"default:0"                         json:"context_window"`
	CompressRatio    float64      `gorm:"default:0.9"                       json:"compress_ratio"`
	Temperature      float64      `gorm:"default:0"                         json:"temperature"`     // 0 = 走全局默认
	ThinkingEffort   string       `gorm:"size:16;default:''"                json:"thinking_effort"` // 空 = 走全局默认
	CapabilitiesJSON string       `gorm:"type:text;column:capabilities_json" json:"capabilities_json"`
	PricingJSON      string       `gorm:"type:text;column:pricing_json"     json:"pricing_json"`
	CreatedAt        int64        `gorm:"autoCreateTime:milli"              json:"created_at"`
	UpdatedAt        int64        `gorm:"autoUpdateTime:milli"              json:"updated_at"`
}

// TableName 固定表名。
func (AiProviderDO) TableName() string { return "ai_providers" }

// AiProviderREQ 写入请求（apiKey 可选；空值表示不更新）。
type AiProviderREQ struct {
	Name             string       `json:"name"`
	Kind             ProviderKind `json:"kind"`
	APIKey           string       `json:"api_key,omitempty"`
	BaseURL          string       `json:"base_url"`
	Model            string       `json:"model"`
	Alias            string       `json:"alias"`
	Tier             ProviderTier `json:"tier"`
	Enabled          *bool        `json:"enabled,omitempty"`
	ContextWindow    int          `json:"context_window"`
	CompressRatio    float64      `json:"compress_ratio"`
	Temperature      float64      `json:"temperature"`
	ThinkingEffort   string       `json:"thinking_effort"`
	CapabilitiesJSON string       `json:"capabilities_json,omitempty"`
	PricingJSON      string       `json:"pricing_json,omitempty"`
}

// AiProviderRESP 出参（API Key 永远脱敏）。
type AiProviderRESP struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Kind             ProviderKind `json:"kind"`
	APIKeyMasked     string       `json:"api_key_masked"`
	BaseURL          string       `json:"base_url"`
	Model            string       `json:"model"`
	Alias            string       `json:"alias"`
	Tier             ProviderTier `json:"tier"`
	Enabled          bool         `json:"enabled"`
	ContextWindow    int          `json:"context_window"`
	CompressRatio    float64      `json:"compress_ratio"`
	Temperature      float64      `json:"temperature"`
	ThinkingEffort   string       `json:"thinking_effort"`
	CapabilitiesJSON string       `json:"capabilities_json"`
	PricingJSON      string       `json:"pricing_json"`
	CreatedAt        int64        `json:"created_at"`
	UpdatedAt        int64        `json:"updated_at"`
}

// AvailableModelRESP 聊天模型选择器用（扁平化）。
type AvailableModelRESP struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Kind           ProviderKind `json:"kind"`
	Model          string       `json:"model"`
	Alias          string       `json:"alias"`
	Tier           ProviderTier `json:"tier"`
	Enabled        bool         `json:"enabled"`
	ContextWindow  int          `json:"context_window"`
	CompressRatio  float64      `json:"compress_ratio"`
	Temperature    float64      `json:"temperature"`
	ThinkingEffort string       `json:"thinking_effort"`
}

// ModelConfigFile 是 {home}/model.json 的解析容器（配置文件字段 snake_case，与 DB/前端契约一致）。
type ModelConfigFile struct {
	Version   int             `json:"version"`
	Providers []ModelProvider `json:"providers"`
}

// ModelProvider model.json 中的单个 Provider 条目。
type ModelProvider struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Kind           ProviderKind `json:"kind"`
	BaseURL        string       `json:"base_url"`
	APIKey         string       `json:"api_key"`
	Model          string       `json:"model"`
	Alias          string       `json:"alias"`
	Tier           ProviderTier `json:"tier"`
	Enabled        *bool        `json:"enabled"`
	ContextWindow  int          `json:"context_window"`
	CompressRatio  float64      `json:"compress_ratio"`
	Temperature    float64      `json:"temperature"`
	ThinkingEffort string       `json:"thinking_effort"`
}

// ProviderCircuitRESP Provider 熔断/就绪状态（前端模型选择器徽标用）。
type ProviderCircuitRESP struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Enabled             bool   `json:"enabled"`
	State               string `json:"state"` // CLOSED / OPEN / HALF_OPEN
	ConsecutiveFailures int    `json:"consecutive_failures"`
	RetryInMs           int64  `json:"retry_in_ms"`
	// Reason 未就绪的原因（apiKey 缺失/解密失败/kind 未知等）；ready 时为空。
	Reason string `json:"reason"`
}

// ProviderReloadRESP model.json 热重载结果。
type ProviderReloadRESP struct {
	Synced int `json:"synced"`
}

// ExecAgentRESP exec 工具二进制白名单（settings/exec/agent）。
type ExecAgentRESP struct {
	Binaries []string `json:"binaries"`
}

// 包级错误变量；错误码段 3000-3999（LLM/Provider）。
var (
	ErrProviderNotFound = pkg.New(3001, "provider not found", "")
	ErrProviderInvalid  = pkg.New(3002, "provider invalid (missing apiKey/baseUrl/model)", "")
	ErrProviderNotReady = pkg.New(3003, "provider not ready (test connect failed)", "")
)

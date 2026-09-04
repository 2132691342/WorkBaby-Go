package domain

// ToolMeta 工具元信息（设置页展示；RiskLevel 取 tool 包枚举的字符串值）。
type ToolMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	RiskLevel   string `json:"risk_level"`
	Enabled     bool   `json:"enabled"`
	SchemaJSON  string `json:"schema_json"`
}

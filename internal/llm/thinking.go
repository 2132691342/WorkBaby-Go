// Package llm 内部使用的思维强度归一化；service/api 层共享，避免重复定义。
package llm

// ThinkingFromEffort 前端/数据库里的 effort 字符串 → ThinkingConfig。
//   - off / "" → nil（不注入 thinking 字段；由 Provider/全局默认兜底）
//   - low / medium / high → enabled + 对应 budget_tokens
//   - 其它 → nil（未知值不传）
func ThinkingFromEffort(effort string) *ThinkingConfig {
	switch effort {
	case "off":
		return &ThinkingConfig{Type: "disabled"}
	case "low":
		return &ThinkingConfig{Type: "enabled", BudgetTokens: 2048}
	case "high":
		return &ThinkingConfig{Type: "enabled", BudgetTokens: 16384}
	case "medium":
		return &ThinkingConfig{Type: "enabled", BudgetTokens: 8192}
	default:
		return nil
	}
}

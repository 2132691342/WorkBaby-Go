package harness

import (
	"WorkBaby/internal/llm"
)

// TokenUsageAccumulator 累加 token（含缓存读写，供仪表盘三线拆分）。
// Runner 持有唯一实例并在每轮回调 AfterTurn；中间件链接口本身不再导出（AfterTurn
// 从未被外部调用过——runner.go 同步直调该实例方法，defaultMiddlewares 也已下线）。
type TokenUsageAccumulator struct {
	Input      int
	Output     int
	CacheRead  int
	CacheWrite int
	Total      int
}

// AfterTurn 累加本轮 token 用量（含缓存读写分项）。
func (a *TokenUsageAccumulator) AfterTurn(u llm.TokenUsage) {
	a.Input += u.InputTokens
	a.Output += u.OutputTokens
	a.CacheRead += u.CacheReadTokens
	a.CacheWrite += u.CacheWriteTokens
	a.Total += u.TotalTokens
}

// Snapshot 当前累计快照（Total 为 0 时用三项之和兜底，部分上游不回 total 字段）。
func (a *TokenUsageAccumulator) Snapshot() llm.TokenUsage {
	total := a.Total
	if total == 0 {
		total = a.Input + a.Output + a.CacheRead
	}
	return llm.TokenUsage{
		InputTokens:      a.Input,
		OutputTokens:     a.Output,
		CacheReadTokens:  a.CacheRead,
		CacheWriteTokens: a.CacheWrite,
		TotalTokens:      total,
	}
}

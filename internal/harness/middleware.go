package harness

import (
	"strings"

	"WorkBaby/internal/llm"
)

// Middleware 横切层接口；提供 TokenUsageAccumulator（统计）与 HistoryTruncator（对折截断）。
type Middleware interface {
	Name() string
	// BeforeTurn 在每轮 LLM 调用前调整 messages。
	BeforeTurn(ms []*llm.Message) []*llm.Message
	// AfterTurn 在每轮响应后做统计。
	AfterTurn(usage llm.TokenUsage)
}

// TokenUsageAccumulator 累加 token（含缓存读写，供仪表盘三线拆分）。
type TokenUsageAccumulator struct {
	Input      int
	Output     int
	CacheRead  int
	CacheWrite int
	Total      int
}

func (a *TokenUsageAccumulator) Name() string                               { return "token-accumulator" }
func (a *TokenUsageAccumulator) BeforeTurn(m []*llm.Message) []*llm.Message { return m }
func (a *TokenUsageAccumulator) AfterTurn(u llm.TokenUsage) {
	a.Input += u.InputTokens
	a.Output += u.OutputTokens
	a.CacheRead += u.CacheReadTokens
	a.CacheWrite += u.CacheWriteTokens
	a.Total += u.TotalTokens
}

// Snapshot 当前累计快照（Total 为 0 时用三项之和兜底，部分上游不回 total）。
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

// HistoryTruncator 上下文截断：
//
// 估算 token = utf8 字符数/4（粗略；无 tokenizer 的轻量近似）。
// 当估算总量超过 threshold 时，对半截断最旧的 user/assistant 消息（保留 system 与最新一条）。
type HistoryTruncator struct {
	Threshold int     // 触发截断的估算 token 数；<=0 表示不启用
	Ratio     float64 // 截断比例（保留比例），默认 0.9
}

// NewHistoryTruncator 构造截断器。
func NewHistoryTruncator(threshold int, ratio float64) *HistoryTruncator {
	if ratio <= 0 {
		ratio = 0.9
	}
	return &HistoryTruncator{Threshold: threshold, Ratio: ratio}
}

func (t *HistoryTruncator) Name() string               { return "history-truncator" }
func (t *HistoryTruncator) AfterTurn(_ llm.TokenUsage) {}

// BeforeTurn 估算历史 token，超阈值时对折截断最旧消息。
func (t *HistoryTruncator) BeforeTurn(ms []*llm.Message) []*llm.Message {
	if t.Threshold <= 0 || len(ms) <= 2 {
		return ms
	}
	if estimateTokens(ms) <= t.Threshold {
		return ms
	}
	keep := int(float64(len(ms)) * t.Ratio)
	if keep < 2 {
		keep = 2
	}
	if keep >= len(ms) {
		return ms
	}
	// 保留首条（通常是 system）与末尾最新 keep-1 条
	out := make([]*llm.Message, 0, keep)
	out = append(out, ms[0])
	out = append(out, ms[len(ms)-keep+1:]...)
	return out
}

// estimateTokens 粗略估算消息 token 总量（utf8 字符数 / 4）。
func estimateTokens(ms []*llm.Message) int {
	n := 0
	for _, m := range ms {
		if m == nil {
			continue
		}
		n += len([]rune(m.Content)) + len([]rune(m.Thinking))
	}
	return n / 4
}

// defaultMiddlewares 默认中间件链。
func defaultMiddlewares() []Middleware {
	return []Middleware{&TokenUsageAccumulator{}}
}

// truncateString 兼容旧引用（runner 已内联 truncate）。
var _ = strings.Builder{}

package harness

import "WorkBaby/internal/llm"

// Compressor 上下文压缩器。
//
// Compress 在每轮 LLM 调用前执行：把消息序列压回预算内，返回新序列（不就地修改原切片）。
// 装配方可按需注入更强实现（如 Auto：watermark 触发 LLM 摘要并保留 touched files），
// MicroCompressor 为默认确定性实现（不调 LLM）。
type Compressor interface {
	Compress(msgs []*llm.Message, budgetTokens int) []*llm.Message
}

// MicroCompressor 确定性轻量压缩：优先清理最旧的 tool 结果消息，仍超预算时截断最旧对话。
// 不调 LLM（零成本），适合作为每次超预算时的快速回收。
type MicroCompressor struct{}

// Compress 实现 Compressor。
func (MicroCompressor) Compress(msgs []*llm.Message, budgetTokens int) []*llm.Message {
	if budgetTokens <= 0 || EstimateTokens(msgs) <= budgetTokens {
		return msgs
	}
	out := append([]*llm.Message(nil), msgs...)

	// 第一遍：删除最旧 tool 结果（保留首条 system 与结构），直到预算内
	for i := 1; i < len(out); i++ {
		if EstimateTokens(out) <= budgetTokens {
			break
		}
		if out[i] != nil && out[i].Role == llm.RoleTool {
			out = append(out[:i], out[i+1:]...)
			i--
		}
	}
	if EstimateTokens(out) <= budgetTokens {
		return out
	}

	// 第二遍：仍超预算时对半截断最旧 user/assistant（保留首条 system 与最新 keep-1 条）
	keep := len(out) / 2
	if keep < 2 {
		keep = 2
	}
	if keep < len(out) {
		trimmed := make([]*llm.Message, 0, keep)
		trimmed = append(trimmed, out[0])
		trimmed = append(trimmed, out[len(out)-keep+1:]...)
		out = trimmed
	}
	return out
}

// EstimateTokens 估算消息 token 总量（utf8 rune 数 / 4，轻量近似；无 tokenizer）。
func EstimateTokens(ms []*llm.Message) int {
	n := 0
	for _, m := range ms {
		if m == nil {
			continue
		}
		n += len([]rune(m.Content)) + len([]rune(m.Thinking))
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				n += len([]rune(tc.Function.Name)) + len([]rune(tc.Function.Arguments))
			}
		}
	}
	return n / 4
}

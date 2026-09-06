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

// MicroCompressor 确定性轻量压缩：先把最旧的「assistant(tool_calls)+其 tool 结果」
// 整段折叠成一条占位 assistant 消息，仍超预算时按安全切点对半截断。
// 不调 LLM（零成本），适合作为每次超预算时的快速回收。
type MicroCompressor struct{}

// Compress 实现 Compressor。
//
// <p>协议铁律：assistant(tool_calls) 与其后连续 tool 结果必须同进同出——
// 只删 tool 结果会让上游以「tool_calls must be followed by tool messages」拒绝；
// 盲切把 tool 结果留在尾部开头则是「tool result's tool id not found」。
// 两条路径都以段为最小单位，绝不制造孤儿。
func (MicroCompressor) Compress(msgs []*llm.Message, budgetTokens int) []*llm.Message {
	if budgetTokens <= 0 || EstimateTokens(msgs) <= budgetTokens {
		return msgs
	}
	out := append([]*llm.Message(nil), msgs...)

	// 第一遍：把最旧工具段（assistant(tool_calls)+连续 tool 结果）折叠为一条占位消息
	for i := 1; i < len(out); i++ {
		if EstimateTokens(out) <= budgetTokens {
			return out
		}
		m := out[i]
		if m == nil || m.Role != llm.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		j := i + 1
		for j < len(out) && out[j] != nil && out[j].Role == llm.RoleTool {
			j++
		}
		folded := &llm.Message{Role: llm.RoleAssistant, Content: "[早期工具调用与结果已省略]"}
		out = append(out[:i], append([]*llm.Message{folded}, out[j:]...)...)
		i-- // 折叠点前移一格，下一轮从当前位置继续
	}
	if EstimateTokens(out) <= budgetTokens {
		return out
	}

	// 第二遍：仍超预算时按安全切点对半截断（保留首条 system 与尾部一半）
	keep := len(out) / 2
	if keep < 2 {
		keep = 2
	}
	cut := safeTailStart(out, len(out)-keep)
	if cut > 1 {
		trimmed := make([]*llm.Message, 0, len(out)-cut+1)
		trimmed = append(trimmed, out[0])
		trimmed = append(trimmed, out[cut:]...)
		out = trimmed
	}
	return out
}

// safeTailStart 从 want 起向前（索引减小方向）找最近的「尾部安全切点」：切点消息不是 tool。
// 尾部以非 tool 消息开头时，tool 消息只会作为其 assistant 的后继整体进入尾部，配对完整。
// 向前找还能把被切断的工具对整体救回尾部（want 落在 tool 块中间时，停在其 assistant 上）。
func safeTailStart(ms []*llm.Message, want int) int {
	for i := want; i > 1; i-- {
		if ms[i] != nil && ms[i].Role != llm.RoleTool {
			return i
		}
	}
	return 1
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

package harness

import (
	"encoding/json"
	"fmt"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/pkg"
)

// Compressor 上下文压缩器。
//
// Compress 在每轮 LLM 调用前执行：把消息序列压回预算内，返回新序列（不就地修改原切片）。
// 装配方可按需注入更强实现（如 Auto：watermark 触发 LLM 摘要并保留 touched files），
// MicroCompressor 为默认确定性实现（不调 LLM）。
type Compressor interface {
	Compress(msgs []*llm.Message, budgetTokens int) []*llm.Message
}

// MicroCompressor 确定性轻量压缩：最旧的「assistant(tool_calls)+连续 tool 结果」
// 整段折叠为一条占位消息，仍超预算时按安全切点对半截断；不调 LLM（零成本）。
// 占位文案声明「结果已消费」，避免模型为找回 payload 重跑有副作用的工具。
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
		folded := &llm.Message{
			Role: llm.RoleAssistant,
			Content: fmt.Sprintf(
				"[早期工具段已折叠（%d 个调用），结果已消费，请勿重跑以避免副作用]",
				j-i,
			),
		}
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

// EstimateTokens 估算消息 token 总量（加权近似；无 tokenizer）。
//
// 口径：CJK 字符按 1 token/字（主流分词器对中文约 0.6~1.5 token/字，取 1 偏保守），
// 其余按 4 字符/token 向上取整；每条消息另计角色与分隔开销。
// 约束：按字符均摊的估算对中文会低估数倍，直接导致压缩永不触发、上游上下文超限。
func EstimateTokens(ms []*llm.Message) int {
	n := 0
	for _, m := range ms {
		if m == nil {
			continue
		}
		n += pkg.EstimateTextTokens(m.Content) + pkg.EstimateTextTokens(m.Thinking)
		for _, tc := range m.ToolCalls {
			n += pkg.EstimateTextTokens(tc.Function.Name) + pkg.EstimateTextTokens(tc.Function.Arguments)
		}
		n += MessageOverheadTokens
	}
	return n
}

// EstimateToolTokens 估算工具定义的 token 占用（名称 + 描述 + 参数 schema）。
//
// 工具定义同样计入上游 prompt_tokens，估算时必须与消息一起纳入，
// 否则实测回推校准会把固定的工具开销误算成文本密度偏差。
func EstimateToolTokens(defs []llm.ToolDefinition) int {
	n := 0
	for _, d := range defs {
		n += pkg.EstimateTextTokens(d.Name) + pkg.EstimateTextTokens(d.Description) + MessageOverheadTokens
		if d.Parameters != nil {
			if b, err := json.Marshal(d.Parameters); err == nil {
				n += pkg.EstimateTextTokens(string(b))
			}
		}
	}
	return n
}

// MessageOverheadTokens 每条消息/工具定义的固定开销（role 标记 + 分隔 + 结束符）。
const MessageOverheadTokens = 7

// 校准系数边界：单轮样本可能极端（首轮全缓存命中 / 大工具结果），限幅避免系数被打飞。
const (
	minTokenScale     = 0.5
	maxTokenScale     = 8.0
	tokenScaleInertia = 0.5 // EMA 惯性：历史系数权重
)

// calibratedBudget 把真实预算折算成「估算口径」的预算交给压缩器。
//
// 压缩器内部以 EstimateTokens(out) <= budget 判定，折算后等价于
// 「估算 × 系数 ≤ 真实预算」。另外：上一轮实测已超预算时，估算再低也必须压缩——
// 实测值比估算可靠，这条硬触发是「估算低估 → 上游 Prompt exceeds max length」的兜底。
func (r *Runner) calibratedBudget(budgetTokens int) int {
	if budgetTokens <= 0 {
		return 0
	}
	if r.tokenScale <= 0 {
		r.tokenScale = 1
	}
	if r.lastMeasuredIn >= budgetTokens {
		return 1
	}
	scaled := int(float64(budgetTokens) / r.tokenScale)
	if scaled < 1 {
		scaled = 1
	}
	return scaled
}

// updateTokenScale EMA 更新校准系数：新样本与历史各占一半，并对单个样本先限幅。
func updateTokenScale(cur, ratio float64) float64 {
	if cur <= 0 {
		cur = 1
	}
	if ratio <= 0 {
		return cur
	}
	if ratio < minTokenScale {
		ratio = minTokenScale
	}
	if ratio > maxTokenScale {
		ratio = maxTokenScale
	}
	next := cur*tokenScaleInertia + ratio*(1-tokenScaleInertia)
	if next < minTokenScale {
		return minTokenScale
	}
	if next > maxTokenScale {
		return maxTokenScale
	}
	return next
}

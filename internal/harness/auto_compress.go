package harness

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/pkg"
)

// ContextCompressor 可选增强压缩接口：需要 ctx 的压缩（LLM 摘要）；
// runner 优先使用，未实现者退回 Compress（确定性 Micro）。
type ContextCompressor interface {
	CompressCtx(ctx context.Context, msgs []*llm.Message, budgetTokens int) []*llm.Message
}

// 摘要压缩参数。
const (
	autoPerMsgRunes   = 300   // 序列化每条消息的截断长度
	autoMaxHeadRunes  = 80000 // 待摘要头部序列化上限，超过直接 Micro（摘要本身也烧钱）
	autoSummaryTokens = 1024  // 摘要输出上限
	autoSummarizeWait = 25 * time.Second
)

// AutoCompressor 结构化摘要压缩器：超预算时把早期历史压成六段交接摘要
// （目标/进度/决策/文件/下一步/约束），切点不拆 tool 调用对，摘要迭代更新；
// 摘要失败降级 MicroCompressor。被移除的历史变成「交接单」而非消失。
type AutoCompressor struct {
	provider llm.Provider
	model    string
	fallback MicroCompressor
	// Notify 压缩完成回调（可选）：service 层发可见事件/日志。
	Notify func(removed int, summary string)
	// OnUsage 压缩摘要调用的用量回调（可选）：摘要消耗真实 token，
	// 不上报会让「总消耗」统计低于实际。service 层据此落 token_usages。
	OnUsage func(usage llm.TokenUsage)

	mu               sync.Mutex
	cachedSummary    string
	cachedHeadCount  int
	cachedPrefixHash uint64
}

// NewAutoCompressor 构造；provider 为 nil 时调用方应直接用 MicroCompressor。
func NewAutoCompressor(provider llm.Provider, model string) *AutoCompressor {
	return &AutoCompressor{provider: provider, model: model}
}

// Compress 实现 Compressor（无 ctx 入口）：自带超时 ctx 走 CompressCtx。
func (a *AutoCompressor) Compress(msgs []*llm.Message, budgetTokens int) []*llm.Message {
	ctx, cancel := context.WithTimeout(context.Background(), autoSummarizeWait+5*time.Second)
	defer cancel()
	return a.CompressCtx(ctx, msgs, budgetTokens)
}

// CompressCtx 实现 ContextCompressor。
func (a *AutoCompressor) CompressCtx(ctx context.Context, msgs []*llm.Message, budgetTokens int) []*llm.Message {
	if budgetTokens <= 0 || len(msgs) < 4 || EstimateTokens(msgs) <= budgetTokens {
		return msgs
	}

	// 1) 选切点：尾部保留约 budget/2（最近工作集），向前对齐到不拆 tool 对的边界
	reserve := budgetTokens / 2
	tailStart := len(msgs) - 1
	acc := 0
	for tailStart > 1 && acc < reserve {
		acc += estimateOne(msgs[tailStart])
		tailStart--
	}
	// 切点落在 tool 结果上 → 前移到其 assistant(tool_calls)，尾部不留孤儿 tool 消息
	for tailStart > 1 && msgs[tailStart] != nil && msgs[tailStart].Role == llm.RoleTool {
		tailStart--
	}
	// 保护最后一轮：tailStart 不越过最后一个 user 锚点——那是模型正在处理的上下文，
	// 被摘要吞掉会直接导致「忘了刚才说啥」的幻觉。
	lastUser := 1
	for i := 1; i < len(msgs); i++ {
		if msgs[i] != nil && msgs[i].Role == llm.RoleUser {
			lastUser = i
		}
	}
	if tailStart < lastUser {
		tailStart = lastUser
	}
	head := msgs[1:tailStart]
	if len(head) == 0 {
		return a.fallback.Compress(msgs, budgetTokens)
	}

	// 2) 摘要（迭代更新：旧摘要覆盖前缀相同则并入）
	a.mu.Lock()
	prev, newHead := "", head
	if a.cachedSummary != "" && a.cachedHeadCount <= len(head) {
		if hashMessages(head[:a.cachedHeadCount]) == a.cachedPrefixHash {
			prev = a.cachedSummary
			newHead = head[a.cachedHeadCount:]
		}
	}
	a.mu.Unlock()

	summary, err := a.summarize(ctx, prev, newHead)
	if err != nil || strings.TrimSpace(summary) == "" {
		return a.fallback.Compress(msgs, budgetTokens)
	}

	a.mu.Lock()
	a.cachedSummary = summary
	a.cachedHeadCount = len(head)
	a.cachedPrefixHash = hashMessages(head)
	a.mu.Unlock()

	// 3) 组装：system + 交接摘要 + 完整尾部；仍超预算再 Micro 兜底
	out := make([]*llm.Message, 0, len(msgs)-len(head)+2)
	out = append(out, msgs[0])
	out = append(out, llm.SystemMessage(
		"[上下文交接摘要·早期过程已压缩：此前的对话轮次与工具结果已归档，结果均已消费，请勿重跑有副作用的工具；如需细节请重新读取对应文件]\n"+summary))
	out = append(out, msgs[tailStart:]...)
	if EstimateTokens(out) > budgetTokens {
		out = a.fallback.Compress(out, budgetTokens)
	}
	if a.Notify != nil {
		a.Notify(len(msgs)-len(out), summary)
	}
	return out
}

// summarize 调 provider.Chat 生成六段交接摘要；prev 非空时迭代合并。
func (a *AutoCompressor) summarize(ctx context.Context, prev string, head []*llm.Message) (string, error) {
	if a.provider == nil {
		return "", fmt.Errorf("auto compressor: provider nil")
	}
	var b strings.Builder
	total := 0
	for _, m := range head {
		line := serializeForSummary(m)
		if line == "" {
			continue
		}
		if total+len(line) > autoMaxHeadRunes {
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
		total += len(line)
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("auto compressor: empty head")
	}

	prompt := "你是上下文压缩官。下面是将被移除的早期对话片段，请产出结构化交接摘要，让后续模型无缝继续任务。\n" +
		"严格按六段输出：[目标] 用户最终目标；[进度] 已完成步骤与结论；[决策] 关键决策及原因；" +
		"[文件] 读过/写过/创建的文件清单；[下一步] 接下来应做；[约束] 用户约束与偏好。\n" +
		"有旧摘要时与新增内容合并迭代更新，直接输出摘要正文。\n"
	if prev != "" {
		prompt += "<旧摘要>\n" + prev + "\n</旧摘要>\n"
	}
	prompt += "<对话片段>\n" + b.String() + "\n</对话片段>"

	sctx, cancel := context.WithTimeout(ctx, autoSummarizeWait)
	defer cancel()
	resp, err := a.provider.Chat(sctx, &llm.ChatRequest{
		Model:     a.model,
		Messages:  []*llm.Message{llm.UserMessage(prompt)},
		MaxTokens: intPtr(autoSummaryTokens),
	})
	if err != nil {
		return "", err
	}
	// 压缩摘要消耗真实 token：不计量会让总消耗统计系统性偏低
	if a.OnUsage != nil {
		a.OnUsage(resp.Usage)
	}
	return resp.Message.Content, nil
}

// serializeForSummary 单条消息压缩序列化：角色 + 工具名 + 截断正文。
func serializeForSummary(m *llm.Message) string {
	if m == nil {
		return ""
	}
	switch m.Role {
	case llm.RoleSystem:
		return "" // system 不参与头部摘要（常驻保留）
	case llm.RoleTool:
		return fmt.Sprintf("tool %s => %s", m.ToolName, truncateRunes(m.Content, autoPerMsgRunes/2))
	case llm.RoleAssistant:
		var sb strings.Builder
		if len(m.ToolCalls) > 0 {
			names := make([]string, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				names = append(names, tc.Function.Name)
			}
			sb.WriteString("assistant 调用工具[" + strings.Join(names, ",") + "] ")
		}
		sb.WriteString(truncateRunes(m.Content, autoPerMsgRunes))
		return sb.String()
	default:
		return string(m.Role) + ": " + truncateRunes(m.Content, autoPerMsgRunes)
	}
}

// estimateOne 单条消息 token 估算（与 EstimateTokens 完全同口径：CJK 计权 +
// tool_calls 计入 + 消息固定开销）。口径不一会让 Auto 压缩的尾部保留量失准
//（中文按字符/4 会低估数倍，摘要吞掉本该保留的近期上下文）。
func estimateOne(m *llm.Message) int {
	if m == nil {
		return 0
	}
	n := pkg.EstimateTextTokens(m.Content) + pkg.EstimateTextTokens(m.Thinking)
	for _, tc := range m.ToolCalls {
		n += pkg.EstimateTextTokens(tc.Function.Name) + pkg.EstimateTextTokens(tc.Function.Arguments)
	}
	return n + MessageOverheadTokens
}

// hashMessages 消息序列指纹（fnv64：序号+角色+长度+正文前缀），供迭代摘要前缀判定。
func hashMessages(ms []*llm.Message) uint64 {
	h := fnv.New64a()
	for i, m := range ms {
		if m == nil {
			continue
		}
		fmt.Fprintf(h, "%d|%s|%d|", i, m.Role, len(m.Content))
		_, _ = h.Write([]byte(truncateRunes(m.Content, 64)))
	}
	return h.Sum64()
}

func intPtr(v int) *int { return &v }

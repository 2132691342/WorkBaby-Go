package capability

import (
	"context"
	"regexp"
	"strings"

	"WorkBaby/internal/harness"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/memory"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/tool"
	"WorkBaby/internal/tool/memorywrite"
)

// MemoryStore 记忆读写契约（memory.Service 实现）。
type MemoryStore interface {
	memory.Memory
	WriteFact(ctx context.Context, p memory.FactProposal) (string, error)
	WriteProcedure(ctx context.Context, p memory.ProcedureProposal) (string, error)
}

// memoryCap 记忆能力：长期记忆与跨会话召回自动注入、模型可主动写入、run 后自动形成。
type memoryCap struct {
	mem     MemoryStore
	enabled func(ctx context.Context) bool // 全局开关回调（system_settings）
	tools   []tool.Tool
}

// NewMemory 构造记忆能力；enabled 为 nil 时按开启处理。
func NewMemory(mem MemoryStore, enabled func(ctx context.Context) bool) Capability {
	c := &memoryCap{mem: mem, enabled: enabled}
	if mem != nil {
		c.tools = []tool.Tool{memorywrite.New(mem, harness.SessionIDFromCtx)}
	}
	return c
}

func (c *memoryCap) ID() string { return "memory" }

func (c *memoryCap) Tools() []tool.Tool { return c.tools }

func (c *memoryCap) allowed(ctx context.Context) bool {
	if c.enabled == nil {
		return true
	}
	return c.enabled(ctx)
}

func (c *memoryCap) Preload(ctx context.Context, p *PreloadCtx) ([]harness.ContextPiece, error) {
	if c.mem == nil || !p.Def.Memory.Enabled || !c.allowed(ctx) {
		return nil, nil
	}
	out := make([]harness.ContextPiece, 0, 2)
	if ltm := c.mem.LongTerm(ctx, p.SessionID); ltm != "" {
		// MEMORY.md 上限 100KB，全量注入会一口吃掉上下文预算且挤掉工具定义；
		// 只注入尾部（按时间追加，最近内容最相关），截断量与 harness 上下文预算同量级。
		out = append(out, harness.ContextPiece{Key: "memory", Title: "本会话长期记忆",
			Body: tailRunes(ltm, maxMemoryInjectRunes)})
	}
	// 跨会话召回：按本轮输入取相关条目，否则形成过的记忆永远进不了上下文
	if recalled := c.recallText(ctx, p.UserInput, p.Def.Memory.RecallLimit); recalled != "" {
		out = append(out, harness.ContextPiece{Key: "recall", Title: "相关记忆（自动召回，仅供参考）", Body: recalled})
	}
	return out, nil
}

// maxMemoryInjectRunes 长期记忆注入上下文的 rune 上限（MEMORY.md 上限 100KB，不能全量进 prompt）。
const maxMemoryInjectRunes = 4000

// tailRunes 取字符串末尾 n 个 rune：长期记忆按时间追加，最近内容最相关。
func tailRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// recallText 按本轮输入召回并渲染；无命中返回空。纯本地检索，不调 LLM、不阻塞首 token。
func (c *memoryCap) recallText(ctx context.Context, query string, limit int) string {
	if strings.TrimSpace(query) == "" {
		return ""
	}
	if limit <= 0 {
		limit = 5
	}
	hits := c.mem.Recall(ctx, query, memory.RecallOpts{TopK: limit})
	if len(hits) == 0 {
		return ""
	}
	var sb strings.Builder
	for i := range hits {
		sb.WriteString("- [")
		sb.WriteString(string(hits[i].Kind))
		sb.WriteString("] ")
		if hits[i].Title != "" {
			sb.WriteString(hits[i].Title)
			sb.WriteString("：")
		}
		sb.WriteString(pkg.TruncateRunes(hits[i].Snippet, 200))
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// Capture 记忆形成：评估本次对话 → 情景/语义/程序记忆落库 + 长期记忆追加。
func (c *memoryCap) Capture(ctx context.Context, p *CaptureCtx) error {
	if c.mem == nil || !p.Def.Memory.Formation || !c.allowed(ctx) {
		return nil
	}
	result := c.mem.Evaluate(ctx, p.SessionID, p.Transcript)
	if result.Episode != nil {
		if _, err := c.mem.WriteEpisode(ctx, *result.Episode); err != nil {
			pkg.L.Warn("write episode failed", "session", p.SessionID, "err", err.Error())
		}
	}
	for i := range result.Semantic {
		if _, err := c.mem.WriteFact(ctx, result.Semantic[i]); err != nil {
			pkg.L.Warn("write fact failed", "session", p.SessionID, "err", err.Error())
		}
	}
	for i := range result.Procedural {
		if _, err := c.mem.WriteProcedure(ctx, result.Procedural[i]); err != nil {
			pkg.L.Warn("write procedure failed", "session", p.SessionID, "err", err.Error())
		}
	}
	if delta := longTermDelta(p.Transcript); delta != "" {
		if err := c.mem.AppendLongTerm(ctx, p.SessionID, delta); err != nil {
			pkg.L.Warn("append long-term failed", "session", p.SessionID, "err", err.Error())
		}
	}
	return nil
}

// longTermDelta 长期记忆追加模板：首条用户消息 + 末条助手消息 + 工具使用。
// 助手正文先剥离 <think> 块：兼容端点把推理写进正文，原样入档会污染记忆。
func longTermDelta(transcript []llm.Message) string {
	var firstUser, lastAssistant, tools []string
	for _, m := range transcript {
		switch m.Role {
		case llm.RoleUser:
			if m.Content != "" && len(firstUser) == 0 {
				firstUser = append(firstUser, pkg.TruncateRunes(m.Content, 200))
			}
		case llm.RoleAssistant:
			if m.Content != "" {
				if clean := stripThink(m.Content); clean != "" {
					lastAssistant = []string{pkg.TruncateRunes(clean, 200)}
				}
			}
			for _, tc := range m.ToolCalls {
				tools = append(tools, tc.Function.Name)
			}
		case llm.RoleTool:
			if m.ToolName != "" {
				tools = append(tools, m.ToolName)
			}
		}
	}
	out := ""
	if len(firstUser) > 0 {
		out += "用户: " + firstUser[0]
	}
	if len(lastAssistant) > 0 {
		if out != "" {
			out += "\n"
		}
		out += "助手: " + lastAssistant[0]
	}
	if len(tools) > 0 {
		out += "\n使用工具: " + joinUnique(tools)
	}
	return out
}

// thinkClosed / thinkOpen 剥离兼容端点写进正文的推理段：闭合块全局移除，
// 未闭合尾部（流式残留）一并移除。
var (
	thinkClosedRe = regexp.MustCompile(`(?s)<think>.*?</think>`)
	thinkOpenRe   = regexp.MustCompile(`(?s)<think>.*$`)
)

func stripThink(s string) string {
	if !strings.Contains(s, "<think>") {
		return strings.TrimSpace(s)
	}
	s = thinkClosedRe.ReplaceAllString(s, "")
	s = thinkOpenRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// joinUnique 去重后按逗号拼接。
func joinUnique(items []string) string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		if it == "" || seen[it] {
			continue
		}
		seen[it] = true
		out = append(out, it)
	}
	return strings.Join(out, ", ")
}

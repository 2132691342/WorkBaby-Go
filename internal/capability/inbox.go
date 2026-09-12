package capability

import (
	"context"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/tool"
)

// Extractor run 终局候选抽取器（用本 run 的模型做一次 strict-JSON 抽取）。
type Extractor func(ctx context.Context, c *CaptureCtx) ([]domain.InboxCandidate, error)

// InboxSink 候选落库契约（service.InboxService 实现）。
type InboxSink interface {
	Submit(ctx context.Context, sessionID, runID string, candidates []domain.InboxCandidate) (int, error)
}

// inboxCap 回写收件箱能力：run 终局抽取记忆/技能候选 → 待审收件箱（人审合入）。
//
// 与 memory 能力的职责边界：memory 负责确定性形成（免审直落库）；
// inbox 负责模型抽取的高价值候选（需人审），避免低质自动写入污染长期记忆。
type inboxCap struct {
	sink    InboxSink
	extract Extractor
	enabled func(ctx context.Context) bool
}

// NewInbox 构造；sink 或 extract 为 nil 时能力整体降级为空。
func NewInbox(sink InboxSink, extract Extractor, enabled func(ctx context.Context) bool) Capability {
	return &inboxCap{sink: sink, extract: extract, enabled: enabled}
}

func (c *inboxCap) ID() string { return "inbox" }

func (c *inboxCap) Tools() []tool.Tool { return nil }

func (c *inboxCap) allowed(ctx context.Context) bool {
	if c == nil || c.sink == nil || c.extract == nil {
		return false
	}
	if c.enabled == nil {
		return true
	}
	return c.enabled(ctx)
}

func (c *inboxCap) Preload(_ context.Context, _ *PreloadCtx) ([]harness.ContextPiece, error) {
	return nil, nil
}

// Capture 抽取候选并入收件箱；失败仅告警（增强能力不阻断 run）。
func (c *inboxCap) Capture(ctx context.Context, p *CaptureCtx) error {
	if !c.allowed(ctx) || !p.Def.Memory.Enabled {
		return nil
	}
	// 成本闸：抽取要再花一次 LLM 调用，琐碎回合（无工具且用户话很短）不值得——「你好」「继续」这类
	// 回合几乎不含可复用知识，模型多半只会回空数组，白付一次费用与环境等待。
	if !worthExtracting(p) {
		return nil
	}
	candidates, err := c.extract(ctx, p)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	if _, err := c.sink.Submit(ctx, p.SessionID, p.RunID, candidates); err != nil {
		return err
	}
	pkg.L.Info("inbox candidates submitted", "session", p.SessionID, "run", p.RunID, "count", len(candidates))
	return nil
}

// inboxMinUserRunes 触发抽取的最小用户输入长度（rune）：更短的回合多为寒暄/确认，无沉淀价值。
const inboxMinUserRunes = 12

// worthExtracting 成本闸：用过工具（有过程可沉淀）或用户输入足够长时才值得再调一次 LLM。
func worthExtracting(p *CaptureCtx) bool {
	for i := range p.Transcript {
		if len(p.Transcript[i].ToolCalls) > 0 {
			return true
		}
	}
	return len([]rune(strings.TrimSpace(p.UserInput))) >= inboxMinUserRunes
}

package service

import (
	"context"
	"encoding/json"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/llm/modelmeta"
	"WorkBaby/internal/pkg"
)

// 本文件：token 用量与计费落库（run 汇总 / 按轮明细 / 委派与压缩分账 / 单价解析）。

func (s *ChatService) persistUsage(ctx context.Context, ses *domain.ChatSessionDO, runID, assistantMsgID string, res harness.RunResult) {
	if s.usages == nil || len(res.Turns) == 0 {
		return
	}
	pricing := s.modelPricing(ctx, ses.Model)
	nowMs := time.Now().UnixMilli()
	rows := make([]domain.TokenUsageDO, 0, len(res.Turns))
	for _, t := range res.Turns {
		rows = append(rows, domain.TokenUsageDO{
			ID:               pkg.NewID("USAGE"),
			SessionID:        ses.ID,
			RunID:            runID,
			MessageID:        assistantMsgID,
			ProviderID:       ses.ProviderID,
			Model:            ses.Model,
			Source:           domain.UsageSourceChat,
			Turn:             t.Turn,
			InputTokens:      t.Usage.InputTokens,
			OutputTokens:     t.Usage.OutputTokens,
			CacheReadTokens:  t.Usage.CacheReadTokens,
			CacheWriteTokens: t.Usage.CacheWriteTokens,
			TotalTokens:      t.Usage.TotalTokens,
			CostUSD:          pricing.CostUSD(int64(t.Usage.InputTokens), int64(t.Usage.OutputTokens), int64(t.Usage.CacheReadTokens)),
			LatencyMs:        t.LatencyMs,
			CreatedAt:        nowMs,
		})
	}
	if err := s.usages.BatchCreate(ctx, rows); err != nil {
		pkg.L.Warn("persist token usage failed", "runID", runID, "turns", len(rows), "err", err.Error())
	}
}

// persistUsageRow 落一条附属 LLM 调用的用量明细（上下文压缩摘要等）。
// turn<0 表示非主循环调用：进总消耗统计，不进按轮次的图表。
func (s *ChatService) persistUsageRow(ctx context.Context, ses *domain.ChatSessionDO, runID, messageID string, turn int, u llm.TokenUsage) {
	s.persistUsageRowFrom(ctx, ses, runID, messageID, turn, domain.UsageSourceChat, "", u)
}

// persistUsageRowFrom 带来源与 Agent 归属的明细落库；来源是仪表盘按场景拆分的唯一依据。
func (s *ChatService) persistUsageRowFrom(ctx context.Context, ses *domain.ChatSessionDO, runID, messageID string, turn int, source domain.TokenUsageSource, agent string, u llm.TokenUsage) {
	if s.usages == nil {
		return
	}
	pricing := s.modelPricing(ctx, ses.Model)
	row := domain.TokenUsageDO{
		ID:               pkg.NewID("USAGE"),
		SessionID:        ses.ID,
		RunID:            runID,
		MessageID:        messageID,
		ProviderID:       ses.ProviderID,
		Model:            ses.Model,
		Source:           source,
		Agent:            agent,
		Turn:             turn,
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
		TotalTokens:      u.TotalTokens,
		CostUSD:          pricing.CostUSD(int64(u.InputTokens), int64(u.OutputTokens), int64(u.CacheReadTokens)),
		CreatedAt:        time.Now().UnixMilli(),
	}
	if err := s.usages.BatchCreate(ctx, []domain.TokenUsageDO{row}); err != nil {
		pkg.L.Warn("persist usage row failed", "runID", runID, "err", err.Error())
	}
}

// modelPricing 读取模型单价（pricing.<model> KV）；未配置 / 解析失败返回零值（不计费）。
func (s *ChatService) modelPricing(ctx context.Context, model string) domain.ModelPricing {
	if s.setRepo == nil || model == "" {
		return domain.ModelPricing{}
	}
	row, err := s.setRepo.Get(ctx, domain.SettingKeyPricingPrefix+model)
	if err != nil || row == nil || row.V == "" {
		// 设置未配 → 内置模型目录兜底（近似公开单价；目录未收录为零值 = 不计费）
		mp := modelmeta.PricingFor(model)
		return domain.ModelPricing{InputPerM: mp.InputPerM, OutputPerM: mp.OutputPerM, CacheReadPerM: mp.CacheReadPerM}
	}
	var p domain.ModelPricing
	if json.Unmarshal([]byte(row.V), &p) != nil {
		return domain.ModelPricing{}
	}
	return p
}

// buildSystem 装配本轮 system 消息：能力注册表按序注入 + 历史归档摘要 + 压缩保留指示。
// run 与 /context 占用透视共用此入口，保证「看到的占用」与「真实请求」同口径。

package service

import (
	"context"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
)

// WithRunRecords 启用运行历史索引；nil = 不记录（旁路能力，不阻断聊天主链路）。
func (s *ChatService) WithRunRecords(r *repo.RunRecordRepo) *ChatService {
	s.runRec = r
	return s
}

// ListRunRecords 运行历史（倒序分页）；sessionID 为空表示全部会话。
func (s *ChatService) ListRunRecords(ctx context.Context, sessionID string, limit, offset int) (domain.RunRecordListRESP, error) {
	if s.runRec == nil {
		return domain.RunRecordListRESP{Items: []domain.RunRecordRESP{}}, nil
	}
	rows, total, err := s.runRec.List(ctx, sessionID, limit, offset)
	if err != nil {
		return domain.RunRecordListRESP{}, err
	}
	out := make([]domain.RunRecordRESP, 0, len(rows))
	for i := range rows {
		out = append(out, toRunRecordRESP(&rows[i]))
	}
	return domain.RunRecordListRESP{Items: out, Total: total}, nil
}

// startRunRecord 记录 run 启动；失败只记日志。
func (s *ChatService) startRunRecord(ctx context.Context, runID string, ses *domain.ChatSessionDO) {
	if s.runRec == nil || runID == "" {
		return
	}
	if err := s.runRec.Start(ctx, domain.RunRecordDO{
		RunID:     runID,
		SessionID: ses.ID,
		Model:     ses.Model,
		Status:    domain.RunStatusRunning,
		StartedAt: time.Now().UnixMilli(),
	}); err != nil {
		pkg.L.Warn("save run record failed", "runID", runID, "err", err)
	}
}

// finishRunRecord 回填终态与用量；错误终态按 status=error 落，便于历史页筛选失败运行。
func (s *ChatService) finishRunRecord(ctx context.Context, runID string, res harness.RunResult) {
	if s.runRec == nil || runID == "" {
		return
	}
	status := domain.RunStatusDone
	if res.Err != nil {
		status = domain.RunStatusError
	}
	upd := map[string]any{
		"status":        status,
		"reason":        string(res.Reason),
		"turns":         len(res.Turns),
		"input_tokens":  res.Usage.InputTokens,
		"output_tokens": res.Usage.OutputTokens,
		"cache_read":    res.Usage.CacheReadTokens,
		"total_tokens":  res.Usage.TotalTokens,
		"ended_at":      time.Now().UnixMilli(),
	}
	if err := s.runRec.Finish(ctx, runID, upd); err != nil {
		pkg.L.Warn("update run record failed", "runID", runID, "err", err)
	}
}

func toRunRecordRESP(r *domain.RunRecordDO) domain.RunRecordRESP {
	return domain.RunRecordRESP{
		RunID: r.RunID, SessionID: r.SessionID, Model: r.Model,
		Status: r.Status, Reason: r.Reason, Turns: r.Turns,
		InputTokens: r.InputTokens, OutputTokens: r.OutputTokens,
		CacheRead: r.CacheRead, TotalTokens: r.TotalTokens,
		StartedAt: r.StartedAt, EndedAt: r.EndedAt,
	}
}

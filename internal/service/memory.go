package service

import (
	"context"
	"encoding/json"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/memory"
)

// MemoryService 记忆业务编排（api 层不直接依赖能力域，经此代理）。
type MemoryService struct {
	mem *memory.Service
}

// NewMemoryService 注入 memory.Service。
func NewMemoryService(mem *memory.Service) *MemoryService { return &MemoryService{mem: mem} }

// ListEpisodes 某会话的最近情景记忆（记忆面板）。
func (s *MemoryService) ListEpisodes(ctx context.Context, sessionID string, limit int) ([]domain.MemoryEpisodeRESP, error) {
	if s.mem == nil {
		return nil, nil
	}
	rows, err := s.mem.Episodes(ctx, sessionID, limit)
	if err != nil {
		return nil, err
	}
	return toEpisodeRESP(rows), nil
}

// SearchEpisodes 关键词搜索情景记忆（记忆面板搜索框）。
func (s *MemoryService) SearchEpisodes(ctx context.Context, query string, topK int) ([]domain.MemoryEpisodeRESP, error) {
	if s.mem == nil {
		return nil, nil
	}
	rows, err := s.mem.SearchEpisodes(ctx, query, topK)
	if err != nil {
		return nil, err
	}
	return toEpisodeRESP(rows), nil
}

// WriteEpisode 手动写入一条情景记忆（记忆面板）。
func (s *MemoryService) WriteEpisode(ctx context.Context, req *domain.MemoryEpisodeREQ) (*domain.MemoryEpisodeRESP, error) {
	if s.mem == nil {
		return nil, nil
	}
	id, err := s.mem.WriteEpisode(ctx, memory.EpisodeProposal{
		SessionID:  req.SessionID,
		Summary:    req.Summary,
		Transcript: nil,
		Score:      1,
		Triggers:   req.Tags,
	})
	if err != nil {
		return nil, err
	}
	return &domain.MemoryEpisodeRESP{ID: id, SessionID: req.SessionID, Summary: req.Summary, Score: 1, CreatedAt: 0}, nil
}

func toEpisodeRESP(rows []domain.MemoryEpisodeDO) []domain.MemoryEpisodeRESP {
	out := make([]domain.MemoryEpisodeRESP, 0, len(rows))
	for i := range rows {
		out = append(out, domain.MemoryEpisodeRESP{
			ID:        rows[i].ID,
			SessionID: rows[i].SessionID,
			Summary:   rows[i].Summary,
			Score:     rows[i].Score,
			Triggers:  rows[i].Triggers,
			CreatedAt: rows[i].CreatedAt,
		})
	}
	return out
}

// LongTermText 读取某会话长期记忆全文（记忆面板）。
func (s *MemoryService) LongTermText(ctx context.Context, sessionID string) string {
	if s.mem == nil {
		return ""
	}
	return s.mem.LongTerm(ctx, sessionID)
}

// DeleteEpisode 删除单条情景记忆。
func (s *MemoryService) DeleteEpisode(ctx context.Context, id string) error {
	if s.mem == nil {
		return nil
	}
	return s.mem.DeleteEpisode(ctx, id)
}

// Stats 记忆统计（记忆面板概览）。
func (s *MemoryService) Stats(ctx context.Context) (domain.MemoryStatsRESP, error) {
	if s.mem == nil {
		return domain.MemoryStatsRESP{}, nil
	}
	return s.mem.Stats(ctx)
}

// ListFacts 语义记忆列表。
func (s *MemoryService) ListFacts(ctx context.Context, limit int) ([]domain.MemoryFactRESP, error) {
	if s.mem == nil {
		return nil, nil
	}
	rows, err := s.mem.ListFacts(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]domain.MemoryFactRESP, 0, len(rows))
	for i := range rows {
		out = append(out, domain.MemoryFactRESP{
			ID: rows[i].ID, Subject: rows[i].Subject, Key: rows[i].Key, Value: rows[i].Value,
			Confidence: rows[i].Confidence, Source: rows[i].Source,
			CreatedAt: rows[i].CreatedAt, UpdatedAt: rows[i].UpdatedAt,
		})
	}
	return out, nil
}

// WriteFact 写入/覆盖一条语义记忆（subject + key 唯一）。
func (s *MemoryService) WriteFact(ctx context.Context, subject, key, value string, confidence float64) (*domain.MemoryFactRESP, error) {
	if s.mem == nil {
		return nil, nil
	}
	if confidence <= 0 {
		confidence = 1
	}
	id, err := s.mem.WriteFact(ctx, memory.FactProposal{Subject: subject, Key: key, Value: value, Confidence: confidence})
	if err != nil {
		return nil, err
	}
	return &domain.MemoryFactRESP{ID: id, Subject: subject, Key: key, Value: value, Confidence: confidence}, nil
}

// DeleteFact 删除单条语义记忆。
func (s *MemoryService) DeleteFact(ctx context.Context, id string) error {
	if s.mem == nil {
		return nil
	}
	return s.mem.DeleteFact(ctx, id)
}

// ListProcedures 程序记忆列表。
func (s *MemoryService) ListProcedures(ctx context.Context, limit int) ([]domain.MemoryProcedureRESP, error) {
	if s.mem == nil {
		return nil, nil
	}
	rows, err := s.mem.ListProcedures(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]domain.MemoryProcedureRESP, 0, len(rows))
	for i := range rows {
		var steps []string
		_ = json.Unmarshal([]byte(rows[i].Steps), &steps)
		out = append(out, domain.MemoryProcedureRESP{
			ID: rows[i].ID, Name: rows[i].Name, Steps: steps,
			SuccessCount: rows[i].SuccessCount, FailureCount: rows[i].FailureCount,
			LastUsedAt: rows[i].LastUsedAt, CreatedAt: rows[i].CreatedAt,
		})
	}
	return out, nil
}

// WriteProcedure 写入/覆盖一个程序记忆（name 唯一）。
func (s *MemoryService) WriteProcedure(ctx context.Context, name string, steps []string) (*domain.MemoryProcedureRESP, error) {
	if s.mem == nil {
		return nil, nil
	}
	id, err := s.mem.WriteProcedure(ctx, memory.ProcedureProposal{Name: name, Steps: steps})
	if err != nil {
		return nil, err
	}
	return &domain.MemoryProcedureRESP{ID: id, Name: name, Steps: steps}, nil
}

// DeleteProcedure 删除单条程序记忆。
func (s *MemoryService) DeleteProcedure(ctx context.Context, id string) error {
	if s.mem == nil {
		return nil
	}
	return s.mem.DeleteProcedure(ctx, id)
}

// Recall 统一召回（情景 + 语义 + 程序，RRF 融合）。
func (s *MemoryService) Recall(ctx context.Context, query string, topK int) ([]domain.MemoryRecallRESP, error) {
	if s.mem == nil {
		return nil, nil
	}
	hits := s.mem.Recall(ctx, query, memory.RecallOpts{TopK: topK})
	out := make([]domain.MemoryRecallRESP, 0, len(hits))
	for i := range hits {
		out = append(out, domain.MemoryRecallRESP{
			Kind:    string(hits[i].Kind),
			Score:   hits[i].Score,
			Source:  hits[i].Source,
			Title:   hits[i].Title,
			Snippet: hits[i].Snippet,
		})
	}
	return out, nil
}

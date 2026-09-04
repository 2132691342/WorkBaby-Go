package api

import (
	"WorkBaby/internal/domain"
)

// ListMemoryEpisodes 某会话最近情景记忆。
func (h *Handler) ListMemoryEpisodes(sessionID string, limit int) ([]domain.MemoryEpisodeRESP, error) {
	return h.memProxy.ListEpisodes(h.ctx, sessionID, limit)
}

// SearchMemory 关键词搜索情景记忆（记忆面板搜索框）。
func (h *Handler) SearchMemory(query string, topK int) ([]domain.MemoryEpisodeRESP, error) {
	return h.memProxy.SearchEpisodes(h.ctx, query, topK)
}

// WriteMemoryEpisode 手动写入一条情景记忆。
func (h *Handler) WriteMemoryEpisode(req domain.MemoryEpisodeREQ) (*domain.MemoryEpisodeRESP, error) {
	return h.memProxy.WriteEpisode(h.ctx, &req)
}

// GetLongTermMemory 某会话长期记忆全文（MEMORY.md）。
func (h *Handler) GetLongTermMemory(sessionID string) (string, error) {
	return h.memProxy.LongTermText(h.ctx, sessionID), nil
}

// DeleteMemoryEpisode 删除单条情景记忆。
func (h *Handler) DeleteMemoryEpisode(id string) error {
	return h.memProxy.DeleteEpisode(h.ctx, id)
}

// GetMemoryStats 记忆面板概览统计。
func (h *Handler) GetMemoryStats() (domain.MemoryStatsRESP, error) {
	return h.memProxy.Stats(h.ctx)
}

// RecallMemory 统一召回（情景 + 语义 + 程序，RRF 融合）。
func (h *Handler) RecallMemory(query string, topK int) ([]domain.MemoryRecallRESP, error) {
	return h.memProxy.Recall(h.ctx, query, topK)
}

// ListMemoryFacts 语义记忆列表。
func (h *Handler) ListMemoryFacts(limit int) ([]domain.MemoryFactRESP, error) {
	return h.memProxy.ListFacts(h.ctx, limit)
}

// WriteMemoryFact 写入/覆盖一条语义记忆。
func (h *Handler) WriteMemoryFact(req domain.MemoryFactREQ) (*domain.MemoryFactRESP, error) {
	return h.memProxy.WriteFact(h.ctx, req.Subject, req.Key, req.Value, req.Confidence)
}

// DeleteMemoryFact 删除单条语义记忆。
func (h *Handler) DeleteMemoryFact(id string) error {
	return h.memProxy.DeleteFact(h.ctx, id)
}

// ListMemoryProcedures 程序记忆列表。
func (h *Handler) ListMemoryProcedures(limit int) ([]domain.MemoryProcedureRESP, error) {
	return h.memProxy.ListProcedures(h.ctx, limit)
}

// WriteMemoryProcedure 写入/覆盖一个程序记忆。
func (h *Handler) WriteMemoryProcedure(req domain.MemoryProcedureREQ) (*domain.MemoryProcedureRESP, error) {
	return h.memProxy.WriteProcedure(h.ctx, req.Name, req.Steps)
}

// DeleteMemoryProcedure 删除单条程序记忆。
func (h *Handler) DeleteMemoryProcedure(id string) error {
	return h.memProxy.DeleteProcedure(h.ctx, id)
}

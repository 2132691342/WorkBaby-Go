package memory

import (
	"context"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
)

// semantic 语义记忆（memory_facts 表）：subject+key 唯一，覆盖更新。
type semantic struct{ repo *repo.MemoryFactRepo }

func newSemantic(r *repo.MemoryFactRepo) *semantic { return &semantic{repo: r} }

// List 列全部事实。
func (s *semantic) List(ctx context.Context, limit int) ([]domain.MemoryFactDO, error) {
	return s.repo.List(ctx, limit)
}

// Write 写入/覆盖一条事实（同 subject+key 视为同一事实）。
// Source 记来源会话 ID（面板溯源）；提案未带时留空。
func (s *semantic) Write(ctx context.Context, p FactProposal) (string, error) {
	now := time.Now().UnixMilli()
	row := &domain.MemoryFactDO{
		ID:         pkg.NewID(domain.IDMemoryFact),
		Subject:    p.Subject,
		Key:        p.Key,
		Value:      p.Value,
		Confidence: p.Confidence,
		Source:     p.SessionID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.repo.Upsert(ctx, row); err != nil {
		return "", err
	}
	return row.ID, nil
}

// Delete 删除单条。
func (s *semantic) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// Count 总数。
func (s *semantic) Count(ctx context.Context) (int64, error) {
	return s.repo.Count(ctx)
}

// Search 召回（并入 FTS5）：
//  1. 优先 FTS5 三列 BM25 召回（memory_facts_fts，写路径同事务索引）；
//  2. FTS 零命中或不可用（trigram 需 ≥3 字符，短查询天然不命中）→ 回退确定性子串扫描。
func (s *semantic) Search(ctx context.Context, query string, topK int) []RecallHit {
	if query == "" {
		return nil
	}
	if rows, err := s.repo.SearchFTS(ctx, query, topK); err == nil && len(rows) > 0 {
		return factsToHits(rows)
	}
	// 回退：全表子串扫描（v1 语义保持；数据量大时可去掉）
	rows, err := s.repo.List(ctx, 200)
	if err != nil {
		return nil
	}
	out := make([]RecallHit, 0, len(rows))
	for i := range rows {
		if !containsFold(rows[i].Subject, query) &&
			!containsFold(rows[i].Key, query) &&
			!containsFold(rows[i].Value, query) {
			continue
		}
		out = append(out, factsToHits([]domain.MemoryFactDO{rows[i]})[0])
		if len(out) >= topK {
			break
		}
	}
	return out
}

// factsToHits DO 行 → RecallHit。
func factsToHits(rows []domain.MemoryFactDO) []RecallHit {
	out := make([]RecallHit, 0, len(rows))
	for i := range rows {
		out = append(out, RecallHit{
			Kind:      domain.MemoryKindSemantic,
			Score:     rows[i].Confidence,
			Source:    rows[i].ID,
			Title:     rows[i].Key,
			Snippet:   snippet(rows[i].Value, 200),
			CreatedAt: rows[i].CreatedAt,
		})
	}
	return out
}

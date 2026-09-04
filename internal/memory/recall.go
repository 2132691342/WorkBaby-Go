package memory

import (
	"context"
	"sort"
	"sync"

	"WorkBaby/internal/domain"
)

// recall 统一召回：RRF 融合 episodic / semantic / procedural 三源。
type recall struct {
	episodic   *episodic
	semantic   *semantic
	procedural *procedural
}

func newRecall(e *episodic, sem *semantic, proc *procedural) *recall {
	return &recall{episodic: e, semantic: sem, procedural: proc}
}

// rrfK 倒数排名融合常数。
const rrfK = 60

// Recall 按 query 召回多源命中，RRF 融合后降序返回。
func (r *recall) Recall(ctx context.Context, query string, opts RecallOpts) []RecallHit {
	if opts.TopK <= 0 {
		opts.TopK = 5
	}
	kinds := opts.Kinds
	if len(kinds) == 0 {
		kinds = []domain.MemoryKind{
			domain.MemoryKindEpisodic,
			domain.MemoryKindSemantic,
			domain.MemoryKindProcedural,
		}
	}
	var streams [][]RecallHit
	var wg sync.WaitGroup
	var mu sync.Mutex
	collect := func(hits []RecallHit) {
		mu.Lock()
		streams = append(streams, hits)
		mu.Unlock()
	}

	for _, k := range kinds {
		switch k {
		case domain.MemoryKindEpisodic:
			wg.Add(1)
			go func() {
				defer wg.Done()
				collect(r.episodic.Search(ctx, query, opts.TopK))
			}()
		case domain.MemoryKindSemantic:
			if r.semantic == nil {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				collect(r.semantic.Search(ctx, query, opts.TopK))
			}()
		case domain.MemoryKindProcedural:
			if r.procedural == nil {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				collect(r.procedural.Search(ctx, query, opts.TopK))
			}()
		}
	}
	wg.Wait()
	return rrfMerge(streams, opts.TopK)
}

// rrfMerge 倒数排名融合：score = Σ 1/(k + rank)，rank 从 1 开始。
func rrfMerge(streams [][]RecallHit, topK int) []RecallHit {
	type scored struct {
		hit   RecallHit
		score float64
	}
	m := map[string]*scored{}
	for _, stream := range streams {
		for rank, h := range stream {
			key := string(h.Kind) + "|" + h.Source
			if s, ok := m[key]; ok {
				s.score += 1.0 / (float64(rrfK) + float64(rank+1))
			} else {
				cp := h
				m[key] = &scored{hit: cp, score: 1.0 / (float64(rrfK) + float64(rank+1))}
			}
		}
	}
	out := make([]RecallHit, 0, len(m))
	for _, s := range m {
		s.hit.Score = s.score
		out = append(out, s.hit)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

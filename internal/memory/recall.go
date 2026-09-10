package memory

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

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

// recallHalfLifeDays 召回时间衰减半衰期：30 天前的记忆分值减半。
// 没有时间权重的话三年前的事实与昨天的同分，旧记忆长期霸占召回头部。
const recallHalfLifeDays = 30.0

// Recall 按 query 召回多源命中，RRF 融合 + 时间衰减 + 近重复合并后降序返回。
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
	return rrfMerge(streams, opts.TopK, time.Now().UnixMilli())
}

// rrfMerge 倒数排名融合：score = Σ 1/(k + rank)，rank 从 1 开始；
// 融合后乘时间衰减因子，再做近重复合并（归一化后互为子串的片段只留高分者）。
func rrfMerge(streams [][]RecallHit, topK int, nowMs int64) []RecallHit {
	type scored struct {
		hit   RecallHit
		score float64
	}
	m := map[string]*scored{}
	for _, stream := range streams {
		for rank, h := range stream {
			key := string(h.Kind) + "|" + h.Source
			score := 1.0 / (float64(rrfK) + float64(rank+1))
			if s, ok := m[key]; ok {
				s.score += score
			} else {
				cp := h
				m[key] = &scored{hit: cp, score: score}
			}
		}
	}
	out := make([]RecallHit, 0, len(m))
	for _, s := range m {
		s.hit.Score = decayScore(s.score, s.hit.CreatedAt, nowMs)
		out = append(out, s.hit)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	out = dedupeSnippets(out)
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

// decayScore 时间衰减：半衰期 recallHalfLifeDays，无时间戳的命中不衰减。
func decayScore(score float64, createdAtMs, nowMs int64) float64 {
	if createdAtMs <= 0 || nowMs <= createdAtMs {
		return score
	}
	ageDays := float64(nowMs-createdAtMs) / 86400000
	return score * math.Exp(-ageDays*math.Ln2/recallHalfLifeDays)
}

// dedupeSnippets 近重复合并：归一化后互为包含的片段只保留高分者。
// 不同会话沉淀了同一段结论时，重复注入只会稀释上下文而不增加信息。
func dedupeSnippets(hits []RecallHit) []RecallHit {
	out := make([]RecallHit, 0, len(hits))
	for _, h := range hits {
		norm := normalizeSnippet(h.Snippet)
		if norm == "" {
			continue
		}
		dup := false
		for _, k := range out {
			kn := normalizeSnippet(k.Snippet)
			if strings.Contains(kn, norm) || strings.Contains(norm, kn) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, h)
		}
	}
	return out
}

// normalizeSnippet 片段归一化：去空白、转小写，供包含判断。
func normalizeSnippet(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

package memory

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
)

// procedural 程序记忆（memory_procedures 表）：按 name 唯一，覆盖更新。
type procedural struct{ repo *repo.MemoryProcedureRepo }

func newProcedural(r *repo.MemoryProcedureRepo) *procedural { return &procedural{repo: r} }

// List 列全部程序。
func (p *procedural) List(ctx context.Context, limit int) ([]domain.MemoryProcedureDO, error) {
	return p.repo.List(ctx, limit)
}

// Write 写入/覆盖一个程序（同名视为同一程序）。
func (p *procedural) Write(ctx context.Context, prop ProcedureProposal) (string, error) {
	stepsJSON, err := json.Marshal(prop.Steps)
	if err != nil {
		return "", pkg.Wrap(6001, "marshal procedure steps failed", err)
	}
	now := time.Now().UnixMilli()
	row := &domain.MemoryProcedureDO{
		ID:         pkg.NewID(domain.IDMemoryProc),
		Name:       prop.Name,
		Steps:      string(stepsJSON),
		LastUsedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := p.repo.Upsert(ctx, row); err != nil {
		return "", err
	}
	return row.ID, nil
}

// Delete 删除单条。
func (p *procedural) Delete(ctx context.Context, id string) error {
	return p.repo.Delete(ctx, id)
}

// Count 总数。
func (p *procedural) Count(ctx context.Context) (int64, error) {
	return p.repo.Count(ctx)
}

// Search 召回：优先 memory_procedures_fts 的 BM25 召回，无 token / 零命中时回退子串扫描。
//
// 子串扫描是兜底而非主路：它每次都要把全表拉回内存（无索引可用），且无法排序打分。
func (p *procedural) Search(ctx context.Context, query string, topK int) []RecallHit {
	if query == "" {
		return nil
	}
	if hits, err := p.repo.SearchFTS(ctx, query, topK); err == nil && len(hits) > 0 {
		out := make([]RecallHit, 0, len(hits))
		for i := range hits {
			out = append(out, RecallHit{
				Kind:      domain.MemoryKindProcedural,
				Score:     1,
				Source:    hits[i].ID,
				Title:     hits[i].Name,
				Snippet:   snippet(parseSteps(hits[i].Steps), 200),
				CreatedAt: hits[i].CreatedAt,
			})
		}
		return out
	}
	rows, err := p.repo.List(ctx, 200)
	if err != nil {
		return nil
	}
	toks := pkg.SplitTokens(query)
	out := make([]RecallHit, 0, len(rows))
	for i := range rows {
		name, steps := strings.ToLower(rows[i].Name), strings.ToLower(parseSteps(rows[i].Steps))
		hit := false
		for _, t := range toks {
			t = strings.ToLower(t)
			if t == "" {
				continue
			}
			if strings.Contains(name, t) || strings.Contains(steps, t) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		out = append(out, RecallHit{
			Kind:      domain.MemoryKindProcedural,
			Score:     1,
			Source:    rows[i].ID,
			Title:     rows[i].Name,
			Snippet:   snippet(parseSteps(rows[i].Steps), 200),
			CreatedAt: rows[i].CreatedAt,
		})
		if len(out) >= topK {
			break
		}
	}
	return out
}

func parseSteps(raw string) string {
	var steps []string
	if err := json.Unmarshal([]byte(raw), &steps); err != nil {
		return raw
	}
	var sb strings.Builder
	for i, s := range steps {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(s)
	}
	return sb.String()
}

// containsFold 大小写不敏感的子串匹配（中文无大小写差异，直接包含判断）。
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

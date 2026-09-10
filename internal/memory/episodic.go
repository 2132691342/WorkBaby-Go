package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
)

// episodic 情景记忆：memory_episodes 表（真相源）+ 文件快照（仅展示）。
type episodic struct {
	repo *repo.MemoryEpisodeRepo
	home string
}

func newEpisodic(r *repo.MemoryEpisodeRepo, home string) *episodic {
	return &episodic{repo: r, home: home}
}

// Write 落库 episode + 写文件快照（快照失败仅忽略，DB 是真相源）。
// 写入前做近重复合并：同一会话反复沉淀同一结论时只保留首条。
func (e *episodic) Write(ctx context.Context, p EpisodeProposal) (string, error) {
	if dupID, err := e.nearDuplicate(ctx, p.SessionID, p.Summary); err == nil && dupID != "" {
		return dupID, nil
	}
	id := pkg.NewID(domain.IDMemoryEpisode)
	transcriptJSON, err := json.Marshal(p.Transcript)
	if err != nil {
		return "", pkg.Wrap(6001, "marshal episode transcript failed", err)
	}
	triggersJSON, _ := json.Marshal(p.Triggers)
	filePath := e.filePath(id)
	row := &domain.MemoryEpisodeDO{
		ID:         id,
		SessionID:  p.SessionID,
		Summary:    p.Summary,
		Transcript: string(transcriptJSON),
		FilePath:   filePath,
		Score:      p.Score,
		Triggers:   string(triggersJSON),
	}
	if err := e.repo.Insert(ctx, row); err != nil {
		return "", err
	}
	// 文件快照（失败仅记日志）
	snapshot := fmt.Sprintf("# Summary\n\n%s\n\n# Transcript\n\n%s\n", p.Summary, formatTranscript(p.Transcript))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err == nil {
		_ = os.WriteFile(filePath, []byte(snapshot), 0o644)
	}
	return id, nil
}

// nearDuplicate 在同一会话的最近 episode 里找近重复摘要；命中返回已有 id。
//
// 写入侧去重（召回侧已有 dedupeSnippets，但那只在注入时生效）：
// 同一结论换几种措辞反复落库，会同时污染 MEMORY.md、FTS 命中与召回排序。
func (e *episodic) nearDuplicate(ctx context.Context, sessionID, summary string) (string, error) {
	if sessionID == "" || strings.TrimSpace(summary) == "" {
		return "", nil
	}
	if len([]rune(normalizeSnippet(summary))) < nearDupMinRunes {
		// 过短摘要的 Dice 系数噪声太大（几个字相同就超过阈值），宁可多存也不误并
		return "", nil
	}
	rows, err := e.repo.ListBySession(ctx, sessionID, nearDupScan)
	if err != nil {
		return "", err
	}
	for i := range rows {
		if similarity(rows[i].Summary, summary) >= nearDupThreshold {
			return rows[i].ID, nil
		}
	}
	return "", nil
}

// similarity 字符二元组 Dice 系数（0~1）：对中文无需分词，对改写/增删词较稳健。
// 计数一律按 rune——按字节算会让中文相似度被低估 3 倍，去重直接失效。
func similarity(a, b string) float64 {
	na, nb := []rune(normalizeSnippet(a)), []rune(normalizeSnippet(b))
	if len(na) == 0 || len(nb) == 0 {
		return 0
	}
	if string(na) == string(nb) {
		return 1
	}
	if len(na) < 2 || len(nb) < 2 {
		return 0
	}
	ba, bb := bigrams(na), bigrams(nb)
	inter := 0
	for g, ca := range ba {
		if cb, ok := bb[g]; ok {
			if ca < cb {
				inter += ca
			} else {
				inter += cb
			}
		}
	}
	return 2 * float64(inter) / float64(len(na)-1+len(nb)-1)
}

// bigrams 字符二元组计数（已按 rune 切分）。
func bigrams(r []rune) map[string]int {
	out := make(map[string]int, len(r))
	for i := 0; i+1 < len(r); i++ {
		out[string(r[i:i+2])]++
	}
	return out
}

const (
	nearDupScan      = 20   // 只与最近 N 条比较：更早的记忆语义早已不同
	nearDupThreshold = 0.7  // 相似度阈值：低于它更像「新结论」而非重复
	nearDupMinRunes  = 10   // 参与去重的最短摘要长度（rune）
)

// Search 检索摘要。
func (e *episodic) Search(ctx context.Context, query string, topK int) []RecallHit {
	rows, err := e.repo.SearchFTS(ctx, query, topK)
	if err != nil || len(rows) == 0 {
		return nil
	}
	out := make([]RecallHit, 0, len(rows))
	for i := range rows {
		out = append(out, RecallHit{
			Kind:      domain.MemoryKindEpisodic,
			Score:     rows[i].Score,
			Source:    rows[i].ID,
			Title:     snippet(rows[i].Summary, 40),
			Snippet:   snippet(rows[i].Summary, 200),
			CreatedAt: rows[i].CreatedAt,
		})
	}
	return out
}

func (e *episodic) filePath(id string) string {
	return filepath.Join(e.home, "memory", "episodes", id, "summary.md")
}

// formatTranscript 生成人读快照（跳过 thinking/tool 细节）。
func formatTranscript(msgs []llm.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		prefix := ""
		switch m.Role {
		case llm.RoleUser:
			prefix = "User: "
		case llm.RoleAssistant:
			prefix = "Assistant: "
		default:
			continue
		}
		if m.Content == "" {
			continue
		}
		sb.WriteString(prefix)
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

func snippet(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

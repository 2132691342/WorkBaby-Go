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
func (e *episodic) Write(ctx context.Context, p EpisodeProposal) (string, error) {
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

// Search 检索摘要。
func (e *episodic) Search(ctx context.Context, query string, topK int) []RecallHit {
	rows, err := e.repo.SearchFTS(ctx, query, topK)
	if err != nil || len(rows) == 0 {
		return nil
	}
	out := make([]RecallHit, 0, len(rows))
	for i := range rows {
		out = append(out, RecallHit{
			Kind:    domain.MemoryKindEpisodic,
			Score:   rows[i].Score,
			Source:  rows[i].ID,
			Title:   snippet(rows[i].Summary, 40),
			Snippet: snippet(rows[i].Summary, 200),
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

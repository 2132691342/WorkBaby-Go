package rag

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"WorkBaby/internal/pkg"
	"gorm.io/gorm"
)

// Hit 检索命中项。
type Hit struct {
	DocID   string            `json:"docID"`
	DocName string            `json:"docName"`
	ChunkID string            `json:"chunkID"`
	Content string            `json:"content"`
	Score   float64           `json:"score"`
	Source  string            `json:"source"`
	Meta    map[string]string `json:"meta"`
}

// Retriever 统一检索接口；上层不感知底层实现。
type Retriever interface {
	Search(ctx context.Context, query string, topK int) ([]Hit, error)
}

// FTS5Retriever 检索实现：trigram tokenizer + BM25 排序；token 全部短于 3 字时 LIKE 兜底。
type FTS5Retriever struct {
	db *gorm.DB
}

// NewFTS5Retriever 构造检索器。
func NewFTS5Retriever(db *gorm.DB) *FTS5Retriever { return &FTS5Retriever{db: db} }

// minGramLen trigram tokenizer 的最小可查长度；短于它的 token 匹配不到任何 3-gram。
const minGramLen = pkg.MinGramLen

// BuildMatchQuery 自由文本 → FTS5 MATCH 表达式。
// 实现收口在 pkg：知识库与记忆共用同一套切词 / OR 口径，避免两处漂移。
func BuildMatchQuery(query string) string { return pkg.BuildMatchQuery(query) }

// Search BM25 检索；Score 取负翻转为「越大越相关」（bm25 值越小越相关）。
func (r *FTS5Retriever) Search(ctx context.Context, query string, topK int) ([]Hit, error) {
	if topK <= 0 {
		topK = 5
	}
	if topK > 50 {
		topK = 50
	}
	match := BuildMatchQuery(query)
	if match == "" {
		// 所有 token 都短于 trigram 最小窗口：走 LIKE 兜底而不是静默无结果
		return r.searchLike(ctx, query, topK)
	}

	var rows []struct {
		ChunkID  string  `gorm:"column:chunk_id"`
		DocID    string  `gorm:"column:doc_id"`
		DocName  string  `gorm:"column:doc_name"`
		Source   string  `gorm:"column:source"`
		Content  string  `gorm:"column:content"`
		MetaJSON string  `gorm:"column:meta_json"`
		Score    float64 `gorm:"column:score"`
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT kc.id AS chunk_id, kc.doc_id AS doc_id, kd.name AS doc_name,
		       kd.source AS source, kc.content AS content, kc.meta_json AS meta_json,
		       bm25(knowledge_chunks_fts) AS score
		FROM knowledge_chunks_fts
		JOIN knowledge_chunks kc ON kc.id = knowledge_chunks_fts.id
		JOIN knowledge_docs kd ON kd.id = kc.doc_id
		WHERE knowledge_chunks_fts MATCH ? AND kd.deleted_at IS NULL
		ORDER BY score
		LIMIT ?`, match, topK).Scan(&rows).Error
	if err != nil {
		return nil, pkg.Wrap(7004, "fts5 search failed", err)
	}

	out := make([]Hit, 0, len(rows))
	for i := range rows {
		out = append(out, Hit{
			DocID:   rows[i].DocID,
			DocName: rows[i].DocName,
			ChunkID: rows[i].ChunkID,
			Content: rows[i].Content,
			Score:   -rows[i].Score,
			Source:  rows[i].Source,
			Meta:    parseMeta(rows[i].MetaJSON),
		})
	}
	return out, nil
}

// searchLike FTS5 兜底：trigram 最小窗口是 3 字符，「部署」「报错」「下载」这类
// 2 字中文高频查询在 MATCH 上永远零命中且静默无结果——这是中文场景最常见的查询长度。
//
// 退化为 LIKE 子串扫描，本地单库规模下代价可接受。命中条件：整串 LIKE OR 任一 token
// LIKE——多 token 短查询（"怎么 部署 服务"）不会因要求整串连续而漏命中。
func (r *FTS5Retriever) searchLike(ctx context.Context, query string, topK int) ([]Hit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	toks := pkg.SplitTokens(q)
	seen := make(map[string]bool, len(toks))
	clauses := make([]string, 0, len(toks)+1)
	args := make([]any, 0, len(toks)+2)
	clauses = append(clauses, "kc.content LIKE ? ESCAPE '\\'")
	args = append(args, "%"+pkg.EscapeLike(q)+"%")
	for _, t := range toks {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		clauses = append(clauses, "kc.content LIKE ? ESCAPE '\\'")
		args = append(args, "%"+pkg.EscapeLike(t)+"%")
	}
	args = append(args, topK)

	sql := `
		SELECT kc.id AS chunk_id, kc.doc_id AS doc_id, kd.name AS doc_name,
		       kd.source AS source, kc.content AS content, kc.meta_json AS meta_json
		FROM knowledge_chunks kc
		JOIN knowledge_docs kd ON kd.id = kc.doc_id
		WHERE kd.deleted_at IS NULL AND (` + strings.Join(clauses, " OR ") + `)
		ORDER BY kc.created_at DESC
		LIMIT ?`
	var rows []struct {
		ChunkID  string `gorm:"column:chunk_id"`
		DocID    string `gorm:"column:doc_id"`
		DocName  string `gorm:"column:doc_name"`
		Source   string `gorm:"column:source"`
		Content  string `gorm:"column:content"`
		MetaJSON string `gorm:"column:meta_json"`
	}
	err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	if err != nil {
		return nil, pkg.Wrap(7004, "like search failed", err)
	}
	out := make([]Hit, 0, len(rows))
	for i := range rows {
		out = append(out, Hit{
			DocID:   rows[i].DocID,
			DocName: rows[i].DocName,
			ChunkID: rows[i].ChunkID,
			Content: rows[i].Content,
			Score:   0,
			Source:  rows[i].Source,
			Meta:    parseMeta(rows[i].MetaJSON),
		})
	}
	return out, nil
}

func parseMeta(metaJSON string) map[string]string {
	if metaJSON == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(metaJSON), &m); err != nil {
		return nil
	}
	return m
}

// FormatHits 把命中项格式化为便于模型引用出处的文本。
func FormatHits(hits []Hit) string {
	if len(hits) == 0 {
		return "（知识库中没有找到相关内容）"
	}
	var sb strings.Builder
	for i, h := range hits {
		sb.WriteString("[")
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString("] ")
		sb.WriteString(h.DocName)
		if h.Meta != nil && h.Meta["title"] != "" {
			sb.WriteString(" · ")
			sb.WriteString(h.Meta["title"])
		}
		sb.WriteString("\n")
		sb.WriteString(h.Content)
		sb.WriteString("\n\n")
	}
	return strings.TrimSpace(sb.String())
}

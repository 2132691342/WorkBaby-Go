package rag

import (
	"context"
	"encoding/json"
	"regexp"
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

// Retriever 统一检索接口；v2 增加 Embedding / Hybrid 实现时上层无感切换。
type Retriever interface {
	Search(ctx context.Context, query string, topK int) ([]Hit, error)
}

// FTS5Retriever v1 检索实现：trigram tokenizer + BM25 排序。
type FTS5Retriever struct {
	db *gorm.DB
}

// NewFTS5Retriever 构造检索器。
func NewFTS5Retriever(db *gorm.DB) *FTS5Retriever { return &FTS5Retriever{db: db} }

// minGramLen trigram tokenizer 的最小可查长度；短于它的 token 匹配不到任何 3-gram。
const minGramLen = 3

// nonWordRe 用非字母数字作分隔切词；保证 token 内不含引号等 FTS5 语法字符（天然防注入）。
var nonWordRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// BuildMatchQuery 把自由文本转成 FTS5 MATCH 表达式：
// 按非字母数字切词 → 丢弃不足 3 字（trigram 最小窗口）→ 双引号包裹 → OR 连接。
// 短查询（如 "Go"）会得到空串，调用方应视为「无结果」而非报错。
func BuildMatchQuery(query string) string {
	var parts []string
	seen := make(map[string]bool)
	for _, tok := range nonWordRe.Split(strings.TrimSpace(query), -1) {
		if runeLen(tok) < minGramLen || seen[tok] {
			continue
		}
		seen[tok] = true
		parts = append(parts, `"`+tok+`"`)
	}
	return strings.Join(parts, " OR ")
}

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
		return nil, nil
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

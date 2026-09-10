// Package memory 是 WorkBaby 三层记忆：短期（滑动窗口）/ 长期（MEMORY.md）/ 情景（episodes + FTS5）。
//
// 边界：memory/ 不依赖 harness / agent / api / service / wails；
// 通过构造注入 repo 与数据根目录；形成策略为确定性打分，不调 LLM。
package memory

import (
	"context"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/llm"
)

// RecallOpts 统一召回参数。
type RecallOpts struct {
	Kinds []domain.MemoryKind // 默认全选
	TopK  int                 // 默认 5
}

// RecallHit 召回命中项。
type RecallHit struct {
	Kind      domain.MemoryKind
	Score     float64
	Source    string // episode id
	Title     string
	Snippet   string
	CreatedAt int64 // 毫秒时间戳；时间衰减据此加权
}

// EpisodeProposal 情景记忆提案（形成策略输出）。
type EpisodeProposal struct {
	SessionID  string
	Summary    string
	Transcript []llm.Message
	Score      float64
	Triggers   []string
}

// FactProposal 语义记忆提案（v2 预留）。
type FactProposal struct {
	Subject    string
	Key        string
	Value      string
	Confidence float64
	SessionID  string // 来源会话（落 MemoryFactDO.Source，面板溯源用）
}

// ProcedureProposal 程序记忆提案（v2 预留）。
type ProcedureProposal struct {
	Name  string
	Steps []string
}

// FormationResult 形成策略评估结果。
type FormationResult struct {
	Episode    *EpisodeProposal
	Semantic   []FactProposal      // v2
	Procedural []ProcedureProposal // v2
}

// Memory 聚合接口；service 层经此读写记忆。
//
// 短期窗口不在此列：会话历史的上下文重建由 chat.toLLMMessages 唯一负责
// （含工具配对、孤儿剥离、占位清理），记忆层再提供一份滑窗属于重复实现。
type Memory interface {
	LongTerm(ctx context.Context, sessionID string) string
	AppendLongTerm(ctx context.Context, sessionID string, delta string) error

	Recall(ctx context.Context, query string, opts RecallOpts) []RecallHit
	Evaluate(ctx context.Context, sessionID string, transcript []llm.Message) FormationResult
	WriteEpisode(ctx context.Context, p EpisodeProposal) (string, error)
}

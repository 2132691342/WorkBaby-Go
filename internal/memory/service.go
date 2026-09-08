package memory

import (
	"context"
	"log/slog"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/repo"
)

// Service 三层记忆聚合实现。
type Service struct {
	long       *longTerm
	episodic   *episodic
	semantic   *semantic
	procedural *procedural
	formation  *FormationPolicy
	recall     *recall
	log        *slog.Logger
}

// NewService 构造记忆服务；home 为数据根目录（paths.Home）。
func NewService(epRepo *repo.MemoryEpisodeRepo,
	factRepo *repo.MemoryFactRepo, procRepo *repo.MemoryProcedureRepo, home string) *Service {
	e := newEpisodic(epRepo, home)
	sem := newSemantic(factRepo)
	proc := newProcedural(procRepo)
	return &Service{
		long:       newLongTerm(home),
		episodic:   e,
		semantic:   sem,
		procedural: proc,
		formation:  NewFormationPolicy(DefaultThresholds()),
		recall:     newRecall(e, sem, proc),
		log:        slog.Default(),
	}
}

// WithMemoryPath 注入长期记忆文件落点解析器：返回某会话 MEMORY.md 的绝对路径。
// 不注入时使用默认根 {home}/memory/{sessionID}/MEMORY.md。
func (s *Service) WithMemoryPath(resolve func(sessionID string) string) *Service {
	if s.long != nil {
		s.long.resolve = resolve
	}
	return s
}

// LongTerm 读取 MEMORY.md 全文（空串 = 无记忆）。
func (s *Service) LongTerm(ctx context.Context, sessionID string) string {
	return s.long.Load(ctx, sessionID)
}

// AppendLongTerm 追加长期记忆。
func (s *Service) AppendLongTerm(ctx context.Context, sessionID string, delta string) error {
	return s.long.Append(ctx, sessionID, delta)
}

// Recall 统一召回。
func (s *Service) Recall(ctx context.Context, query string, opts RecallOpts) []RecallHit {
	return s.recall.Recall(ctx, query, opts)
}

// Evaluate 形成策略评估（确定性打分，不调 LLM）。
func (s *Service) Evaluate(ctx context.Context, sessionID string, transcript []llm.Message) FormationResult {
	return s.formation.Evaluate(ctx, sessionID, transcript)
}

// WriteEpisode 落库情景记忆。
func (s *Service) WriteEpisode(ctx context.Context, p EpisodeProposal) (string, error) {
	return s.episodic.Write(ctx, p)
}

// Episodes 某会话最近情景记忆（记忆面板展示）。
func (s *Service) Episodes(ctx context.Context, sessionID string, limit int) ([]domain.MemoryEpisodeDO, error) {
	return s.episodic.repo.ListBySession(ctx, sessionID, limit)
}

// SearchEpisodes 关键词搜索情景记忆（记忆面板搜索框）。
func (s *Service) SearchEpisodes(ctx context.Context, query string, topK int) ([]domain.MemoryEpisodeDO, error) {
	return s.episodic.repo.SearchFTS(ctx, query, topK)
}

// LongTermPath 暴露某会话 MEMORY.md 路径（调试/面板用）。
func (s *Service) LongTermPath(sessionID string) string {
	return s.long.Path(sessionID)
}

// DeleteEpisode 删除单条情景记忆。
func (s *Service) DeleteEpisode(ctx context.Context, id string) error {
	return s.episodic.repo.Delete(ctx, id)
}

// Stats 三层记忆条目数（记忆面板概览）。
func (s *Service) Stats(ctx context.Context) (domain.MemoryStatsRESP, error) {
	ep, err := s.episodic.repo.Count(ctx)
	if err != nil {
		return domain.MemoryStatsRESP{}, err
	}
	fact, err := s.semantic.Count(ctx)
	if err != nil {
		return domain.MemoryStatsRESP{}, err
	}
	proc, err := s.procedural.Count(ctx)
	if err != nil {
		return domain.MemoryStatsRESP{}, err
	}
	return domain.MemoryStatsRESP{Episodes: ep, Facts: fact, Procedures: proc}, nil
}

// ListFacts 语义记忆列表。
func (s *Service) ListFacts(ctx context.Context, limit int) ([]domain.MemoryFactDO, error) {
	return s.semantic.List(ctx, limit)
}

// WriteFact 写入/覆盖一条语义记忆。
func (s *Service) WriteFact(ctx context.Context, p FactProposal) (string, error) {
	return s.semantic.Write(ctx, p)
}

// DeleteFact 删除单条语义记忆。
func (s *Service) DeleteFact(ctx context.Context, id string) error {
	return s.semantic.Delete(ctx, id)
}

// ListProcedures 程序记忆列表。
func (s *Service) ListProcedures(ctx context.Context, limit int) ([]domain.MemoryProcedureDO, error) {
	return s.procedural.List(ctx, limit)
}

// WriteProcedure 写入/覆盖一个程序记忆。
func (s *Service) WriteProcedure(ctx context.Context, p ProcedureProposal) (string, error) {
	return s.procedural.Write(ctx, p)
}

// DeleteProcedure 删除单条程序记忆。
func (s *Service) DeleteProcedure(ctx context.Context, id string) error {
	return s.procedural.Delete(ctx, id)
}

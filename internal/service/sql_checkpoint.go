package service

import (
	"context"
	"encoding/json"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
)

// sqlCheckpointStore 把 harness.Checkpoint 持久化到 agent_checkpoints 表。
//
// 相比 JSONL 文件：跨进程重启可恢复、会话删除可级联清理、可为「可恢复运行」列表查询。
// 增量模型（v2）：Messages 仅存本轮新增切片（自 LastSeq 起），Resume 拼接 chat_messages 历史 +
// 增量 → 完整上下文；旧数据 last_seq=0 由 adapter 视为「全量落库」兼容。
//
// repo 层只做无业务语义读写，序列化/反序列化在此 adapter 收敛（service 允许依赖 repo/harness）。
type sqlCheckpointStore struct {
	repo     *repo.AgentCheckpointRepo
	messages *repo.MessageRepo // Resume 时拼接 chat_messages 历史；nil = 退化为纯增量（不推荐）
}

// NewSQLCheckpointStore 构造 SQL 检查点存储（实现 harness.CheckpointStore）。
func NewSQLCheckpointStore(r *repo.AgentCheckpointRepo) harness.CheckpointStore {
	return &sqlCheckpointStore{repo: r}
}

// WithMessageRepo 注入 message 仓库以支持 Resume 增量拼接；可选。
func (s *sqlCheckpointStore) WithMessageRepo(m *repo.MessageRepo) *sqlCheckpointStore {
	s.messages = m
	return s
}

// Append 每轮工具执行后保存一行（(run_id, turn) upsert）。
// 增量：cp.LastSeq 是上轮检查点的首条 seq（本轮新增起点），Messages 整体透传（runner 已传完整切片）。
func (s *sqlCheckpointStore) Append(cp *harness.Checkpoint) error {
	messages, err := json.Marshal(cp.Messages)
	if err != nil {
		return pkg.Wrap(5005, "checkpoint messages marshal failed", err)
	}
	state, err := json.Marshal(cp.State)
	if err != nil {
		return pkg.Wrap(5005, "checkpoint state marshal failed", err)
	}
	usage, err := json.Marshal(cp.Usage)
	if err != nil {
		return pkg.Wrap(5005, "checkpoint usage marshal failed", err)
	}
	stepsJSON := ""
	if len(cp.StepRecords) > 0 {
		bs, serr := json.Marshal(cp.StepRecords)
		if serr != nil {
			return pkg.Wrap(5005, "checkpoint steps marshal failed", serr)
		}
		stepsJSON = string(bs)
	}
	row := &domain.AgentCheckpointDO{
		ID:                 pkg.NewID("CP"),
		SessionID:          cp.SessionID,
		RunID:              cp.RunID,
		Turn:               cp.Turn,
		AssistantMessageID: cp.AssistantMsgID,
		LastSeq:            cp.LastSeq,
		MessagesJSON:       string(messages),
		StateJSON:          string(state),
		UsageJSON:          string(usage),
		StepsJSON:          stepsJSON,
		Content:            cp.Content,
		Thinking:           cp.Thinking,
	}
	return s.repo.Save(context.Background(), row)
}

// LoadLast 取最新一轮检查点；Resume 时按 LastSeq 拼接 chat_messages 历史。
// 兼容旧数据（LastSeq=0）：messages 已是完整切片，直接返回。
func (s *sqlCheckpointStore) LoadLast(_ string, runID string) (*harness.Checkpoint, error) {
	row, err := s.repo.LoadLast(context.Background(), runID)
	if err != nil {
		return nil, err
	}
	cp := &harness.Checkpoint{
		RunID:          row.RunID,
		SessionID:      row.SessionID,
		Turn:           row.Turn,
		AssistantMsgID: row.AssistantMessageID,
		Content:        row.Content,
		Thinking:       row.Thinking,
		CreatedAt:      row.CreatedAt,
		LastSeq:        row.LastSeq,
	}
	if err := json.Unmarshal([]byte(row.MessagesJSON), &cp.Messages); err != nil {
		return nil, pkg.Wrap(5005, "checkpoint messages parse failed", err)
	}
	if err := json.Unmarshal([]byte(row.StateJSON), &cp.State); err != nil {
		return nil, pkg.Wrap(5005, "checkpoint state parse failed", err)
	}
	if err := json.Unmarshal([]byte(row.UsageJSON), &cp.Usage); err != nil {
		return nil, pkg.Wrap(5005, "checkpoint usage parse failed", err)
	}
	if row.StepsJSON != "" {
		if err := json.Unmarshal([]byte(row.StepsJSON), &cp.StepRecords); err != nil {
			return nil, pkg.Wrap(5005, "checkpoint steps parse failed", err)
		}
	}
	// Resume 拼接：增量起点（LastSeq>0）+ 增量 = 完整上下文。
	// 旧数据 LastSeq=0 时 messages 已是全量，跳过拼接避免重复。
	if row.LastSeq > 0 && s.messages != nil && len(cp.Messages) > 0 {
		if err := s.hydrateHistory(cp, row.SessionID, row.LastSeq); err != nil {
			return nil, pkg.Wrap(5005, "hydrate checkpoint history failed", err)
		}
	}
	return cp, nil
}

// hydrateHistory 按 LastSeq 把 chat_messages 的历史消息拼到增量前面。
// 增量中第一条消息的 seq 应该 == LastSeq+1，拼接时跳过增量本身（按 seq 去重）。
// 复用 chat.ToLLMMessages 保证 Resume 出的上下文与正常 run 完全同口径。
func (s *sqlCheckpointStore) hydrateHistory(cp *harness.Checkpoint, sessionID string, lastSeq int64) error {
	hist, err := s.messages.ListBySession(context.Background(), sessionID, 0, 0)
	if err != nil {
		return err
	}
	// 过滤：增量起点前的消息（seq<lastSeq）才是要补齐的历史
	var prefix []domain.MessageDO
	for _, m := range hist {
		if m.Seq < lastSeq {
			prefix = append(prefix, m)
		}
	}
	llmPrefix, err := ToLLMMessages(prefix)
	if err != nil {
		return err
	}
	cp.Messages = append(llmPrefix, cp.Messages...)
	return nil
}

// Cleanup 保留最近 keep 个 run 的检查点。
func (s *sqlCheckpointStore) Cleanup(sessionID string, keep int) error {
	return s.repo.CleanupRuns(context.Background(), sessionID, keep)
}

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
// repo 层只做无业务语义读写，序列化/反序列化在此 adapter 收敛（service 允许依赖 repo/harness）。
type sqlCheckpointStore struct {
	repo *repo.AgentCheckpointRepo
}

// NewSQLCheckpointStore 构造 SQL 检查点存储（实现 harness.CheckpointStore）。
func NewSQLCheckpointStore(r *repo.AgentCheckpointRepo) harness.CheckpointStore {
	return &sqlCheckpointStore{repo: r}
}

// Append 每轮工具执行后保存一行（(run_id, turn) upsert）。
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
		MessagesJSON:       string(messages),
		StateJSON:          string(state),
		UsageJSON:          string(usage),
		StepsJSON:          stepsJSON,
		Content:            cp.Content,
		Thinking:           cp.Thinking,
	}
	return s.repo.Save(context.Background(), row)
}

// LoadLast 取最新一轮检查点。
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
	return cp, nil
}

// Cleanup 保留最近 keep 个 run 的检查点。
func (s *sqlCheckpointStore) Cleanup(sessionID string, keep int) error {
	return s.repo.CleanupRuns(context.Background(), sessionID, keep)
}

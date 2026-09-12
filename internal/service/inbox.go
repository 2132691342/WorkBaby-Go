package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"WorkBaby/internal/domain"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
)

// InboxApplier 把已批准的候选合入正式库（按 kind 注册；service 不感知具体能力域）。
type InboxApplier func(ctx context.Context, item domain.InboxItemDO) error

// InboxService 回写收件箱编排：候选去重落库 + 人审合入。
type InboxService struct {
	repo     *repo.InboxRepo
	appliers map[domain.InboxKind]InboxApplier
}

// NewInboxService 构造；repo 为 nil 时全部退化为空操作。
func NewInboxService(r *repo.InboxRepo) *InboxService {
	return &InboxService{repo: r, appliers: map[domain.InboxKind]InboxApplier{}}
}

// WithApplier 注册某类候选的合入器；链式装配。
func (s *InboxService) WithApplier(kind domain.InboxKind, fn InboxApplier) *InboxService {
	s.appliers[kind] = fn
	return s
}

// Submit 落库候选并返回新增条数（kind+title 已存在则跳过，避免重复提案）。
func (s *InboxService) Submit(ctx context.Context, sessionID, runID string, candidates []domain.InboxCandidate) (int, error) {
	if s == nil || s.repo == nil || len(candidates) == 0 {
		return 0, nil
	}
	now := time.Now().UnixMilli()
	added := 0
	for i := range candidates {
		c := candidates[i]
		if c.Kind == "" || strings.TrimSpace(c.Title) == "" {
			continue
		}
		if exist, err := s.repo.FindByKindTitle(ctx, c.Kind, c.Title); err != nil {
			return added, err
		} else if exist != nil {
			continue
		}
		payload, err := json.Marshal(c.Payload)
		if err != nil {
			pkg.L.Warn("marshal inbox payload failed", "kind", c.Kind, "title", c.Title, "err", err.Error())
			continue
		}
		row := &domain.InboxItemDO{
			ID:         pkg.NewID(domain.IDInbox),
			Kind:       c.Kind,
			Title:      pkg.TruncateRunes(c.Title, 200),
			Summary:    pkg.TruncateRunes(inboxOrDefault(c.Summary, summarizePayload(c.Kind, payload)), 400),
			Payload:    string(payload),
			Status:     domain.InboxStatusPending,
			Source:     orSource(c.Source),
			Confidence: c.Confidence,
			SessionID:  sessionID,
			RunID:      runID,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := s.repo.Create(ctx, row); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// List 按状态列出条目（status 空 = 全部）。
func (s *InboxService) List(ctx context.Context, status string, limit int) ([]domain.InboxItemRESP, error) {
	if s == nil || s.repo == nil {
		return []domain.InboxItemRESP{}, nil
	}
	rows, err := s.repo.ListByStatus(ctx, domain.InboxStatus(status), limit)
	if err != nil {
		return nil, err
	}
	out := make([]domain.InboxItemRESP, 0, len(rows))
	for i := range rows {
		out = append(out, toInboxRESP(&rows[i]))
	}
	return out, nil
}

// Stats 概览统计。
func (s *InboxService) Stats(ctx context.Context) (domain.InboxStatsRESP, error) {
	if s == nil || s.repo == nil {
		return domain.InboxStatsRESP{}, nil
	}
	p, err := s.repo.CountByStatus(ctx, domain.InboxStatusPending)
	if err != nil {
		return domain.InboxStatsRESP{}, err
	}
	a, err := s.repo.CountByStatus(ctx, domain.InboxStatusApproved)
	if err != nil {
		return domain.InboxStatsRESP{}, err
	}
	r, err := s.repo.CountByStatus(ctx, domain.InboxStatusRejected)
	if err != nil {
		return domain.InboxStatsRESP{}, err
	}
	return domain.InboxStatsRESP{Pending: p, Approved: a, Rejected: r}, nil
}

// Approve 合入一条候选（按 kind 分发），随后标记 approved。
func (s *InboxService) Approve(ctx context.Context, id string) (*domain.InboxItemRESP, error) {
	if s == nil || s.repo == nil {
		return nil, domain.ErrInboxNotFound
	}
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.Status != domain.InboxStatusPending {
		return nil, domain.ErrInboxNotPending
	}
	fn, ok := s.appliers[row.Kind]
	if !ok {
		return nil, pkg.New(domain.ErrInboxApplyFailed.Code, "该类型暂不支持合入", string(row.Kind))
	}
	if err := fn(ctx, *row); err != nil {
		return nil, pkg.Wrap(domain.ErrInboxApplyFailed.Code, "合入失败："+err.Error(), err)
	}
	if err := s.repo.SetStatus(ctx, id, domain.InboxStatusApproved); err != nil {
		return nil, err
	}
	row.Status = domain.InboxStatusApproved
	resp := toInboxRESP(row)
	return &resp, nil
}

// Reject 拒绝一条候选。
func (s *InboxService) Reject(ctx context.Context, id string) error {
	if s == nil || s.repo == nil {
		return domain.ErrInboxNotFound
	}
	return s.repo.SetStatus(ctx, id, domain.InboxStatusRejected)
}

func toInboxRESP(row *domain.InboxItemDO) domain.InboxItemRESP {
	payload := json.RawMessage(row.Payload)
	if len(payload) == 0 {
		payload = json.RawMessage("null")
	}
	return domain.InboxItemRESP{
		ID: row.ID, Kind: row.Kind, Title: row.Title, Summary: row.Summary,
		Payload: payload, Status: row.Status, Source: row.Source, Confidence: row.Confidence,
		SessionID: row.SessionID, RunID: row.RunID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// summarizePayload 未提供摘要时按 kind 从载荷派生一行人类可读文本。
func summarizePayload(kind domain.InboxKind, payload []byte) string {
	switch kind {
	case domain.InboxKindFact:
		var p domain.InboxFactPayload
		if json.Unmarshal(payload, &p) == nil {
			return strings.TrimSpace(p.Key + " = " + p.Value)
		}
	case domain.InboxKindProcedure:
		var p domain.InboxProcedurePayload
		if json.Unmarshal(payload, &p) == nil {
			return strings.Join(p.Steps, " → ")
		}
	case domain.InboxKindSkill:
		var p domain.InboxSkillPayload
		if json.Unmarshal(payload, &p) == nil {
			return strings.TrimSpace(p.Description)
		}
	}
	return ""
}

// inboxOrDefault 空值回落默认（与 provider.go 的数值版同名冲突，故加前缀）。
func inboxOrDefault(v, def string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func orSource(s domain.InboxSource) domain.InboxSource {
	if s == "" {
		return domain.InboxSourceLLM
	}
	return s
}

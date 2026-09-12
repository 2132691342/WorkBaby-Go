package api

import (
	"WorkBaby/internal/domain"
)

// ListInbox 回写收件箱条目（status 空 = 全部）。
func (h *Handler) ListInbox(status string, limit int) ([]domain.InboxItemRESP, error) {
	if h.inboxSvc == nil {
		return []domain.InboxItemRESP{}, nil
	}
	return h.inboxSvc.List(h.ctx, status, limit)
}

// GetInboxStats 收件箱概览统计。
func (h *Handler) GetInboxStats() (domain.InboxStatsRESP, error) {
	if h.inboxSvc == nil {
		return domain.InboxStatsRESP{}, nil
	}
	return h.inboxSvc.Stats(h.ctx)
}

// ApproveInboxItem 合入一条候选（按 kind 写入语义记忆 / 程序记忆 / 技能库）。
func (h *Handler) ApproveInboxItem(id string) (*domain.InboxItemRESP, error) {
	if h.inboxSvc == nil {
		return nil, domain.ErrInboxNotFound
	}
	return h.inboxSvc.Approve(h.ctx, id)
}

// RejectInboxItem 拒绝一条候选。
func (h *Handler) RejectInboxItem(id string) error {
	if h.inboxSvc == nil {
		return domain.ErrInboxNotFound
	}
	return h.inboxSvc.Reject(h.ctx, id)
}

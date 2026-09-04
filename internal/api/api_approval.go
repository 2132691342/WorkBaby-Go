package api

import "WorkBaby/internal/domain"

// DecideApproval 用户对工具审批请求（chat:approval 事件）的决策回填。
// 阻塞中的 exec 工具调用据此放行/拒绝；请求不存在或已超时返回 4003。
func (h *Handler) DecideApproval(id string, req domain.DecideApprovalREQ) error {
	return h.approvalSvc.Decide(id, req.Approved)
}

// AnswerInput 用户对补充输入请求（request_input 工具，risk=input_required）的回复回填。
func (h *Handler) AnswerInput(id string, req domain.AnswerInputREQ) error {
	return h.approvalSvc.Answer(id, req.Answer)
}

// ListPendingApprovals 返回当前未决审批（前端刷新/重连后恢复审批用）。
func (h *Handler) ListPendingApprovals() []domain.ApprovalPendingRESP {
	return h.approvalSvc.Pending()
}

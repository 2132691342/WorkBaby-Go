package domain

// ApprovalPendingRESP 未决审批快照（§8 审批恢复：前端刷新后重拉注入）。
type ApprovalPendingRESP struct {
	ID        string `json:"id"` // approvalID，回填 /chat/approval/{id}/decide 用
	RunID     string `json:"run_id"`
	SessionID string `json:"session_id"`
	Command   string `json:"command"`
	Reason    string `json:"reason"`
	Risk      string `json:"risk"`
	ExpiresAt int64  `json:"expires_at"`
}

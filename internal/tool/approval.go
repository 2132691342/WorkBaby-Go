package tool

import "context"

// 审批风险语义（与前端 ChatStreamDecoder.ApprovalRequest.risk 对齐）：
const (
	// RiskApprovalNeeds 白名单外但可恢复：同命令批准过一次后免审（进程内记忆）。
	RiskApprovalNeeds = "needs_approval"
	// RiskApprovalIrrev 命中危险正则（不可逆操作）：每次都问，永不免审。
	RiskApprovalIrrev = "irreversible"
)

// Approver 高风险命令审批门：阻塞等待用户决策后放行/拒绝。
//
// 抽象定义在 tool 包（ExecTool 依赖），实现位于 service 层（ApprovalService）——
// tool 包不感知事件总线与前端，符合分层铁律（internal/{能力域} 不引上层）。
type Approver interface {
	// Approve 阻塞等待用户决策；返回 true = 放行执行，false = 拒绝 / 超时 / 取消。
	// ctx 携带 harness 注入的 runID/sessionID（RunIDFromCtx），用于事件路由。
	Approve(ctx context.Context, command string, risk string) bool
}

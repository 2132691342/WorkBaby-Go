package tool

import (
	"context"
	"encoding/json"
)

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

// RiskClassifier 按本次调用参数计算风险与人类可读描述（工具可选实现）。
//
// <p>统一护栏链的关键拼图：exec / skillrun 的「命令级」风险（白名单 / 危险正则）
// 经此上移到执行链统一裁决——runner 的策略门拿它给出精准审批描述与 per-call
// 风险，工具自身不再内置第二道审批。返回空 description 表示无法按参数分类
// （退回工具级静态风险）。
type RiskClassifier interface {
	ClassifyArgs(args json.RawMessage) (description string, risk string)
}

// guardChainKey 标记「runner 护栏链已对本调用完成策略裁决与审批」。
type guardChainKey struct{}

// WithGuardChain 标记 ctx：本次工具调用已经过 runner 护栏链（策略门 + 审批）。
// 工具内部据此跳过自有审批兜底，消除双层闸门的重复询问。
func WithGuardChain(ctx context.Context) context.Context {
	return context.WithValue(ctx, guardChainKey{}, true)
}

// GuardChainActive 报告本次调用是否来自 runner 护栏链。
func GuardChainActive(ctx context.Context) bool {
	v, _ := ctx.Value(guardChainKey{}).(bool)
	return v
}

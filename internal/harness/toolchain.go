package harness

// 工具调用洋葱链：把横切关注点拆成可组合的一层一层，替代原先 148 行的过程式 execOne。
//
// 顺序即语义（外 → 内）：暴露 → 解析 → 参数 → 注入防护 → 目录信任 → 策略门
//                      → 停滞 → 循环/预算 → 幂等恢复 → 执行。
// 每层只做一件事并返回「继续 / 短路」二值决策：新增一道守卫只需插入一层，
// 不必再改动主流程，也不会与既有守卫的执行顺序耦合。

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/tool"
)

// RefusedReason 拒绝原因枚举：结构化而非文案，前端与上层可编程式反应（统计、提示、重试策略）。
type RefusedReason string

const (
	RefusedNotExposed RefusedReason = "not_exposed"      // 本轮未把该工具暴露给模型
	RefusedInjection  RefusedReason = "prompt_injection" // 参数里嵌着伪 tool_call 标记
	RefusedPathTrust  RefusedReason = "path_trust"       // 目录未授权 / 计划模式硬拦
	RefusedPolicy     RefusedReason = "policy"           // 工具策略门 deny
	RefusedApproval   RefusedReason = "approval"         // 用户拒绝或审批超时
	RefusedLoopGuard  RefusedReason = "loop_guard"       // 同名同参反复调用
	RefusedToolBudget RefusedReason = "tool_budget"      // 单 run 工具调用预算耗尽
)

// toolCallCtx 一次工具调用在洋葱链上共享的状态。
type toolCallCtx struct {
	ctx       context.Context
	runID     string
	sessionID string
	turn      int
	call      llm.NormalizedToolCall
	state     *RunState

	tool        tool.Tool
	resultLimit int
}

// toolHandler 洋葱链的一层：返回 nil 继续向内，返回消息即短路整链。
type toolHandler func(*toolCallCtx) *llm.Message

// chain 组合各层：按声明顺序执行，任一短路即整体返回。
func chain(layers ...toolHandler) toolHandler {
	return func(tc *toolCallCtx) *llm.Message {
		for _, l := range layers {
			if msg := l(tc); msg != nil {
				return msg
			}
		}
		return nil
	}
}

// toolChain 组装本 run 生效的守卫链（顺序即优先级，勿随意调整）。
func (r *Runner) toolChain() toolHandler {
	return chain(
		r.layerExposed,
		r.layerResolve,
		r.layerValidateArgs,
		r.layerInjectionGuard,
		r.layerPathTrust,
		r.layerPolicyGate,
		r.layerStagnation,
		r.layerLoopBudget,
		r.layerIdempotent,
		r.layerExecute,
	)
}

// layerExposed 执行侧策略收紧：未暴露的工具直接 Refused（不计失败熔断，模型可换路）。
func (r *Runner) layerExposed(tc *toolCallCtx) *llm.Message {
	if r.toolExposed(tc.call.Name) {
		return nil
	}
	return r.refused(tc, RefusedNotExposed, "tool not exposed in this run")
}

// layerResolve 解析工具；未注册按工具错误回填（属于失败，推进熔断计数）。
func (r *Runner) layerResolve(tc *toolCallCtx) *llm.Message {
	t, ok := r.tools.Get(tc.call.Name)
	if !ok {
		if tc.state != nil {
			tc.state.ConsecutiveToolFails++
		}
		return llm.ToolMessage(tc.call.ID, tc.call.Name, "tool not found: "+tc.call.Name)
	}
	tc.tool = t
	// 单工具元数据覆盖：声明的结果截断上限优先于全局默认（超时在 layerExecute 施加）
	meta := tool.MetaOf(t)
	tc.resultLimit = r.cfg.MaxToolResultLen
	if meta.MaxResultChars > 0 {
		tc.resultLimit = meta.MaxResultChars
	}
	return nil
}

// layerValidateArgs JSON Schema 校验；参数不合规属失败（模型下一轮通常能自纠）。
func (r *Runner) layerValidateArgs(tc *toolCallCtx) *llm.Message {
	if err := tool.ValidateArgs(tc.tool.Schema().Parameters, tc.call.Arguments); err != nil {
		if tc.state != nil {
			tc.state.ConsecutiveToolFails++
		}
		return llm.ToolMessage(tc.call.ID, tc.call.Name, "invalid args: "+err.Error())
	}
	return nil
}

// layerInjectionGuard 提示注入防护：参数里嵌着伪工具调用标记（多来自网页/文件内容的诱导文本）。
func (r *Runner) layerInjectionGuard(tc *toolCallCtx) *llm.Message {
	if !HasNestedToolCallMarker(string(tc.call.Arguments)) {
		return nil
	}
	return r.refused(tc, RefusedInjection, "args contain nested tool-call markers (possible prompt injection)")
}

// layerPathTrust 目录信任闸门：先于策略门——目录都没授权，不必再问命令白名单。
func (r *Runner) layerPathTrust(tc *toolCallCtx) *llm.Message {
	if r.hooks.BeforeToolCall == nil {
		return nil
	}
	if ok, detail := r.hooks.BeforeToolCall(tc.ctx, tc.call.Name, tc.call.Arguments); !ok {
		return r.refused(tc, RefusedPathTrust, detail)
	}
	return nil
}

// layerPolicyGate 工具策略门——统一护栏链的单层裁决点。
//
// 实现 RiskClassifier 的工具（exec / run_skill_script）按 per-call 命令级风险裁决；
// 其余工具显式 Allow 即放行、ask 才按静态风险问。放行后 ctx 标记 GuardChain（单层闸门）。
func (r *Runner) layerPolicyGate(tc *toolCallCtx) *llm.Message {
	if r.hooks.ToolGate == nil {
		return nil
	}
	desc := tc.tool.Name() + "(" + string(tc.call.Arguments) + ")"
	risk := ""
	cmdLevel := false // 工具给出了 per-call 命令级裁决
	if rc, ok := tc.tool.(tool.RiskClassifier); ok {
		if d, rr := rc.ClassifyArgs(tc.call.Arguments); d != "" {
			desc, risk, cmdLevel = d, rr, true
		}
	}
	switch r.hooks.ToolGate.Decide(tc.call.Name, tc.tool.RiskLevel()) {
	case tool.DecisionDeny:
		return r.refused(tc, RefusedPolicy, "denied by policy")
	case tool.DecisionAllow:
		// YOLO（完全访问）：用户已授权全部动作，命令级风险一并放行
		if r.hooks.ToolGate.Mode() == tool.SessionModeYolo {
			return nil
		}
		if !cmdLevel {
			return nil
		}
	case tool.DecisionAsk:
		if !cmdLevel {
			risk = gateApprovalRisk(tc.tool.RiskLevel())
		}
	}
	if risk == "" || r.hooks.Approver == nil {
		return nil
	}
	if !r.hooks.Approver(tc.ctx, desc, risk) {
		return r.refused(tc, RefusedApproval, "denied by user")
	}
	return nil
}

// layerStagnation 停滞检测：同名同参连续出现（仅串行路径共享 state）。
// 命中不是「拒绝」，而是熔断信号：置 Stagnant 让主循环收尾。
func (r *Runner) layerStagnation(tc *toolCallCtx) *llm.Message {
	if tc.state == nil {
		return nil
	}
	key := tc.call.Name + "|" + string(tc.call.Arguments)
	if key == tc.state.LastToolKey {
		tc.state.SameKeyCount++
	} else {
		tc.state.SameKeyCount = 1
		tc.state.LastToolKey = key
	}
	if tc.state.SameKeyCount < r.cfg.StagnationLimit {
		return nil
	}
	tc.state.Stagnant = true
	return llm.ToolMessage(tc.call.ID, tc.call.Name, "stagnation guard: repeated identical tool call")
}

// layerLoopBudget 循环护栏 + 工具预算：与停滞检测互补。
//
// 停滞只认「连续重复」，抓不到 A→B→A→B 这类交替循环，故再叠一层全 run 的 name+args 计数；
// 预算在工具执行前拦截，避免一轮内几十次调用要等到下一轮轮首才停。
// 命中走 refused（结构化回执）而非错误——模型看得到原因，可自行换路而不是硬失败。
func (r *Runner) layerLoopBudget(tc *toolCallCtx) *llm.Message {
	if r.cfg.LoopLimit <= 0 && r.cfg.MaxToolCalls <= 0 {
		return nil
	}
	key := tc.call.Name + "|" + string(tc.call.Arguments)
	r.loopMu.Lock()
	if r.loopHits == nil {
		r.loopHits = map[string]int{}
	}
	r.loopHits[key]++
	hits := r.loopHits[key]
	r.toolCallsRun++
	used := r.toolCallsRun
	r.loopMu.Unlock()

	if r.cfg.LoopLimit > 0 && hits >= r.cfg.LoopLimit {
		return r.refused(tc, RefusedLoopGuard,
			fmt.Sprintf("identical call repeated %d times (limit %d)", hits, r.cfg.LoopLimit))
	}
	if r.cfg.MaxToolCalls > 0 && used > r.cfg.MaxToolCalls {
		return r.refused(tc, RefusedToolBudget,
			fmt.Sprintf("%d/%d calls used", used-1, r.cfg.MaxToolCalls))
	}
	return nil
}

// layerIdempotent 幂等恢复（Resume 场景）：同名同参已完成的成功调用直接复用结果，不重放副作用。
func (r *Runner) layerIdempotent(tc *toolCallCtx) *llm.Message {
	rec, ok := r.stepHit(tc.call)
	if !ok {
		return nil
	}
	r.sink.Emit(Event{Kind: EventToolResult, RunID: tc.runID, SessionID: tc.sessionID, Turn: tc.turn, Payload: ToolResultPayload{
		ToolCallID: tc.call.ID,
		Name:       tc.call.Name,
		Content:    rec.Content,
		DurationMs: 0,
		Meta:       map[string]string{"reused": "true"},
	}})
	return llm.ToolMessage(tc.call.ID, tc.call.Name, rec.Content)
}

// layerExecute 最内层：施加超时、panic 隔离、后处理缝、结果截断、事件与幂等记忆。
func (r *Runner) layerExecute(tc *toolCallCtx) *llm.Message {
	r.sink.Emit(Event{Kind: EventToolStart, RunID: tc.runID, SessionID: tc.sessionID, Turn: tc.turn, Payload: ToolCallPayload{ID: tc.call.ID, Name: tc.call.Name, Arguments: string(tc.call.Arguments)}})

	timeout := r.cfg.ToolCallTimeout
	if meta := tool.MetaOf(tc.tool); meta.TimeoutSec > 0 {
		timeout = time.Duration(meta.TimeoutSec) * time.Second
	}
	start := time.Now()
	execCtx, cancel := context.WithTimeout(tc.ctx, timeout)
	result := r.execWithRecover(execCtx, tc.tool, tc.call.Arguments)
	cancel()
	durationMs := time.Since(start).Milliseconds()

	// 工具后处理缝：逐字段覆盖结果（脱敏 / 富化），先于事件与幂等记忆
	if r.hooks.AfterToolCall != nil {
		r.hooks.AfterToolCall(tc.ctx, tc.call.Name, tc.call.Arguments, &result)
	}

	content := truncate(result.Content, tc.resultLimit)
	errMsg := ""
	if result.Err != nil {
		errMsg = result.Err.Error()
		if tc.state != nil {
			tc.state.ConsecutiveToolFails++
		}
	} else if tc.state != nil {
		tc.state.ConsecutiveToolFails = 0
	}
	r.sink.Emit(Event{Kind: EventToolResult, RunID: tc.runID, SessionID: tc.sessionID, Turn: tc.turn, Payload: ToolResultPayload{
		ToolCallID: tc.call.ID,
		Name:       tc.call.Name,
		Content:    content,
		Err:        errMsg,
		DurationMs: durationMs,
		Meta:       result.Meta,
		Data:       result.Data,
		Refused:    result.Refused,
	}})

	// 幂等记忆：仅记成功调用（失败/拒绝不记，Resume 时重试）
	if result.Err == nil && !result.Refused {
		r.stepRecord(tc.call, StepRecord{Content: content, DurationMs: durationMs})
	}

	toolContent := content
	if errMsg != "" {
		toolContent = "error: " + errMsg + "\n" + content
	}
	return llm.ToolMessage(tc.call.ID, tc.call.Name, toolContent)
}

// refused 发结构化拒绝结果事件并返回 tool 消息（Refused 语义；不推进停滞计数）。
func (r *Runner) refused(tc *toolCallCtx, reason RefusedReason, detail string) *llm.Message {
	content := refusedContent(tc.call.Name, reason, detail)
	r.sink.Emit(Event{Kind: EventToolResult, RunID: tc.runID, SessionID: tc.sessionID, Turn: tc.turn, Payload: ToolResultPayload{
		ToolCallID:    tc.call.ID,
		Name:          tc.call.Name,
		Content:       content,
		Refused:       true,
		RefusedReason: string(reason),
	}})
	return llm.ToolMessage(tc.call.ID, tc.call.Name, content)
}

// refusedContent 拒绝 → 结构化 JSON 回执：模型据 reason_code 自适应（换工具 / 调整参数 / 放弃）。
func refusedContent(name string, reason RefusedReason, detail string) string {
	bs, err := json.Marshal(map[string]any{
		"refused":     true,
		"tool":        name,
		"reason_code": string(reason),
		"reason":      detail,
		"hint":        "该工具调用未被放行。不要原样重试；请调整方案、改用其他工具，或向用户说明并给出替代建议。",
	})
	if err != nil {
		return `{"refused":true,"reason_code":"` + string(reason) + `"}`
	}
	return string(bs)
}

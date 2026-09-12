package harness

import (
	"context"
	"strings"
	"time"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/pkg"
)

// 子 Agent 委派预算：父 run 的资源不能被单个委派吃光。
const (
	delegateMaxTurns    = 12
	delegateWallTime    = 3 * time.Minute
	delegateSummaryMax  = 4000 // 摘要回传的字符上限（rune）
	delegateToolTimeout = 2 * time.Minute
)

// delegateFlight 同参委派的去重句柄：并发相同 (agent, task) 只跑一次。
type delegateFlight struct {
	done    chan struct{}
	summary string
	err     error
}

// Delegate 以指定 Agent 定义跑一个子任务，只把摘要回传给父 Agent（tool.Delegator 实现）。
//
// 四重隔离：子 run 只看到「人设 + 任务」、独立预算、工具集只能收缩不能升权、正文流不进父回答。
// 并发相同 (agent, task) 共享一次执行；工具层事件转发父 sink 供前端展示过程。
func (r *Runner) Delegate(ctx context.Context, agentName, task string) (string, error) {
	if r.provider == nil || r.tools == nil {
		return "", pkg.New(5008, "委派不可用：当前运行环境未配置模型或工具", "")
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return "", pkg.New(5008, "委派失败：任务描述为空", "")
	}
	def := Agent(agentName) // 未知名回退 default

	// 并发同参去重：命中在飞委派则等待其结果，不重复扇出
	key := def.Name + "|" + task
	r.delegateMu.Lock()
	if r.delegateFlights == nil {
		r.delegateFlights = map[string]*delegateFlight{}
	}
	if f, ok := r.delegateFlights[key]; ok {
		r.delegateMu.Unlock()
		select {
		case <-f.done:
			return f.summary, f.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	flight := &delegateFlight{done: make(chan struct{})}
	r.delegateFlights[key] = flight
	r.delegateMu.Unlock()
	defer func() {
		close(flight.done)
		r.delegateMu.Lock()
		delete(r.delegateFlights, key)
		r.delegateMu.Unlock()
	}()

	parentRunID := RunIDFromCtx(ctx)
	sessionID := SessionIDFromCtx(ctx)
	childRunID := pkg.NewID("RUN")
	if r.execs != nil {
		r.execs.Register(&ExecutionRun{
			RunID:       childRunID,
			ParentRunID: parentRunID,
			SessionID:   sessionID,
			AgentName:   def.Name,
			Scope:       ScopeDelegate,
			State:       StateRunning,
		})
		defer func() {
			if r.execs != nil {
				r.execs.Finish(childRunID, StateCompleted)
			}
		}()
	}

	// 子 run 事件转发：只转发工具层与错误，正文/思考不转发（避免混入父回答）
	sink := FuncSink(func(e Event) {
		switch e.Kind {
		case EventToolCall, EventToolStart, EventToolResult, EventError, EventRunStart, EventRunDone:
			e.ParentRunID = parentRunID
			e.Agent = def.Name
			r.sink.Emit(e)
		}
	})

	cfg := r.cfg
	if cfg.MaxTurns <= 0 || cfg.MaxTurns > delegateMaxTurns {
		cfg.MaxTurns = delegateMaxTurns
	}
	cfg.ToolCallTimeout = delegateToolTimeout
	cfg.ContextBudget = 0 // 子 run 上下文天然短小，无需压缩
	child := NewRunner(r.provider, sink, cfg).
		WithTools(r.tools, def.FilterTools(r.toolDefs)).
		WithRequestParams(r.reqParams).
		WithProviderParams(r.providerParams).
		WithDefaults(r.defaults)
	// 继承父 run 的护栏钩子：子 Agent 与父 run 受同一套权限模式、目录信任与审批约束。
	// 不继承会退化为「无策略门」——exec / run_skill_script 这类靠策略门裁决的工具
	// 将在无闸门状态下执行，安全性只能依赖工具自身兜底而非统一的单层闸门。
	child.hooks.ToolGate = r.hooks.ToolGate
	child.hooks.Approver = r.hooks.Approver
	child.hooks.BeforeToolCall = r.hooks.BeforeToolCall

	msgs := make([]*llm.Message, 0, 2)
	if persona := def.PersonaSystemMessage(); persona != nil {
		msgs = append(msgs, persona)
	}
	msgs = append(msgs, llm.UserMessage(task))

	childCtx, cancel := context.WithTimeout(WithRunContext(ctx, childRunID, sessionID), delegateWallTime)
	defer cancel()

	res := child.RunMessages(childCtx, childRunID, sessionID, "", r.model, msgs)
	// 子 run 的消耗单独上报（turn 沿子 run 编号，由 service 决定落库口径）；
	// 丢弃会让 token_usages 系统性漏计整个委派的用量。
	if r.OnDelegateUsage != nil && len(res.Turns) > 0 {
		r.OnDelegateUsage(def.Name, res.Turns)
	}
	if res.Err != nil {
		flight.err = pkg.Wrap(5008, "子任务执行失败", res.Err)
		return "", flight.err
	}
	flight.summary = pkg.TruncateRunes(strings.TrimSpace(res.Content), delegateSummaryMax)
	return flight.summary, nil
}

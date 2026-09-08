// Package harness 是 WorkBaby 的 Agent 内核（自研轻量 ReAct 循环；不依赖 wails / api / service）。
package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"WorkBaby/internal/llm"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/tool"
)

// RunStopReason 终止原因枚举。
type RunStopReason string

const (
	ReasonEndTurn        RunStopReason = "end_turn"
	ReasonMaxTokens      RunStopReason = "max_tokens"
	ReasonBudgetExceeded RunStopReason = "budget_exceeded"
	ReasonMaxTurns       RunStopReason = "max_turns"
	ReasonStagnation     RunStopReason = "stagnation"
	ReasonCancelled      RunStopReason = "cancelled"
	ReasonError          RunStopReason = "error"
)

// Config Runner 配置。
type Config struct {
	MaxTurns         int           // 默认 30
	MaxTokens        int           // harness 预算，默认 0 = 不限
	StreamTime       time.Duration // 单轮流式超时
	ToolCallTimeout  time.Duration // 工具执行超时，默认 5min
	StagnationLimit  int           // 连续失败/重复调用熔断阈值，默认 5
	LoopLimit        int           // 同名同参在整 run 内的累计次数上限；默认 3；0 = 关
	MaxToolCalls     int           // 单 run 工具执行总次数预算；默认 0 = 不限
	MaxToolResultLen int           // 工具结果回填 LLM 的截断长度，默认 50k
	ToolParallelism  int           // 只读工具并发数；默认 4；1 = 串行
	ContextBudget    int           // 单轮消息估算 token 预算；超预算每轮前自动压缩；0 = 关
	CompressTrigger  int           // 触发历史截断的估算 token 数，默认 0 = 不截断（旧字段，由 ContextBudget 取代）
	CompressRatio    float64       // 截断比例，默认 0.9
}

// DefaultConfig 默认值。
func DefaultConfig() Config {
	return Config{
		MaxTurns:         30,
		MaxTokens:        0,
		ToolCallTimeout:  5 * time.Minute,
		StagnationLimit:  5,
		LoopLimit:        3,
		MaxToolCalls:     0,
		MaxToolResultLen: 50_000,
		ToolParallelism:  4,
		ContextBudget:    120_000, // 估算 token 上限
		CompressRatio:    0.9,
	}
}

// RunState 单次 run 的运行状态（含停滞检测）。
type RunState struct {
	ToolCallCount        int    // 已发起的工具调用数
	ConsecutiveToolFails int    // 连续失败计数
	LastToolKey          string // 上一次工具调用签名（name+args），用于停滞检测
	SameKeyCount         int    // 同一签名连续出现次数
	Stagnant             bool   // 已触发停滞熔断
}

// LoopHooks 循环缝集合：按在循环中的位置从外到里排开，nil 字段 = 关闭（走默认行为）。
//
//	Steering / FollowUp   注入缝——跑过工具的轮之间 / 本轮收尾后续接
//	PrepareNextTurn       轮间调整——下一轮换模型 / 换工具集（流式失败降级也经此）
//	ShouldStopAfterTurn   优雅停止点——任务已完成等主动终止，区别于停滞/预算类被动熔断
//	BeforeToolCall        工具执行前拦截（目录信任三态）
//	AfterToolCall         工具执行后逐字段覆盖结果（脱敏 / 富化 / 前端提示增强）
type LoopHooks struct {
	Steering            Injector
	FollowUp            Injector
	PrepareNextTurn     TurnAdjuster
	ShouldStopAfterTurn func(ctx context.Context, sig *TurnSignal) bool
	BeforeToolCall      PathTrust
	AfterToolCall       func(ctx context.Context, name string, args json.RawMessage, res *tool.ToolResult)
	ToolGate            *tool.Gate
	Approver            func(ctx context.Context, description, risk string) bool
}

// Runner 控制循环主控（多轮 ReAct）；模型可调用工具，每轮执行并回填结果，直到无调用或达终止条件。
type Runner struct {
	provider        llm.Provider
	sink            Sink
	usage           *TokenUsageAccumulator
	tools           *tool.Registry
	toolDefs        []llm.ToolDefinition
	middlewares     []Middleware
	cfg             Config
	checkpoints     CheckpointStore            // 可选；nil = 不落检查点（JSONL 或 SQL 实现）
	hooks           LoopHooks                  // 循环缝集合（nil 字段 = 关闭）
	compressor      Compressor                 // 上下文压缩器；默认 Micro；nil 时 ByContextBudget 关闭
	reqParams       RequestParams              // 请求级采样参数（chat 透传；nil = 不覆盖）
	providerParams  *llm.ProviderParams        // Provider 级（ai_providers.temperature/thinking）；nil = 走全局
	defaults        llm.Defaults               // 全局默认（system_settings.chat.defaultTemperature/Thinking）
	model           string                     // 当前 run 的模型（子 Agent 委派继承同一模型）
	execs           *ExecutionRegistry         // 执行平面；委派时登记子 run 拓扑（nil = 不登记）
	modelSwitches   int                        // 本 run 已发生的模型切换次数（有界防横跳）
	tokenScale      float64                    // 估算→实测校准系数（EMA）；<=0 视作 1（未校准）
	lastMeasuredIn  int                        // 上一轮上游实测的 prompt token 数
	loopMu          sync.Mutex                 // 循环护栏 / 工具计数保护（并行只读路径也走 execOne）
	loopHits        map[string]int             // run 内 name+args → 累计调用次数
	toolCallsRun    int                        // 本 run 已执行的工具次数（预算护栏）
	steps           map[string]StepRecord      // 幂等恢复：已完成成功工具调用（name+args → 结果）
	stepsMu         sync.Mutex                 // 工具并发路径保护 steps
	delegateMu      sync.Mutex                 // 委派去重保护
	delegateFlights map[string]*delegateFlight // 同参委派在飞表（agent|task → flight）
}

// PathTrust 目录信任闸门。
//
// 输入工具名与原始参数，由 service 侧抽取目标目录并解析信任三态：
//   - ok=true：放行（目录已信任，或 ask 经人工批准）；
//   - ok=false：拒绝，reason 直接进工具回执——与 Refused 同构，模型可据此改道
//     （例如改用工作区内的目录）而不是把拒绝当故障硬终止。
//
// 询问（阻塞等待人工决策）在钩子内部完成，harness 不感知审批机制。
type PathTrust func(ctx context.Context, toolName string, args json.RawMessage) (ok bool, reason string)

// TurnUpdate turn 间的热切换载荷；非 nil 字段在下一轮或本轮重试时生效。
type TurnUpdate struct {
	Model *string
	// Tools 替换下一轮暴露给模型的工具定义（执行侧 toolExposed 校验同步生效）。
	Tools *[]llm.ToolDefinition
}

// TurnSignal 传给调整器的本轮信号，降级策略据此决策。
type TurnSignal struct {
	Turn                 int
	ConsecutiveToolFails int
	StreamErr            error // 本轮 LLM 流式调用失败原因（nil = 正常收尾）
}

// TurnAdjuster turn 间调整钩子：返回要应用的 TurnUpdate（空值 = 不调整）。
type TurnAdjuster func(ctx context.Context, sig *TurnSignal) TurnUpdate

// Injector 注入缝：返回要追加进上下文的消息；返回空表示不注入。
//
// 两条缝的分工：
//   - steering：turn 与 turn 之间，且仅在本轮跑过工具后触发——「一次持续工作中的中途插话」；
//   - followUp：模型说完了（本轮无工具调用）之后触发——「一轮对话结束后的自动续接」。
//
// 缝的实现由 service 提供（从注入队列取消息），harness 不感知来源。
type Injector func(ctx context.Context) []*llm.Message

// checkpointKeepRuns 每会话保留的最近 run 检查点数。
const checkpointKeepRuns = 3

// RequestParams 单次 run 的请求级采样参数。
type RequestParams struct {
	Temperature *float64
	Thinking    *llm.ThinkingConfig
}

// WithRequestParams 注入请求级采样参数（温度 / 思考开关）。
func (r *Runner) WithRequestParams(p RequestParams) *Runner { r.reqParams = p; return r }

// WithProviderParams 注入 Provider 级采样参数（构建 Runner 时由 service 设置）。
func (r *Runner) WithProviderParams(p *llm.ProviderParams) *Runner { r.providerParams = p; return r }

// WithDefaults 注入全局默认采样参数（service 从 system_settings 读取）。
func (r *Runner) WithDefaults(d llm.Defaults) *Runner { r.defaults = d; return r }

// WithSteering 启用转向注入缝（每轮工具执行后、下一轮前）。nil = 关闭。
func (r *Runner) WithSteering(inj Injector) *Runner { r.hooks.Steering = inj; return r }

// WithFollowUp 启用续接注入缝（本轮无工具调用、run 即将收尾时）。nil = 关闭。
func (r *Runner) WithFollowUp(inj Injector) *Runner { r.hooks.FollowUp = inj; return r }

// WithTurnAdjuster 启用轮间调整钩子：LLM 流式失败时换模型重试本轮、
// turn 收尾时可为下一轮换模型 / 换工具集。nil = 关闭。
func (r *Runner) WithTurnAdjuster(adj TurnAdjuster) *Runner { r.hooks.PrepareNextTurn = adj; return r }

// WithShouldStopAfterTurn 启用优雅停止点：每轮收尾后询问「该停了吗」。
// 区别于停滞/预算类被动熔断，这是外部感知任务完成后的主动终止（end_turn 语义）。nil = 关闭。
func (r *Runner) WithShouldStopAfterTurn(fn func(ctx context.Context, sig *TurnSignal) bool) *Runner {
	r.hooks.ShouldStopAfterTurn = fn
	return r
}

// WithAfterToolCall 启用工具后处理钩子：拿到 ToolResult 后、事件发出前逐字段覆盖
// （脱敏、富化、前端展示增强）。nil = 关闭。
func (r *Runner) WithAfterToolCall(fn func(ctx context.Context, name string, args json.RawMessage, res *tool.ToolResult)) *Runner {
	r.hooks.AfterToolCall = fn
	return r
}

// WithPathTrust 启用目录信任闸门：工具执行前先过信任三态，挂在工具策略门之前。
// nil = 关闭（维持既有行为）。
func (r *Runner) WithPathTrust(p PathTrust) *Runner { r.hooks.BeforeToolCall = p; return r }

// streamWithRetry 建流分类有界重试：仅重试瞬时错误（限流/5xx/超时），
// 指数退避+抖动，Retry-After 优先，取消优先；流中途错误不重试（避免重复输出）。
func (r *Runner) streamWithRetry(ctx context.Context, req *llm.ChatRequest, runID, sessionID string, turn int) (<-chan llm.StreamChunk, error) {
	policy := llm.DefaultRetryPolicy()
	stream, err := r.provider.Stream(ctx, req)
	for attempt := 0; err != nil && llm.IsTransient(err) && attempt+1 < policy.MaxAttempts; attempt++ {
		delay := policy.Backoff(attempt, llm.RetryAfter(err))
		r.sink.Emit(Event{
			Kind: EventRetry, RunID: runID, SessionID: sessionID, Turn: turn,
			Payload: RetryPayload{Attempt: attempt + 1, DelayMs: delay.Milliseconds(), Reason: err.Error()},
		})
		if werr := llm.Wait(ctx, delay); werr != nil {
			return nil, err
		}
		stream, err = r.provider.Stream(ctx, req)
	}
	return stream, err
}

// applyTurnUpdate 询问轮间调整钩子；工具集替换不受切换次数限制，
// 模型切换同一 run 最多 StagnationLimit 次（防主备横跳）。返回是否换了模型。
func (r *Runner) applyTurnUpdate(ctx context.Context, sig *TurnSignal, current string) (string, bool) {
	if r.hooks.PrepareNextTurn == nil || r.modelSwitches >= r.cfg.StagnationLimit {
		return "", false
	}
	up := r.hooks.PrepareNextTurn(ctx, sig)
	if up.Tools != nil {
		r.toolDefs = *up.Tools
	}
	if up.Model == nil || *up.Model == "" || *up.Model == current {
		return "", false
	}
	r.modelSwitches++
	return *up.Model, true
}

// NewRunner 构造 Runner（无工具）。
func NewRunner(p llm.Provider, sink Sink, cfg Config) *Runner {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 30
	}
	if cfg.ToolCallTimeout <= 0 {
		cfg.ToolCallTimeout = 5 * time.Minute
	}
	if cfg.StagnationLimit <= 0 {
		cfg.StagnationLimit = 5
	}
	if cfg.MaxToolResultLen <= 0 {
		cfg.MaxToolResultLen = 50_000
	}
	mws := defaultMiddlewares()
	// CompressTrigger 仅在调用方关闭 ContextBudget 且仍想按阈值截断时显式追加 HistoryTruncator；
	// 现代配置走 ContextBudget + Micro 压缩器每轮压缩，两者不并存。
	if cfg.CompressTrigger > 0 {
		mws = append(mws, NewHistoryTruncator(cfg.CompressTrigger, cfg.CompressRatio))
	}
	return &Runner{
		provider:    p,
		sink:        sink,
		usage:       &TokenUsageAccumulator{},
		toolDefs:    nil,
		middlewares: mws,
		compressor:  MicroCompressor{},
		cfg:         cfg,
	}
}

// WithTools 注入工具注册中心与已过滤的工具定义（由装配方按启停策略构造）。
func (r *Runner) WithTools(reg *tool.Registry, defs []llm.ToolDefinition) *Runner {
	r.tools = reg
	r.toolDefs = defs
	return r
}

// WithMiddleware 追加中间件（默认链已含 TokenCounter/HistoryTruncator）。
func (r *Runner) WithMiddleware(m ...Middleware) *Runner {
	r.middlewares = append(r.middlewares, m...)
	return r
}

// WithCheckpointDir 启用 JSONL 文件检查点；dir 为空则禁用。
func (r *Runner) WithCheckpointDir(dir string) *Runner {
	if dir != "" {
		r.checkpoints = NewCheckpointStore(dir)
	}
	return r
}

// WithCheckpointStore 注入自定义检查点存储（如 service 的 SQL 实现），覆盖 WithCheckpointDir。
func (r *Runner) WithCheckpointStore(store CheckpointStore) *Runner {
	if store != nil {
		r.checkpoints = store
	}
	return r
}

// WithToolGate 启用工具策略门（allow/ask/deny × SessionMode）。
// approve 委托人工审批（通常包 ApprovalService.Approve）；approve 为 nil 时 ask 按放行处理。
func (r *Runner) WithToolGate(gate *tool.Gate, approve func(ctx context.Context, description, risk string) bool) *Runner {
	r.hooks.ToolGate = gate
	r.hooks.Approver = approve
	return r
}

// WithCompressor 覆盖默认上下文压缩器（如 Auto：watermark + LLM 摘要）；nil 关闭压缩。
func (r *Runner) WithCompressor(c Compressor) *Runner {
	r.compressor = c
	return r
}

// WithExecutionRegistry 注入执行平面：子 Agent 委派时登记 parent_run_id 拓扑。
func (r *Runner) WithExecutionRegistry(reg *ExecutionRegistry) *Runner {
	r.execs = reg
	return r
}

// TurnUsage 单轮 LLM 调用的用量与耗时（落 token_usages 明细，支撑仪表盘分线统计）。
type TurnUsage struct {
	Turn      int
	Usage     llm.TokenUsage
	LatencyMs int // 本轮「建流→流结束」墙钟耗时（含重试等待；0 = 未计量）
}

// RunResult 一次 Run 的成品（service 用于落库与返回）。
type RunResult struct {
	Content    string
	Thinking   string
	StopReason string
	Usage      llm.TokenUsage
	Turns      []TurnUsage // 每轮明细；只在有用量时追加
	Reason     RunStopReason
	Err        error
}

// RunMessages 跑一个多轮 ReAct 循环；返回完整 content/thinking 与 usage。
//
// runID / sessionID / assistantMessageID 由调用方传入；harness 透传到事件里。
func (r *Runner) RunMessages(ctx context.Context, runID, sessionID, assistantMessageID, model string, msgs []*llm.Message) RunResult {
	r.model = model
	// run 身份注入 ctx：深层工具（审批门）可从 ctx 读取，事件载荷据此路由
	ctx = WithRunContext(ctx, runID, sessionID)
	// 委派能力注入 ctx：delegate_task 工具据此发起子 Agent（子 run 继承本 run 的身份与模型）
	ctx = WithDelegator(ctx, r)

	r.sink.Emit(Event{Kind: EventRunStart, RunID: runID, SessionID: sessionID, Payload: RunStartPayload{Model: model}})

	r.steps = map[string]StepRecord{}
	return r.runLoop(ctx, runID, sessionID, assistantMessageID, model, msgs, &RunState{}, 0, "", "", llm.TokenUsage{})
}

// Resume 从检查点恢复运行：加载最后一行检查点，从下一轮继续循环。
// 未启用检查点 / 找不到检查点 / 解析失败时返回 5007 / 5005。
func (r *Runner) Resume(ctx context.Context, runID, sessionID, assistantMessageID, model string) RunResult {
	if r.checkpoints == nil {
		return RunResult{Reason: ReasonError, Err: pkg.New(5007, "检查点未启用", "")}
	}
	cp, err := r.checkpoints.LoadLast(sessionID, runID)
	if err != nil {
		return RunResult{Reason: ReasonError, Err: err}
	}
	ctx = WithDelegator(WithRunContext(ctx, runID, sessionID), r)
	r.steps = cp.StepRecords
	if r.steps == nil {
		r.steps = map[string]StepRecord{}
	}
	return r.runLoop(ctx, runID, sessionID, assistantMessageID, model, cp.Messages, &cp.State, cp.Turn+1, cp.Content, cp.Thinking, cp.Usage)
}

// runLoop 控制循环主体；startTurn 与初始累积值用于中断后 Resume。
func (r *Runner) runLoop(ctx context.Context, runID, sessionID, assistantMessageID, model string, msgs []*llm.Message, state *RunState, startTurn int, initContent, initThinking string, initUsage llm.TokenUsage) RunResult {
	r.modelSwitches = 0
	r.usage = &TokenUsageAccumulator{
		Input:      initUsage.InputTokens,
		Output:     initUsage.OutputTokens,
		CacheRead:  initUsage.CacheReadTokens,
		CacheWrite: initUsage.CacheWriteTokens,
		Total:      initUsage.TotalTokens,
	}

	var finalReason RunStopReason = ReasonEndTurn
	var stopReason string
	var finalUsage llm.TokenUsage
	var turnUsages []TurnUsage
	var contentAll, thinkingAll strings.Builder
	contentAll.WriteString(initContent)
	thinkingAll.WriteString(initThinking)

	turnsRun := 0
	var runErr error
	normalStop := false // 主动收尾 break（说完 / 优雅停止）；用于区分「跑满还想继续」的 max_turns
	for turn := startTurn; turn < r.cfg.MaxTurns; turn++ {
		turnsRun++
		if ctx.Err() != nil {
			finalReason = ReasonCancelled
			break
		}
		if r.cfg.MaxTokens > 0 && r.usage.Total >= r.cfg.MaxTokens {
			finalReason = ReasonBudgetExceeded
			break
		}

		// 上下文预算：每轮前把消息压回 ContextBudget 内；
		// Auto（结构化摘要）优先，确定性 Micro 兜底。
		// 预算先按校准系数折算再交给压缩器：压缩器内部用 EstimateTokens 比较，
		// 折算后等价于「估算 × 系数 ≤ 真实预算」，无需改动压缩器实现。
		if r.cfg.ContextBudget > 0 && r.compressor != nil {
			budget := r.calibratedBudget(r.cfg.ContextBudget)
			if cc, ok := r.compressor.(ContextCompressor); ok {
				msgs = cc.CompressCtx(ctx, msgs, budget)
			} else {
				msgs = r.compressor.Compress(msgs, budget)
			}
		}

		// 中间件：截断/压缩（BeforeTurn 调整消息）
		msgs = r.applyMiddlewares(msgs)
		// 本轮实际送入模型的估算量（含工具定义），供实测回推校准
		estAtTurn := EstimateTokens(msgs) + EstimateToolTokens(r.toolDefs)

		r.sink.Emit(Event{Kind: EventTurnStart, RunID: runID, SessionID: sessionID, Turn: turn})
		req := &llm.ChatRequest{
			Model:    model,
			Messages: msgs,
			User:     "local",
			Tools:    r.toolDefs,
		}
		// 三层合并：请求级 > Provider 级 > 全局默认
		resolved := llm.ResolveParams(req, r.providerParams, r.defaults)
		req.Temperature = resolved.Temperature
		req.TopP = resolved.TopP
		req.MaxTokens = resolved.MaxTokens
		req.Thinking = resolved.Thinking
		req.ExtraBody = resolved.ExtraBody
		// 请求级覆盖：保留 RequestParams 的显式语义（即便 provider/defaults 也有值）
		if r.reqParams.Temperature != nil {
			req.Temperature = r.reqParams.Temperature
		}
		if r.reqParams.Thinking != nil {
			req.Thinking = r.reqParams.Thinking
		}

		turnStartedAt := time.Now()
		stream, err := r.streamWithRetry(ctx, req, runID, sessionID, turn)
		if err != nil {
			// 自动降级：重试耗尽仍失败时询问调整钩子，换模型再试本轮（有界；仅连接期错误，
			// 流中途错误不动——部分增量已推送，原地重试会重复输出）
			if up, ok := r.applyTurnUpdate(ctx, &TurnSignal{Turn: turn, ConsecutiveToolFails: state.ConsecutiveToolFails, StreamErr: err}, model); ok {
				model = up
				r.model = model
				req.Model = model
				stream, err = r.streamWithRetry(ctx, req, runID, sessionID, turn)
			}
		}
		if err != nil {
			runErr = err
			finalReason = ReasonError
			break
		}

		var content, thinking strings.Builder
		var toolCalls []llm.NormalizedToolCall
		var usage llm.TokenUsage
		for chunk := range stream {
			if chunk.Err != nil {
				runErr = chunk.Err
				finalReason = ReasonError
				break
			}
			if chunk.Delta.Content != "" {
				content.WriteString(chunk.Delta.Content)
				r.sink.Emit(Event{Kind: EventTurnDelta, RunID: runID, SessionID: sessionID, Turn: turn, Payload: TurnDeltaPayload{Kind: "content", Text: chunk.Delta.Content}})
			}
			if chunk.Delta.Thinking != "" {
				thinking.WriteString(chunk.Delta.Thinking)
				r.sink.Emit(Event{Kind: EventTurnThinking, RunID: runID, SessionID: sessionID, Turn: turn, Payload: TurnDeltaPayload{Kind: "thinking", Text: chunk.Delta.Thinking}})
			}
			if chunk.ToolCall != nil {
				tc := *chunk.ToolCall
				toolCalls = append(toolCalls, tc)
				r.sink.Emit(Event{Kind: EventToolCall, RunID: runID, SessionID: sessionID, Turn: turn, Payload: ToolCallPayload{ID: tc.ID, Name: tc.Name, Arguments: string(tc.Arguments)}})
			}
			if chunk.FinalUsage != nil {
				usage = *chunk.FinalUsage
			}
			if chunk.FinishReason != nil {
				stopReason = *chunk.FinishReason
			}
		}
		// 流中途错误：跳过本轮收尾（无完整轮次语义），交统一出口收束
		if runErr != nil {
			break
		}
		turnLatency := int(time.Since(turnStartedAt).Milliseconds())
		r.usage.AfterTurn(usage)
		// 用上游实测 prompt tokens 回推本地估算的系统性偏差：工具结果（文档正文 /
		// 结构化 JSON）与纯文本的 token 密度可差数倍，单次估算无法覆盖，逐轮 EMA 修正。
		if usage.InputTokens > 0 && estAtTurn > 0 {
			r.tokenScale = updateTokenScale(r.tokenScale, float64(usage.InputTokens)/float64(estAtTurn))
			r.lastMeasuredIn = usage.InputTokens
		}
		if usage.TotalTokens > 0 || usage.InputTokens > 0 || usage.OutputTokens > 0 {
			turnUsages = append(turnUsages, TurnUsage{Turn: turn, Usage: usage, LatencyMs: turnLatency})
		}

		contentAll.WriteString(content.String())
		thinkingAll.WriteString(thinking.String())

		// 文本工具调用兜底：部分兼容端点把调用意图写成正文；只认本次暴露的工具名
		if len(toolCalls) == 0 {
			if textCalls := parseTextToolCalls(content.String(), r.toolDefs); len(textCalls) > 0 {
				toolCalls = textCalls
				for _, tc := range textCalls {
					r.sink.Emit(Event{Kind: EventToolCall, RunID: runID, SessionID: sessionID, Turn: turn, Payload: ToolCallPayload{ID: tc.ID, Name: tc.Name, Arguments: string(tc.Arguments)}})
				}
			}
		}

		// 组装 assistant 消息（含 tool_calls）并回填
		msgs = append(msgs, buildAssistantMessage(content.String(), toolCalls))

		// 轮次结束附带本轮用量，供前端流式 stats（上下文进度条）。必须是 per-turn：
		// usage 在循环里随最新一块流式响应被覆盖，对应当前 turn 实际送入模型的 token；
		// 用累计快照会让 53 个工具轮后 input_tokens 累到百万级，把进度条顶到 100%。
		r.sink.Emit(Event{Kind: EventTurnEnd, RunID: runID, SessionID: sessionID, Turn: turn, Payload: UsagePayload{
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
			CacheRead:    usage.CacheReadTokens,
			CacheWrite:   usage.CacheWriteTokens,
			Total:        usage.TotalTokens,
		}})

		// 截断重发：输出被上限截断时 tool call 参数多半残缺——不执行（必失败或行为危险），
		// 回填错误结果让模型缩短输出后重发。
		if isTruncatedStop(stopReason) && len(toolCalls) > 0 {
			for _, tc := range toolCalls {
				r.emitTruncated(runID, sessionID, turn, tc)
				msgs = append(msgs, llm.ToolMessage(tc.ID, tc.Name, truncatedToolContent(tc.Name)))
			}
			state.ConsecutiveToolFails++
			if state.ConsecutiveToolFails >= r.cfg.StagnationLimit {
				finalReason = ReasonStagnation
				break
			}
			r.saveCheckpoint(runID, sessionID, assistantMessageID, turn, msgs, state, contentAll.String(), thinkingAll.String())
			continue
		}

		// 无工具调用 → 先问 follow-up 缝「还有没有后续输入」，有则把 run 续接下去
		if len(toolCalls) == 0 {
			if follow := r.collect(r.hooks.FollowUp, ctx); len(follow) > 0 {
				msgs = append(msgs, follow...)
				resetStagnation(state)
				continue
			}
			finalReason = ReasonEndTurn
			normalStop = true
			break
		}

		// 执行工具并回填 tool 消息
		results := r.executeTools(ctx, runID, sessionID, turn, toolCalls, state)
		if state.Stagnant {
			finalReason = ReasonStagnation
			break
		}
		if state.ConsecutiveToolFails >= r.cfg.StagnationLimit {
			finalReason = ReasonStagnation
			break
		}
		for _, tr := range results {
			msgs = append(msgs, tr)
		}
		// steering 缝：本轮跑过工具后、下一轮前注入转向消息（用户中途插话）
		if steer := r.collect(r.hooks.Steering, ctx); len(steer) > 0 {
			msgs = append(msgs, steer...)
			resetStagnation(state)
		}
		r.saveCheckpoint(runID, sessionID, assistantMessageID, turn, msgs, state, contentAll.String(), thinkingAll.String())

		// turn 收尾：调整钩子可为下一轮热切换模型 / 工具集（TurnUpdate）
		if up, ok := r.applyTurnUpdate(ctx, &TurnSignal{Turn: turn, ConsecutiveToolFails: state.ConsecutiveToolFails}, model); ok {
			model = up
			r.model = model
		}
		// 优雅停止点：外部感知任务完成后的主动终止（区别于停滞/预算类被动熔断）
		if r.hooks.ShouldStopAfterTurn != nil && r.hooks.ShouldStopAfterTurn(ctx, &TurnSignal{Turn: turn, ConsecutiveToolFails: state.ConsecutiveToolFails}) {
			finalReason = ReasonEndTurn
			normalStop = true
			break
		}
	}
	// 循环跑满 MaxTurns 仍想继续（最后一轮还在发起工具调用）→ 真实原因是 max_turns
	if !normalStop && turnsRun == r.cfg.MaxTurns-startTurn && finalReason == ReasonEndTurn {
		finalReason = ReasonMaxTurns
	}
	// 已消耗 token 即出账：用户取消 / 预算超限 / 停滞 等非 end_turn 终态同样回填用量。
	//
	// 关键：finalUsage 必须是「末轮 per-turn」而非「全程累加」。
	//   - message.input_tokens 落库后被 ContextUsage.historyTokens() 当成「当前上下文占用」展示，
	//     累加值会让一次 5 轮 run 显示成 5× input_tokens（前端 323% 的根因）。
	//   - 每轮 per-turn 明细在 turnUsages 里，会单独写入 token_usage 表用于「总消耗」统计；
	//     RunResult.Usage / RunDone 事件 / message.input_tokens 三个口径统一用末轮 per-turn。
	if len(turnUsages) > 0 {
		last := turnUsages[len(turnUsages)-1].Usage
		if last.InputTokens > 0 || last.OutputTokens > 0 || last.TotalTokens > 0 {
			finalUsage = last
		}
	}

	// 统一出口：先错误事件后终态 RunDone——所有退出路径都从这里收束，事件流有始有终。
	// 错误按类别归一（kind）并附可操作提示，用户看到的不是原始堆栈而是「下一步怎么办」。
	if runErr != nil {
		ae, _ := pkg.As(runErr)
		code, msg := 5000, runErr.Error()
		if ae != nil {
			code = ae.Code
			msg = ae.Message
			if ae.Details != "" {
				msg = msg + ": " + ae.Details
			}
		}
		kind, hint := classifyRunError(runErr)
		if hint != "" {
			msg = msg + "\n" + hint
		}
		r.sink.Emit(Event{Kind: EventError, RunID: runID, SessionID: sessionID, Payload: ErrorPayload{Code: code, Message: msg, Kind: kind}})
	}

	r.sink.Emit(Event{Kind: EventRunDone, RunID: runID, SessionID: sessionID, Payload: RunDonePayload{
		Reason:     string(finalReason),
		StopReason: stopReason,
		MessageID:  assistantMessageID,
		Usage: UsagePayload{
			InputTokens:  finalUsage.InputTokens,
			OutputTokens: finalUsage.OutputTokens,
			CacheRead:    finalUsage.CacheReadTokens,
			Total:        finalUsage.TotalTokens,
		},
	}})

	return RunResult{
		Content:    contentAll.String(),
		Thinking:   thinkingAll.String(),
		StopReason: stopReason,
		Usage:      finalUsage,
		Turns:      turnUsages,
		Reason:     finalReason,
		Err:        runErr,
	}
}

// collect 取注入缝的消息；缝未配置或返回空时返回 nil（零开销、不改变既有行为）。
func (r *Runner) collect(inj Injector, ctx context.Context) []*llm.Message {
	if inj == nil {
		return nil
	}
	return inj(ctx)
}

// resetStagnation 注入新输入后重置停滞计数：新指令不是「重复调用」，不该累计熔断。
func resetStagnation(state *RunState) {
	if state == nil {
		return
	}
	state.ConsecutiveToolFails = 0
	state.SameKeyCount = 0
	state.LastToolKey = ""
}

// isTruncatedStop 是否被输出长度上限截断（OpenAI: length / Anthropic、Ollama: max_tokens）。
func isTruncatedStop(reason string) bool {
	return reason == "max_tokens" || reason == "length"
}

// truncatedToolContent 截断回执：明确「未执行 + 参数可能残缺 + 缩短后重发」，模型据此自愈。
func truncatedToolContent(name string) string {
	bs, err := json.Marshal(map[string]any{
		"error":   "output truncated at max_tokens",
		"tool":    name,
		"hint":    "上一条回复被输出长度上限截断，该工具调用未执行，参数可能残缺。请缩短本轮输出（少说废话、分批调用）后重新发起该调用。",
		"refused": true,
	})
	if err != nil {
		return `{"error":"output truncated at max_tokens"}`
	}
	return string(bs)
}

// emitTruncated 发截断回执事件：前端工具块显示为错误，用户能看懂「模型输出被截断、未执行」。
func (r *Runner) emitTruncated(runID, sessionID string, turn int, call llm.NormalizedToolCall) {
	r.sink.Emit(Event{Kind: EventToolResult, RunID: runID, SessionID: sessionID, Turn: turn, Payload: ToolResultPayload{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    truncatedToolContent(call.Name),
		Err:        "output truncated at max_tokens",
	}})
}

// buildAssistantMessage 组装 assistant 消息：正文 + 归一化 tool_calls。
func buildAssistantMessage(content string, calls []llm.NormalizedToolCall) *llm.Message {
	msg := &llm.Message{Role: llm.RoleAssistant, Content: content}
	if len(calls) > 0 {
		ts := make([]llm.ToolCall, 0, len(calls))
		for _, c := range calls {
			ts = append(ts, llm.ToolCall{
				ID:   c.ID,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      c.Name,
					Arguments: string(c.Arguments),
				},
			})
		}
		msg.ToolCalls = ts
	}
	return msg
}

// executeTools 执行本轮所有工具调用：
//   - 同轮全部为只读工具且并发数 > 1 时并行执行（结果按调用顺序回填）；
//   - 其余严格串行（写工具间无并发，避免互相覆盖）；
//   - 每次执行包 panic 恢复：单工具 panic 不拖垮整个 run。
func (r *Runner) executeTools(ctx context.Context, runID, sessionID string, turn int, calls []llm.NormalizedToolCall, state *RunState) []*llm.Message {
	// 标记护栏链生效：工具内部据此跳过自有审批兜底（单层闸门，杜绝重复询问）
	ctx = tool.WithGuardChain(ctx)
	if len(calls) > 1 && r.cfg.ToolParallelism > 1 && r.allReadonly(calls) {
		// 只读无副作用，停滞检测略过（并行下 state 不共享）；结果按序回填
		out := make([]*llm.Message, len(calls))
		sem := make(chan struct{}, r.cfg.ToolParallelism)
		var wg sync.WaitGroup
		for i, call := range calls {
			sem <- struct{}{}
			wg.Add(1)
			go func(i int, call llm.NormalizedToolCall) {
				defer wg.Done()
				defer func() { <-sem }()
				out[i] = r.execOne(ctx, runID, sessionID, turn, call, nil)
			}(i, call)
		}
		wg.Wait()
		return out
	}

	out := make([]*llm.Message, 0, len(calls))
	for _, call := range calls {
		out = append(out, r.execOne(ctx, runID, sessionID, turn, call, state))
	}
	return out
}

// allReadonly 该轮全部工具都存在且声明只读（ToolMeta 声明优先，RiskLevel 兜底）。
func (r *Runner) allReadonly(calls []llm.NormalizedToolCall) bool {
	names := make([]string, 0, len(calls))
	for _, c := range calls {
		names = append(names, c.Name)
	}
	return r.tools.AllReadOnly(names)
}

// toolExposed 该工具是否在本次 run 暴露给 LLM 的 defs 中（执行侧收紧）。
// Skill 白名单 / Agent 过滤只在 defs 层生效——若模型幻觉出未暴露但已注册的工具名，
// 注册中心仍能查到并执行，这是执行侧漏洞；defs 非空时严格校验，
// defs 为空（单测/无工具场景）视为不过滤以保持兼容。
func (r *Runner) toolExposed(name string) bool {
	if len(r.toolDefs) == 0 {
		return true
	}
	for _, d := range r.toolDefs {
		if d.Name == name {
			return true
		}
	}
	return false
}

// execOne 执行单次工具调用：暴露校验 → 查找 → 参数校验 → 目录信任 → 工具策略门 → 停滞检测 → 执行（panic 恢复）。
func (r *Runner) execOne(ctx context.Context, runID, sessionID string, turn int, call llm.NormalizedToolCall, state *RunState) *llm.Message {
	if state != nil {
		state.ToolCallCount++
	}

	// 执行侧策略收紧：未暴露的工具直接 Refused（不计失败熔断，模型可换路）
	if !r.toolExposed(call.Name) {
		return r.refused(runID, sessionID, turn, call, "tool not exposed in this run")
	}

	t, ok := r.tools.Get(call.Name)
	if !ok {
		if state != nil {
			state.ConsecutiveToolFails++
		}
		return llm.ToolMessage(call.ID, call.Name, "tool not found: "+call.Name)
	}
	if err := tool.ValidateArgs(t.Schema().Parameters, call.Arguments); err != nil {
		if state != nil {
			state.ConsecutiveToolFails++
		}
		return llm.ToolMessage(call.ID, call.Name, "invalid args: "+err.Error())
	}

	// 提示注入防护：参数里嵌着伪工具调用标记（多来自网页/文件内容的诱导文本）→ 拒绝执行。
	// 拒绝走 refused（结构化回执），模型与用户都能看到原因。
	if HasNestedToolCallMarker(string(call.Arguments)) {
		return r.refused(runID, sessionID, turn, call, "args contain nested tool-call markers (possible prompt injection)")
	}

	// 目录信任闸门：先于工具策略门——目录都没授权，不必再问命令白名单
	if msg := r.trustTool(ctx, runID, sessionID, turn, call); msg != nil {
		return msg
	}

	// 工具策略门：deny / ask 前置（默认未启用，不改变既有行为）
	if msg := r.gateTool(ctx, runID, sessionID, turn, call, t); msg != nil {
		return msg
	}

	// 停滞检测：同名同参连续出现（仅串行路径共享 state）
	if state != nil {
		key := call.Name + "|" + string(call.Arguments)
		if key == state.LastToolKey {
			state.SameKeyCount++
		} else {
			state.SameKeyCount = 1
			state.LastToolKey = key
		}
		if state.SameKeyCount >= r.cfg.StagnationLimit {
			state.Stagnant = true
			return llm.ToolMessage(call.ID, call.Name, "stagnation guard: repeated identical tool call")
		}
	}

	// 循环护栏 + 工具预算：与停滞检测互补。
	// 停滞只认「连续重复」，抓不到 A→B→A→B 这类交替循环，故再叠一层全 run 的 name+args 计数；
	// 预算则在工具执行前拦截，避免一轮内几十次调用要等到下一轮轮首才停。
	// 命中走 refused（结构化回执）而非错误——模型看得到原因，可自行换路而不是硬失败。
	if r.cfg.LoopLimit > 0 || r.cfg.MaxToolCalls > 0 {
		key := call.Name + "|" + string(call.Arguments)
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
			return r.refused(runID, sessionID, turn, call,
				fmt.Sprintf("loop guard: identical call repeated %d times (limit %d)", hits, r.cfg.LoopLimit))
		}
		if r.cfg.MaxToolCalls > 0 && used > r.cfg.MaxToolCalls {
			return r.refused(runID, sessionID, turn, call,
				fmt.Sprintf("tool budget exhausted: %d/%d calls used", used-1, r.cfg.MaxToolCalls))
		}
	}

	// 幂等恢复（Resume 场景）：同名同参已完成的成功调用直接复用结果，不重放副作用
	if rec, ok := r.stepHit(call); ok {
		r.sink.Emit(Event{Kind: EventToolResult, RunID: runID, SessionID: sessionID, Turn: turn, Payload: ToolResultPayload{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    rec.Content,
			DurationMs: 0,
			Meta:       map[string]string{"reused": "true"},
		}})
		return llm.ToolMessage(call.ID, call.Name, rec.Content)
	}

	r.sink.Emit(Event{Kind: EventToolStart, RunID: runID, SessionID: sessionID, Turn: turn, Payload: ToolCallPayload{ID: call.ID, Name: call.Name, Arguments: string(call.Arguments)}})

	// 单工具元数据覆盖：声明超时 / 结果截断优先于全局默认
	meta := tool.MetaOf(t)
	timeout := r.cfg.ToolCallTimeout
	if meta.TimeoutSec > 0 {
		timeout = time.Duration(meta.TimeoutSec) * time.Second
	}
	resultLimit := r.cfg.MaxToolResultLen
	if meta.MaxResultChars > 0 {
		resultLimit = meta.MaxResultChars
	}

	start := time.Now()
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	result := r.execWithRecover(execCtx, t, call.Arguments)
	cancel()
	durationMs := time.Since(start).Milliseconds()

	// 工具后处理缝：逐字段覆盖结果（脱敏 / 富化），先于事件与幂等记忆
	if r.hooks.AfterToolCall != nil {
		r.hooks.AfterToolCall(ctx, call.Name, call.Arguments, &result)
	}

	content := truncate(result.Content, resultLimit)
	errMsg := ""
	if result.Err != nil {
		errMsg = result.Err.Error()
		if state != nil {
			state.ConsecutiveToolFails++
		}
	} else if state != nil {
		state.ConsecutiveToolFails = 0
	}
	r.sink.Emit(Event{Kind: EventToolResult, RunID: runID, SessionID: sessionID, Turn: turn, Payload: ToolResultPayload{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    content,
		Err:        errMsg,
		DurationMs: durationMs,
		Meta:       result.Meta,
		Data:       result.Data,
		Refused:    result.Refused,
	}})

	// 幂等记忆：仅记成功调用（失败/拒绝不记，Resume 时重试）
	if result.Err == nil && !result.Refused {
		r.stepRecord(call, StepRecord{Content: content, DurationMs: durationMs})
	}

	toolContent := content
	if errMsg != "" {
		toolContent = "error: " + errMsg + "\n" + content
	}
	return llm.ToolMessage(call.ID, call.Name, toolContent)
}

// stepKey 工具调用幂等键：name + 原始参数（模型同参重试/Resume 重发时命中）。
func stepKey(call llm.NormalizedToolCall) string {
	return "tool:" + call.Name + "|" + string(call.Arguments)
}

func (r *Runner) stepHit(call llm.NormalizedToolCall) (StepRecord, bool) {
	r.stepsMu.Lock()
	defer r.stepsMu.Unlock()
	rec, ok := r.steps[stepKey(call)]
	return rec, ok
}

func (r *Runner) stepRecord(call llm.NormalizedToolCall, rec StepRecord) {
	r.stepsMu.Lock()
	defer r.stepsMu.Unlock()
	if r.steps == nil {
		r.steps = map[string]StepRecord{}
	}
	r.steps[stepKey(call)] = rec
}

// execWithRecover 包 panic：工具 panic 转错误结果（值语义，模型可见反馈），run 不崩。
func (r *Runner) execWithRecover(ctx context.Context, t tool.Tool, args json.RawMessage) (res tool.ToolResult) {
	defer func() {
		if rec := recover(); rec != nil {
			res = tool.ToolResult{Err: fmt.Errorf("tool panic: %v", rec)}
		}
	}()
	return t.Execute(ctx, args)
}

// trustTool 目录信任闸门；未启用（pathTrust nil）返回 nil。
// 拒绝一律走 refused（结构化回执、不计失败熔断），与策略门拒绝同构。
func (r *Runner) trustTool(ctx context.Context, runID, sessionID string, turn int, call llm.NormalizedToolCall) *llm.Message {
	if r.hooks.BeforeToolCall == nil {
		return nil
	}
	if ok, reason := r.hooks.BeforeToolCall(ctx, call.Name, call.Arguments); !ok {
		return r.refused(runID, sessionID, turn, call, reason)
	}
	return nil
}

// gateTool 工具策略门——统一护栏链的单层裁决点。
// 实现 RiskClassifier 的工具（exec / run_skill_script）按 per-call 命令级风险裁决；
// 其余工具显式 Allow 即放行、ask 才按静态风险问。拒绝一律 refused（结构化回执），
// 不计失败熔断，模型可据此换方案自愈。放行后 ctx 标记 GuardChain（单层闸门）。
func (r *Runner) gateTool(ctx context.Context, runID, sessionID string, turn int, call llm.NormalizedToolCall, t tool.Tool) *llm.Message {
	if r.hooks.ToolGate == nil {
		return nil
	}
	desc := t.Name() + "(" + string(call.Arguments) + ")"
	risk := ""
	cmdLevel := false // 工具给出了 per-call 命令级裁决
	if rc, ok := t.(tool.RiskClassifier); ok {
		if d, rr := rc.ClassifyArgs(call.Arguments); d != "" {
			desc, risk, cmdLevel = d, rr, true
		}
	}
	switch r.hooks.ToolGate.Decide(call.Name, t.RiskLevel()) {
	case tool.DecisionDeny:
		return r.refused(runID, sessionID, turn, call, "denied by policy")
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
			risk = gateApprovalRisk(t.RiskLevel())
		}
	}
	if risk == "" || r.hooks.Approver == nil {
		return nil
	}
	if !r.hooks.Approver(ctx, desc, risk) {
		return r.refused(runID, sessionID, turn, call, "denied by user")
	}
	return nil
}

// refused 发结构化拒绝结果事件并返回 tool 消息（Refused 语义；不推进停滞计数）。
func (r *Runner) refused(runID, sessionID string, turn int, call llm.NormalizedToolCall, reason string) *llm.Message {
	content := refusedContent(call.Name, reason)
	r.sink.Emit(Event{Kind: EventToolResult, RunID: runID, SessionID: sessionID, Turn: turn, Payload: ToolResultPayload{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    content,
		Refused:    true,
	}})
	return llm.ToolMessage(call.ID, call.Name, content)
}

// refusedContent 审批拒绝 → 结构化 JSON 回执：模型据此自适应（换工具 / 调整参数 / 放弃）。
func refusedContent(name, reason string) string {
	bs, err := json.Marshal(map[string]any{
		"refused": true,
		"tool":    name,
		"reason":  reason,
		"hint":    "该工具调用被用户拒绝。不要原样重试；请调整方案、改用其他工具，或向用户说明并给出替代建议。",
	})
	if err != nil {
		return `{"refused":true,"reason":"` + reason + `"}`
	}
	return string(bs)
}

// gateApprovalRisk 工具风险 → 审批风险语义（与 ApprovalService 前端展示对齐）。
func gateApprovalRisk(risk tool.RiskLevel) string {
	if risk == tool.RiskDestructive || risk == tool.RiskExec {
		return tool.RiskApprovalIrrev
	}
	return tool.RiskApprovalNeeds
}

// saveCheckpoint 每轮工具执行后落一行检查点；失败仅发事件，不阻断 run。
func (r *Runner) saveCheckpoint(runID, sessionID, assistantMessageID string, turn int, msgs []*llm.Message, state *RunState, content, thinking string) {
	if r.checkpoints == nil {
		return
	}
	cp := &Checkpoint{
		RunID:          runID,
		SessionID:      sessionID,
		Turn:           turn,
		Messages:       msgs,
		State:          *state,
		AssistantMsgID: assistantMessageID,
		Content:        content,
		Thinking:       thinking,
		Usage:          r.usage.Snapshot(),
	}
	r.stepsMu.Lock()
	if len(r.steps) > 0 {
		cp.StepRecords = make(map[string]StepRecord, len(r.steps))
		for k, v := range r.steps {
			cp.StepRecords[k] = v
		}
	}
	r.stepsMu.Unlock()
	if err := r.checkpoints.Append(cp); err != nil {
		r.sink.Emit(Event{Kind: EventError, RunID: runID, SessionID: sessionID, Turn: turn, Payload: ErrorPayload{Code: 5005, Message: "检查点保存失败: " + err.Error()}})
		return
	}
	_ = r.checkpoints.Cleanup(sessionID, checkpointKeepRuns)
	r.sink.Emit(Event{Kind: EventCheckpoint, RunID: runID, SessionID: sessionID, Turn: turn})
}

// applyMiddlewares 顺序执行 BeforeTurn。
func (r *Runner) applyMiddlewares(msgs []*llm.Message) []*llm.Message {
	for _, m := range r.middlewares {
		msgs = m.BeforeTurn(msgs)
	}
	return msgs
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n... (truncated)"
}

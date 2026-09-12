package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"WorkBaby/internal/capability"
	"WorkBaby/internal/domain"
	"WorkBaby/internal/event"
	"WorkBaby/internal/harness"
	"WorkBaby/internal/llm"
	"WorkBaby/internal/llm/registry"
	"WorkBaby/internal/memory"
	"WorkBaby/internal/pkg"
	"WorkBaby/internal/repo"
	"WorkBaby/internal/tool"
	"WorkBaby/internal/tool/planmode"
)

// FileStore 附件读取能力（ChatService 只用于把图片附件转成 LLM 多模态 part）。
type FileStore interface {
	Get(ctx context.Context, id string) (domain.FileRESP, error)
	ReadDataURL(ctx context.Context, id string) (string, error)
}

// ChatService 会话 + 消息编排；接入 harness.Runner 调用真实 LLM。
type ChatService struct {
	sessions    *repo.ChatSessionRepo
	messages    *repo.MessageRepo
	provRepo    *repo.AiProviderRepo
	setRepo     *repo.SystemSettingRepo
	usages      *repo.TokenUsageRepo // token 明细（仪表盘三线图的唯一数据源）
	bus         *event.Bus
	reg         *registry.Registry
	tools       *ToolService
	mem         *memory.Service                     // 上下文占用统计（/context 分段）读取长期记忆
	trust       *TrustService                       // 目录信任；nil = 关闭
	planStore   *planmode.Store                     // 计划模式状态；nil = 不启用
	locksMu     sync.Mutex                          // 保护 locks
	locks       map[string]*sync.Mutex              // 会话级互斥：同会话的建流 / 插话 / 后台 run 串行，跨会话并行
	runs        *runRegistry                        // 活动 run 注册中心（按 sessionID → cancel）；用于前端「停止」按钮
	checkpoints harness.CheckpointStore             // 检查点存储（SQL 默认；nil = 关闭）
	events      *event.RunEventLog                  // run 事件日志：分配序号 + 缓存，供 SSE 断线重放
	runRec      *repo.RunRecordRepo                 // 运行历史索引；nil = 不记录
	execs       *harness.ExecutionRegistry          // 执行平面
	blocks      *repo.MessageBlockRepo              // 消息块持久化；nil = 不落块
	files       FileStore                           // 受管文件读取（消息附件 → 多模态 part）；nil = 附件降级为文本
	changeSvc   *FileChangeService                  // 本 run 文件变更查询（完成度证据核对）
	approval    *ApprovalService                    // 工具策略门 ask 决策的人工审批；nil = 策略门不启用
	steers      *steerQueue                         // run 中用户新消息的注入队列（steering / follow-up）
	seqMu       sync.Mutex                          // 保护 seqs
	seqs        map[string]int64                    // 会话消息序号分配水位（工具消息与注入消息统一分配，防撞号）
	dataHome    string                              // 数据根（paths.Home）；目录策略默认根由此派生
	caps        *capability.Registry                // 能力注册表：上下文装配 / 工具暴露 / run 后沉淀三条通道
	skillSync   func(context.Context, string) error // 技能目录同步钩子（run 前按会话工作区叠加）；nil = 不启用
	tempClear   func(runID string)                  // run 级临时态清理钩子（三层 State 的 temp 作用域）；nil = 不启用
}

// WithTempStateClearer 注入 run 级临时态清理钩子（run 结束后调用一次）。
func (s *ChatService) WithTempStateClearer(clear func(runID string)) *ChatService {
	s.tempClear = clear
	return s
}

// memoryCaptureTimeout run 后沉淀（记忆形成等）的独立超时。
const memoryCaptureTimeout = 30 * time.Second

// lockSession 取会话级互斥锁；返回解锁函数。
// 关键区只覆盖「占位消息落库 + 活动 run 登记」：同一会话的并发发送 / 插话在此串行，
// 不同会话互不阻塞（全局单锁会让多会话同时对话排队）。
func (s *ChatService) lockSession(sessionID string) func() {
	s.locksMu.Lock()
	if s.locks == nil {
		s.locks = make(map[string]*sync.Mutex)
	}
	l, ok := s.locks[sessionID]
	if !ok {
		l = &sync.Mutex{}
		s.locks[sessionID] = l
	}
	s.locksMu.Unlock()
	l.Lock()
	return l.Unlock
}

// WithCapabilities 接入能力注册表；未接入时上下文装配与沉淀均为空操作。

func NewChatService(sessions *repo.ChatSessionRepo, messages *repo.MessageRepo, provRepo *repo.AiProviderRepo, setRepo *repo.SystemSettingRepo, usages *repo.TokenUsageRepo, bus *event.Bus, reg *registry.Registry, tools *ToolService, mem *memory.Service) *ChatService {
	return &ChatService{
		sessions: sessions, messages: messages, provRepo: provRepo, setRepo: setRepo, usages: usages,
		bus: bus, reg: reg, tools: tools, mem: mem, runs: newRunRegistry(),
		steers: newSteerQueue(), seqs: map[string]int64{},
	}
}

// CancelStream 中断指定 session 正在跑的 run（前端「停止」按钮）。
//
// 幂等：无 run 或 sessionID 为空直接 nil；harness 收到 ctx 取消后 emit ReasonCancelled → chat:done。

func (s *ChatService) SendStream(ctx context.Context, sessionID, content string, fileIDs []string, params harness.RequestParams) (*domain.SendStreamResult, error) {
	unlock := s.lockSession(sessionID)
	defer unlock()

	ses, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	// 兜底：历史会话可能缺 provider/model（首次启动升级 / 旧数据），运行时补一个默认 Provider
	if ses.ProviderID == "" || ses.Model == "" {
		if pid, model, ok := s.resolveDefaultProviderModel(ctx, ses.Model); ok {
			ses.ProviderID = pid
			ses.Model = model
			_ = s.sessions.Update(ctx, ses)
		} else {
			return nil, pkg.Wrap(domain.ErrSessionInvalid.Code, domain.ErrSessionInvalid.Message, domain.ErrSessionInvalid)
		}
	}

	// v1：chat 固定走 default Agent；前端 Agent 选择器待 P3 接入。
	// 首条用户消息自动命名（无历史消息可推导时），让会话在列表里可辨认。
	firstTurn := ses.MessageCount == 0
	atts := s.resolveAttachments(ctx, fileIDs)
	agentName := SessionAgent(ses)
	if agentName == "" {
		agentName = defaultAgentName
	}
	ids, err := s.prepareRun(ctx, ses, content, atts, harness.ScopeChatTurn, agentName)
	if err != nil {
		return nil, err
	}
	if firstTurn {
		// 只有附件没有文字时（纯图片提问）用附件名兜底，避免会话标题为空
		titleSrc := content
		if strings.TrimSpace(titleSrc) == "" && len(atts) > 0 {
			titleSrc = atts[0].Name
		}
		s.autoTitleSession(ctx, ses, titleSrc)
	}

	// 异步跑；用可取消 ctx（前端「停止」→ CancelStream 触发）
	runCtx, cancel := context.WithCancel(context.Background())
	s.runs.set(ses.ID, ids.RunID, cancel)
	go func() {
		defer s.runs.delete(ses.ID)
		s.runLLM(runCtx, ses, ids.RunID, ids.AssistantMsgID, content, params, harness.Agent(agentName), false)
	}()

	return &domain.SendStreamResult{
		RunID:          ids.RunID,
		SessionID:      sessionID,
		UserMsgID:      ids.UserMsgID,
		AssistantMsgID: ids.AssistantMsgID,
	}, nil
}

// defaultAgentName chat 主入口的默认 Agent。

func (s *ChatService) runLLM(parentCtx context.Context, ses *domain.ChatSessionDO, runID, assistantMsgID, userInput string, params harness.RequestParams, def harness.Definition, resume bool) {
	s.startRunRecord(parentCtx, runID, ses)
	wall := 10 * time.Minute
	if def.Budget.MaxWallTime > 0 {
		wall = def.Budget.MaxWallTime
	}
	ctx, cancel := context.WithTimeout(parentCtx, wall)
	defer cancel()
	// run 结束（含取消/失败）后队列作废：注入消息只属于本次 run
	defer s.steers.clear(ses.ID)
	s.executeAgent(ctx, ses, runID, assistantMsgID, userInput, params, def, resume)
}

// ResumeRun 从检查点续跑：中断/崩溃后以同一 runID 恢复，前端重挂 SSE 续渲染。
// 已完成工具调用经 StepRecords 复用，不重放副作用；审批/补充输入会重新发起。

func (s *ChatService) executeAgent(ctx context.Context, ses *domain.ChatSessionDO, runID, assistantMsgID, userInput string, params harness.RequestParams, def harness.Definition, resume bool) harness.RunResult {
	// run 前按会话工作区叠加技能目录（未变化时零开销）；失败不阻断 run
	if s.skillSync != nil {
		if err := s.skillSync(ctx, ses.WorkspacePath); err != nil {
			pkg.L.Warn("sync workspace skills failed", "sessionID", ses.ID, "err", err.Error())
		}
	}
	prov, err := s.reg.Get(ses.ProviderID)
	if err != nil {
		s.failRun(ctx, runID, ses.ID, assistantMsgID, err)
		return harness.RunResult{Reason: harness.ReasonError, Err: err}
	}

	// 拉历史 messages → 组装给 LLM（含工具调用上下文）
	hists, err := s.messages.ListBySession(ctx, ses.ID, 0, 0)
	if err != nil {
		s.failRun(ctx, runID, ses.ID, assistantMsgID, err)
		return harness.RunResult{Reason: harness.ReasonError, Err: err}
	}
	llmMsgs, err := s.toLLMMessagesWithVision(ctx, hists, s.providerVision(ctx, ses.ProviderID))
	if err != nil {
		s.failRun(ctx, runID, ses.ID, assistantMsgID, err)
		return harness.RunResult{Reason: harness.ReasonError, Err: err}
	}

	// system 装配（真实请求口径，与 ContextUsage 透视同源——见 buildSystem）
	sys, runState := s.buildSystem(ctx, ses, runID, userInput, def)
	activeSkillTools := runState.SkillTools
	if sys != nil {
		llmMsgs = append([]*llm.Message{sys}, llmMsgs...)
	}

	// 终态事件延迟到 assistant 消息落库后再发：前端收 chat:done 会立刻拉权威快照，
	// 早于落库会让「空 content」覆盖已渲染内容，过程块一并丢失。
	// 事件 → chat:* / 消息块 / tool 消息的映射收敛在 runEventMapper（双通道汇聚点）。
	mapper := newRunEventMapper(s, ctx, ses, runID, assistantMsgID, runState)
	defer func() {
		if mapper.doneEvent != nil {
			s.emit(runID, ses.ID, "chat:done", mapper.doneEvent)
		}
	}()
	sink := harness.FuncSink(mapper.handle)
	runStart := mapper.runStart

	// 工具两级过滤：Skill 白名单（命中 skill 时）→ Agent 工具策略
	toolDefs := s.tools.LLMDefinitionsFiltered(ctx, activeSkillTools)
	toolDefs = def.FilterTools(toolDefs)
	cfg := harness.DefaultConfig()
	def.Budget.Apply(&cfg)
	// 单次 run 的 token 花费上限（全局设置；0 = 不限）。与 Agent 内置预算取更严者——
	// 两者一个管「成本」，一个管「任务规模」，谁更紧谁生效。
	if maxRun := int(s.settingFloat(ctx, domain.SettingKeyChatMaxRunTokens, 0)); maxRun > 0 {
		if cfg.MaxTokens <= 0 || maxRun < cfg.MaxTokens {
			cfg.MaxTokens = maxRun
		}
	}
	// 工具定义预算：工具 schema 同样吃 prompt token，MCP 挂几十个工具时会挤掉历史预算。
	// 超限部分不再整体塞给模型也不静默丢弃——折叠为 tool_search 按需激活（deferred tools）。
	var deferredDefs []llm.ToolDefinition
	if trimmed, droppedNames := harness.TrimToolDefs(toolDefs, cfg.ContextBudget*harness.ToolDefBudgetPercent/100); len(droppedNames) > 0 {
		droppedSet := make(map[string]struct{}, len(droppedNames))
		for _, n := range droppedNames {
			droppedSet[n] = struct{}{}
		}
		deferredDefs = make([]llm.ToolDefinition, 0, len(droppedNames))
		for _, d := range toolDefs {
			if _, dropped := droppedSet[d.Name]; dropped {
				deferredDefs = append(deferredDefs, d)
			}
		}
		toolDefs = append(trimmed, harness.ToolSearchDef())
		pkg.L.Warn("tool definitions deferred behind tool_search", "runID", runID,
			"deferred", strings.Join(droppedNames, ","))
	}
	// 上下文预算按「上下文窗口 × 压缩比例」重算：Agent 内置 120k 是静态值，
	// 小窗口模型会撑爆、大窗口模型又过早压缩，交给 provider/全局设置决定。
	provRow := s.providerDO(ctx, ses.ProviderID)
	window := s.modelContextWindow(ctx, ses.ProviderID, ses.Model)
	if budget := s.contextBudget(ctx, window, provRow); budget > 0 {
		cfg.ContextBudget = budget
	}
	// 上下文压缩升级：Auto（watermark + LLM 六段交接摘要）优先，失败降级 Micro。
	// 压缩的可见事件与边界证据由 runner 统一发（EventCompressed），此处不再重复上报。
	auto := harness.NewAutoCompressor(prov, ses.Model)
	// 压缩摘要也计量：Turn 记 -1 与主循环轮次区分（不进「按轮次」图表，但进总消耗）
	auto.OnUsage = func(u llm.TokenUsage) {
		s.persistUsageRow(ctx, ses, runID, assistantMsgID, -1, u)
	}
	r := harness.NewRunner(prov, sink, cfg).
		WithCompressor(auto).
		WithRequestParams(params).
		WithProviderParams(s.providerParams(ctx, ses.ProviderID)).
		WithDefaults(s.defaults(ctx)).
		WithTools(s.tools.Registry(), toolDefs).
		WithCheckpointStore(s.checkpoints).
		WithExecutionRegistry(s.execs)
	if gate := s.toolGate(ctx, ses.PermissionMode); gate != nil {
		r = r.WithToolGate(gate, s.approval.Approve)
	}
	// 计划模式 Guard + 目录信任组合闸门：挂在工具策略门之前——
	// 计划模式先拦（非只读一律拒绝），目录都没授权也不必再问命令白名单
	if trustHook := s.planTrustHook(); trustHook != nil {
		r = r.WithPathTrust(trustHook)
	}
	// 注入缝：同一队列供两条缝消费——跑工具中途插话 / 说完后自动续接
	drain := func(context.Context) []*llm.Message { return s.steers.drain(ses.ID) }
	r = r.WithSteering(drain).WithFollowUp(drain)
	// 自动降级：LLM 建流失败 → 切到 chat.fallback_model 重试本轮
	r = r.WithTurnAdjuster(s.turnAdjuster(ctx))
	// 子 Agent 委派消耗单独落库：委派可达 12 轮 + 几十次工具调用，
	// 不落库会让 token_usages 系统性漏计。Turn 记 -1（同压缩摘要口径），
	// Source 记 delegate 并带上子 Agent 名，保证消耗可按委派归因而非混进父对话成本。
	r.OnDelegateUsage = func(agent string, turns []harness.TurnUsage) {
		for _, t := range turns {
			s.persistUsageRowFrom(ctx, ses, runID, assistantMsgID, -1, domain.UsageSourceDelegate, agent, t.Usage)
		}
	}
	// 折叠激活：被预算裁掉的工具经 tool_search 按需暴露（激活后下一轮起可调用）
	if len(deferredDefs) > 0 {
		r = r.WithDeferredTools(deferredDefs)
	}
	// 补充输入能力注入 ctx：request_input 工具暂停 run 问用户
	if s.approval != nil {
		ctx = tool.WithInputRequester(ctx, s.approval)
	}

	pkg.L.Info("chat run start",
		"runID", runID, "sessionID", ses.ID, "providerID", ses.ProviderID, "model", ses.Model,
		"history", len(llmMsgs), "tools", len(toolDefs), "skill", strings.Join(activeSkillTools, ","),
		"resume", resume)
	var res harness.RunResult
	if resume {
		// 续跑：前端按同一 runID 挂 SSE；Resume 不重发 RunStart，这里补 stream.start
		s.emit(runID, ses.ID, "chat:stream.start", map[string]any{"model": ses.Model, "resumed": true})
		res = r.Resume(ctx, runID, ses.ID, assistantMsgID, ses.Model)
	} else {
		res = r.RunMessages(ctx, runID, ses.ID, assistantMsgID, ses.Model, llmMsgs)
	}
	elapsed := time.Since(runStart).Milliseconds()

	s.persistUsage(ctx, ses, runID, assistantMsgID, res)

	if res.Err != nil {
		pkg.L.Error("chat run failed",
			"runID", runID, "sessionID", ses.ID, "reason", string(res.Reason),
			"latencyMs", elapsed, "err", res.Err.Error())
	} else {
		pkg.L.Info("chat run done",
			"runID", runID, "sessionID", ses.ID, "reason", string(res.Reason),
			"stopReason", res.StopReason, "turns", len(res.Turns), "toolCalls", len(mapper.toolCalls),
			"latencyMs", elapsed,
			"input", res.Usage.InputTokens, "output", res.Usage.OutputTokens,
			"cacheRead", res.Usage.CacheReadTokens, "total", res.Usage.TotalTokens)
	}
	// 运行历史落库：事件明细走 JSONL，这里只回填可列表 / 可跳转回放的索引。
	s.finishRunRecord(ctx, runID, res)

	nowMs := time.Now().UnixMilli()
	// 终止原因统一口径：harness 枚举 → 领域 stop_reason，
	// max_turns / stagnation / token_budget 不再被硬写成 completed，前端可差异化收尾。
	stopReason := domain.MapHarnessReason(string(res.Reason))
	// 权限来源回滚：以 error/cancelled 收尾的 run 不留下本次扩出的免审授权
	// （未验证的工作不保留「本会话允许」），成功/主动收尾的 run 授权保留。
	if s.approval != nil && (res.Reason == harness.ReasonCancelled || res.Reason == harness.ReasonError) {
		s.approval.RollbackRun(runID)
	}
	toolCallsJSON := ""
	if len(mapper.toolCalls) > 0 {
		if bs, err := json.Marshal(mapper.toolCalls); err == nil {
			toolCallsJSON = string(bs)
		}
	}
	// 消息级费用估算：按累计用量 × 单价（未配置单价为空串，前端不显示费用）。
	costUSD := s.modelPricing(ctx, ses.Model).
		CostUSD(int64(res.Accumulated.InputTokens), int64(res.Accumulated.OutputTokens), int64(res.Accumulated.CacheReadTokens))
	costStr := ""
	if costUSD > 0 {
		costStr = fmt.Sprintf("$%.4f", costUSD)
	}
	if res.Err != nil {
		s.finishExec(runID, harness.StateFailed)
		_ = s.messages.UpdateStatus(ctx, assistantMsgID, map[string]any{
			"content":       res.Content,
			"thinking":      res.Thinking,
			"tool_calls":    toolCallsJSON,
			"status":        domain.MessageStatusFailed,
			"stop_reason":   stopReason,
			"output_tokens": res.Usage.OutputTokens,
			"cost":          costStr,
			"updated_at":    nowMs,
		})
		return res
	}
	s.finishExec(runID, execState(res.Reason))
	_ = s.messages.UpdateStatus(ctx, assistantMsgID, map[string]any{
		"content":       res.Content,
		"thinking":      res.Thinking,
		"tool_calls":    toolCallsJSON,
		"status":        domain.MessageStatusCompleted,
		"stop_reason":   stopReason,
		"input_tokens":  res.Usage.InputTokens,
		"output_tokens": res.Usage.OutputTokens,
		"cache_read":    res.Usage.CacheReadTokens,
		"total_tokens":  res.Usage.TotalTokens,
		"cost":          costStr,
		"latency_ms":    nowMs - ses.LastMessageAt,
		"updated_at":    nowMs,
	})

	// 反幻觉核验：声称已产出文件，但本 run 没有对应的 file_changes（拒绝/失败的不算）
	// → 强提示揭露。证据级别从「整轮是否有写工具」升级为「声明路径与本 run 产物的精确比对」——
	// 只跑 exec / 搜索后泛指「已生成 report」也算幻觉。
	if claimsArtifact(res.Content) && s.changeSvc != nil {
		changeRows, _ := s.changeSvc.ListByRun(ctx, runID, 200)
		changes := make([]changeEvidence, 0, len(changeRows))
		for _, r := range changeRows {
			changes = append(changes, changeEvidence{Path: r.RelPath})
		}
		if !evidenceForClaim(claimedPaths(res.Content), mapper.toolCalls, changes) {
			pkg.L.Warn("unbacked artifact claim (no matching file_changes in run)",
				"runID", runID, "sessionID", ses.ID, "model", ses.Model,
				"claimed", fmt.Sprint(claimedPaths(res.Content)), "changes", len(changeRows))
			s.emit(runID, ses.ID, "chat:warn", map[string]any{
				"kind":    "unbacked_claim",
				"message": "本条回复声称已产出文件，但本 run 没有任何对应的写文件变更记录——相关文件并不存在，请让模型实际写入后再确认。",
			})
		}
	}

	// run 后沉淀（记忆形成等）：异步执行、独立超时，不阻塞响应；
	// 各能力按自身策略决定是否沉淀（如 Agent 定义关闭 Formation 时记忆能力直接跳过）
	s.caps.CaptureAll(&capability.CaptureCtx{
		SessionID:  ses.ID,
		RunID:      runID,
		UserInput:  userInput,
		Reply:      res.Content,
		Transcript: captureTranscript(llmMsgs, userInput, res.Content),
		Def:        def,
		ProviderID: ses.ProviderID,
		Model:      ses.Model,
	}, memoryCaptureTimeout)
	// 三层 State：run 结束清理 temp 作用域（run 级临时态不跨轮存续）
	if s.tempClear != nil {
		s.tempClear(runID)
	}
	return res
}

// captureTranscript 组装沉淀用的对话副本：本轮发送给 LLM 的消息 + 用户输入 + 最终回答。
func captureTranscript(sent []*llm.Message, userInput, reply string) []llm.Message {
	transcript := make([]llm.Message, 0, len(sent)+2)
	for _, m := range sent {
		if m == nil {
			continue
		}
		transcript = append(transcript, *m)
	}
	if userInput != "" {
		transcript = append(transcript, *llm.UserMessage(userInput))
	}
	if reply != "" {
		transcript = append(transcript, *llm.AssistantMessage(reply, nil))
	}
	return transcript
}

// persistUsage 把每次 LLM 调用的用量落 token_usages。
//
// 先落明细再统计：明细是仪表盘三线图的唯一数据源，失败只告警不阻断（不因计量丢回答）。
// 计费按 pricing.<model> KV 单价估算（未配置则 cost=0），费用随明细落库供仪表盘聚合。

func toolResultContent(p harness.ToolResultPayload) string {
	if p.Err != "" {
		return "error: " + p.Err + "\n" + p.Content
	}
	return p.Content
}

// failRun 错误落库：标 failed + 发 chat:error。
func (s *ChatService) failRun(ctx context.Context, runID, sessionID, assistantMsgID string, err error) {
	s.finishExec(runID, harness.StateFailed)
	now := time.Now().UnixMilli()
	_ = s.messages.UpdateStatus(ctx, assistantMsgID, map[string]any{
		"status":      domain.MessageStatusFailed,
		"stop_reason": domain.StopReasonError,
		"updated_at":  now,
	})
	s.emit(runID, sessionID, "chat:error", map[string]any{"code": 5000, "message": err.Error()})
	s.emit(runID, sessionID, "chat:done", map[string]any{
		"status": "failed", "reason": string(domain.StopReasonError), "message_id": assistantMsgID,
	})
}

// providerParams 拉 Provider DO → *llm.ProviderParams；DO 缺/取失败返回 nil（走全局默认）。

// finishExec 执行平面终态收束（registry 未注入时空操作）。
func (s *ChatService) finishExec(runID string, state harness.ExecutionState) {
	if s.execs != nil {
		s.execs.Finish(runID, state)
	}
}

// execState run 终止原因 → 执行平面终态：取消优先，错误落 failed，其余按完成收束。
func execState(reason harness.RunStopReason) harness.ExecutionState {
	switch reason {
	case harness.ReasonCancelled:
		return harness.StateCancelled
	case harness.ReasonError:
		return harness.StateFailed
	default:
		return harness.StateCompleted
	}
}

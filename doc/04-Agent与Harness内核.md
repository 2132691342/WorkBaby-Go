# 04 · Agent 与 Harness 内核

`internal/harness/`：自研轻量 ReAct 内核。不依赖 wails/api/service；LLM/Tool/Memory/Compressor 全部接口注入，单测可全 mock。

## 1. 模块边界

```
harness/
├── runner.go        # 主循环（流式/ReAct/重试/中间件/检查点落点）
├── event.go         # 事件类型与载荷（agent.{run|turn|tool}.{phase} + retry）
├── checkpoint.go    # 检查点快照 + JSONL 存储 + StepRecord（幂等恢复）
├── auto_compress.go # 结构化摘要压缩器（ContextCompressor）
├── compress.go      # MicroCompressor（确定性兜底）+ token 估算
├── delegate.go      # 子 Agent 委派（隔离 + 同参去重）
├── agents.go        # 内置 Agent 定义（default/coding/research/writer：人设/预算/工具过滤）
├── definition.go    # Definition（Budget/ToolPolicy/Memory 开关）
├── middleware.go    # Middleware（BeforeTurn/AfterTurn）
├── sink.go          # EventSink 事件出口
├── execution.go     # ExecutionRegistry 多 run 拓扑（parent_run_id）
├── runctx.go        # runID/sessionID ctx 注入
└── text_tool_calls.go # 正文工具调用兜底解析
```

依赖铁律：harness 只 import llm/tool/memory/pkg；事件经 sink 出口，由 service 层翻译为 `chat:*`。

## 2. 事件体系

| Kind | 触发 | 载荷要点 |
|---|---|---|
| agent.run.start / run.done | run 起止 | model / reason / stop_reason / usage |
| agent.turn.start / end | 轮次起止 | end 带本轮用量 |
| agent.turn.delta / thinking | 流式增量 | kind=content\|thinking |
| agent.tool.call / start / result | 工具三段 | id/name/arguments；result 含 error/duration/meta/data/refused |
| agent.retry | 建流瞬时错误退避重试 | attempt/delay_ms/reason |
| agent.checkpoint / error | 检查点落盘 / 失败 | — |

子 Agent 事件带 `parent_run_id` + `agent`，service 层据此分流（子生命周期独立成 `chat:subagent-*`，绝不复用父 `chat:done`）。

## 3. Runner 主循环

`RunMessages(ctx, runID, sessionID, assistantMessageID, model, msgs) RunResult`：

1. 组装上下文（调用方装配 system 前缀 + 历史 + 本条输入）；
2. 每轮：中间件 BeforeTurn → 预算内压缩 → `streamWithRetry` 流式调 LLM（thinking 分离）→ 累积正文/推理 → 发 delta 事件；
3. 有 tool_call：Schema 校验 → 执行侧 defs 校验（幻觉工具 refused）→ 目录信任闸 → 策略门（deny/ask+审批）→ **幂等命中检查** → 执行（recover 包 panic、单工具超时、结果截断）→ 停滞检测 → 回填 tool 消息 → 落检查点；
4. 无 tool_call：问 follow-up 缝，无续接则 end_turn 终止；
5. 终止原因：end_turn / cancelled / error / max_turns / stagnation / token_budget / max_tokens。

横切能力（均 `WithXxx` 注入，nil = 关闭）：

- **streamWithRetry**：仅瞬时错误（限流/5xx/超时）重试，指数退避+抖动、Retry-After 优先、取消优先；流中途错误不重试（防重复输出）；重试发 `agent.retry`。重试耗尽再问 TurnAdjuster 换模型（有界，防主备横跳）。
- **steering / follow-up 注入缝**：跑工具中途插话 / 说完自动续接，注入后重置停滞计数。
- **中间件**：TokenUsageAccumulator（每轮用量）、HistoryTruncator（旧阈值兜底）。
- **停滞熔断**：同名同参连续 / 连续工具失败达 StagnationLimit → stagnation。
- **text_tool_calls**：部分端点把 tool call 写进正文时按白名单解析兜底。

## 4. 检查点与幂等恢复

`Checkpoint`：runID/sessionID/turn/messages/state/assistantMsgID/content/thinking/usage/**stepRecords**。

- 存储接口 `CheckpointStore`（Append/LoadLast/Cleanup）：内置 JSONL 文件实现；service 提供 SQL 实现（agent_checkpoints 表，跨重启可恢复、会话删除级联）。
- 保存时机：每轮工具执行回填后；每会话保留最近 3 个 run。
- **StepRecords 幂等**：成功工具调用按 `tool:{name}:{args}` 记结果；`Resume` 续跑时同名同参命中直接复用结果并发带 `reused` 标记的事件，**不重放副作用**；失败/被拒不记录，恢复时重试。
- `Resume(ctx, runID, sessionID, assistantMsgID, model)`：LoadLast → 从 turn+1 续跑，累积值（content/thinking/usage）跨恢复连续。

## 5. 上下文压缩

接口 `Compressor.Compress(msgs, budget)`；增强接口 `ContextCompressor.CompressCtx`（runner 优先）。

- **MicroCompressor**（默认兜底）：先清最旧 tool 结果，仍超则截断最旧对话；零 LLM 成本。
- **AutoCompressor**（service 装配）：超预算时把早期历史压成六段交接摘要（目标/进度/决策/文件/下一步/约束）；切点不拆 `assistant(tool_calls)↔tool` 对；尾部保留约 budget/2；摘要迭代更新（旧摘要按前缀指纹并入）；失败降级 Micro。压缩完成经 Notify 回调发可见事件。

## 6. 子 Agent 委派

`Delegate(ctx, agent, task)`（tool.Delegator 实现，run 开始注入 ctx）：

- 四重隔离：上下文（人设+任务，不继承父历史）/预算（≤12 轮 3 分钟）/工具（只收缩不升权）/输出（只回传 ≤4000 rune 摘要）。
- 同参去重：并发相同 (agent, task) 共享一次执行，后来者等待首发结果。
- 事件转发：仅工具层与生命周期事件进父 sink（带 parent_run_id/agent）。

## 7. 错误码（5000 段）

5001 会话不存在 · 5002 会话忙 · 5003 会话无效 · 5004/5005 消息不存在/无效 · 5007 Resume/检查点失败 · 5008 消息不属于会话/委派不可用。

## 8. 测试

测试只保留复杂链路：ReAct 多轮工具调用（mock LLM）、审批拒绝/门禁、steering 注入、截断重试、委派上下文隔离、上下文压缩、检查点写入+Resume；见 `runner_test.go` / `checkpoint_test.go` / `delegate_test.go` / `compress_test.go`。简单分支与参数校验不设用例。

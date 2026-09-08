# 04 Agent 与 Harness 内核

## 定位

`internal/harness/` 是 WorkBaby 的 Agent 内核：自研轻量 ReAct 循环，无外部 Agent 框架依赖。本模块产出所有 `chat:*` 事件 + 驱动消息落库 + 工具执行 + 检查点 resume。

## 设计要点

- **Runner = 编排器**（`runner.go`）：每轮「装配 → LLM → 工具 → 回填 → 收尾」，单锁串行（一个 Runner 实例同一时刻只跑一个 run）
- **事件驱动**：通过 `sink` 接口发出 `Event{Kind, RunID, SessionID, Turn, Payload}`，service 层映射为 SSE
- **工具护栏链**：单层裁决——`Gate(Decision) × Approver` 在 runner，工具内部不再内置审批
- **压缩保护配对**：MicroCompressor + HistoryTruncator 用 `safeTailStart` 找非 tool 切点，绝不拆散 assistant(tool_calls) 与其 tool 结果
- **检查点幂等**：工具步骤记忆化 `stepRecord`，resume 时同 name+args 命中直接复用结果不重放
- **错误归类 + 操作提示**：分类为机器可读 `kind`，附"去哪调配置"的中文 hint

### 上下文预算校准

`tokenScale`（EMA，惯性 0.5，限幅 [0.5, 8.0]）逐轮用上游实测 prompt_tokens ÷ 本地估算修正系统性偏差
（工具结果 JSON 与纯文本的 token 密度可差数倍）。压缩前 `calibratedBudget` 把 ContextBudget 折算成
估算口径；上一轮实测已超预算时直接把预算压到 1 强制压缩——实测值优先于估算。

### 工具护栏两层（execOne，停滞检测之后、幂等恢复之前）

- `LoopLimit`：全 run 维度 `name+args` 累计计数，抓停滞检测抓不到的 A→B→A→B 交替循环，命中走 refused；
- `MaxToolCalls`：单 run 工具执行总次数预算，超限 refused。

## 核心契约

### Config（`DefaultConfig`）

| 字段 | 默认 | 说明 |
|---|---|---|
| `MaxTurns` | 30 | 单 run 最多轮次 |
| `LoopLimit` | 3 | 同名同参在整 run 内累计次数上限；0 = 关 |
| `MaxToolCalls` | 0 | 单 run 工具执行总次数预算；0 = 不限 |
| `ContextBudget` | 120_000 | 估算 token 上限 |
| `CompressRatio` | 0.9 | 压缩保留比例 |
| `MaxToolResultLen` | 50_000 | 工具结果回填 LLM 的截断长度 |
| `ToolParallelism` | 4 | 只读工具并发数；1=串行 |
| `MaxTokens` | 0 | 全 run token 预算；超即停 |
| `ToolCallTimeout` | 5min | 工具执行超时 |
| `StagnationLimit` | 5 | 连续失败/同名同参重试熔断 |

### Middleware 链

| Middleware | 职责 |
|---|---|
| `TokenUsageAccumulator` | `AfterTurn` 累加 + 触发器（per-turn 已在 `EventTurnEnd` 发出） |
| `HistoryTruncator` | 仅 legacy `CompressTrigger>0` 时挂；现代配置走 `ContextBudget` |

### Compressor 接口

```go
type Compressor interface {
    Compress(msgs []*llm.Message, budgetTokens int) []*llm.Message
}
```

- `MicroCompressor`（默认 / 兜底）：先折最旧 assistant+tool 段（→ `[早期工具调用与结果已省略]` 占位 assistant），仍超再按安全切点对半截断
- `AutoCompressor`（LLM 摘要）：watermark 触发 → LLM 六段交接摘要 → 迭代更新，失败降级 Micro
- `ContextCompressor`：`CompressCtx(ctx, ...)` 带 ctx 入口

### 统一工具护栏链（gateTool 单层裁决点）

```
[runner] gateTool:
  Gate.Decide(name, risk):
    Deny  → refused("denied by policy")            显式 Deny 任何模式（含 yolo）都不绕过
    Allow:
      yolo 模式 → 整体放行（命令级风险一并放行）
      实现 RiskClassifier 的工具（exec / run_skill_script）
        → ClassifyArgs per-call 命令级裁决：
            risk=="" → 免审放行（白名单内且未命中危险正则）
            risk 非空 → 走 Approver（危险正则命中每次必问）
      其余工具 → 直接放行
    Ask:
      有 per-call 风险 → risk=="" 免审，否则走 Approver
      无 per-call 风险 → 按工具静态风险问一次（Exec/Destructive → Irrev，其余 Needs）
  拒绝一律 refused（结构化回执），模型据此换方案自愈
  放行后 ctx 标记 GuardChain → 工具 Execute 跳过自有审批（单层闸门）
```

### EventTurnEnd payload（per-turn）

`UsagePayload{InputTokens, OutputTokens, CacheRead, CacheWrite, Total}` —— 全部为**本轮**实际值，非累计（前端展示上下文占比用此值，避免被误读为"占比爆炸"）。`chat:stats` 载荷另含 `latency_ms`（本轮建流→流结束墙钟耗时）。

### 计量

- `TurnUsage{Turn, Usage, LatencyMs}`：per-turn 明细 + 本轮墙钟耗时（含重试退避），service 逐行落 `token_usages`（含 latency_ms）
- 工作流 LLM 节点（ReAct 与补全两路）与上下文压缩摘要同样落 `token_usages`（Source=workflow，压缩 Turn=-1）
- 系统只做中立 token 计量与耗时记录；计费是供应商侧的事，无 cost 字段

### ErrorPayload（终态错误事件）

`{Code, Message, Kind}`：`Kind` ∈ `timeout / rate_limited / auth / context_length / connection / upstream / approval_denied`，未匹配为空串；`Message` 末尾自动追加对应 hint。

### 关键流程

#### ReAct 主循环

```
for turn := 0; turn < MaxTurns; turn++ {
  if ctx.Err() != nil { break }
  msgs = compressor.Compress(msgs, ContextBudget)  // 配对安全
  stream := provider.Stream(ctx, req)
  for chunk := range stream {
    appendDelta / appendThinking / appendToolCall
    usage += chunk.FinalUsage
  }
  buildAssistantMessage + append to msgs
  if no tool_calls && text clean && !truncated → break end_turn
  if tool_calls:
    for each call (parallel if all readonly + parallelism>1):
      executeOne(call): 暴露校验 → 路径信任 → 工具策略门+per-call 风险 → 注入防护 → 工具 Execute
      msgs += tool_result
    saveCheckpoint
  if no tool_calls && follow-up queue has msgs → continue
}
```

#### 截断重发（finish_reason=length / max_tokens）

残缺 tool_calls **不执行**，回填一条 truncated 工具结果让模型缩短重发。

#### 注入防护

`HasNestedToolCallMarker(args)` 检查 `<tool_call` / `"tool_calls"` 等伪标记；命中即 `refused("args contain nested tool-call markers (possible prompt injection)")`。

#### 优雅停止

`hooks.ShouldStopAfterTurn(ctx, &TurnSignal{Turn, ConsecutiveToolFails})` 每轮收尾询问，返回 true 即以 end_turn 收束（区别于 max_turns 被动熔断）。

## 约束

- harness 不依赖 wails / api / service（CLAUDE.md §2.2）
- 单 runner 实例同时只能跑一个 run（按 session 串行）
- 工具消息 `tool_call_id` 必须在 assistant.tool_calls 中存在（toLLMMessages 剥孤儿；压缩保留配对）
- 上下文压缩以段为最小单位，**绝不拆散 assistant+tool 对**——这是与上游协议兼容的硬底线

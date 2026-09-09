# Agent 与 Harness 内核

位置：`internal/harness/`。这是一个与业务无关的 ReAct 执行内核：给它一个 Provider、一组工具和一份任务定义，它负责把任务跑完。聊天、工作流 LLM 节点、后台任务、定时任务共用同一内核。

## Runner 主循环

`Runner.runLoop` 每一轮：

1. 中间件 `BeforeTurn` 调整消息（截断 / 压缩）
2. 调 Provider 流式接口，逐 chunk 转发：正文 → `EventTurnDelta`，思维链 → `EventTurnThinking`
3. 收到工具调用 → 过闸门 → 执行 → 结果作为 tool 消息回填 → 进入下一轮
4. 收到 `stop` → 结束，汇总用量与终止原因

终止原因枚举（`ReasonEndTurn` / `ReasonCancelled` / `ReasonMaxTurns` / `ReasonBudgetExceeded` / `ReasonStagnation` / `ReasonError`）统一映射到领域 `stop_reason`，前端据此差异化收尾（cancelled 是中性终态，不是错误）。

## 稳定性护栏

| 机制 | 行为 |
|---|---|
| 工具 panic 恢复 | 单工具崩溃不影响 run，转为该工具的失败结果 |
| 截断的工具调用重试 | 模型输出未闭合的 tool_call 标记时按可重试错误处理 |
| 停滞检测 | 连续多轮相同调用（同工具同参数）判定停滞，主动终止，避免死循环烧 token |
| 注入防护 | 检测模型正文里的伪 `tool_call` 标记（`HasNestedToolCallMarker`），命中则终止本轮 |
| 轮数 / token 预算 | 双上限，超出即终止并标记原因 |

## 工具闸门（Gate）

`tool.Gate` 按「会话权限档 + 命令风险」决定放行 / 询问：

| 权限档 | 行为 |
|---|---|
| restricted | 白名单外一律拒绝 |
| confirm | 白名单放行，其余询问 |
| auto（默认） | 低风险放行，高风险询问 |
| full | 全部放行（UI 用危险色提示） |

询问通过审批门（`service.ApprovalService`）发给前端：`chat:approval` 事件 + 待决列表接口，用户批准 / 拒绝 / 跳过。审批记录落库，应用重启时把遗留的未决审批标为 cancelled，卡在 streaming 的消息标为 interrupted（可续跑）。

## 上下文管理

- **HistoryTruncator**：估算 token 超阈值时按安全切点对折截断最旧消息（切点永不落在 tool 消息上，避免孤儿 tool 结果被上游 400 拒绝）。
- **MicroCompressor**：确定性折叠旧轮次（保留工具配对），超长时在安全切点二次截断。折叠后的占位文案显式声明「结果已消费，请勿重跑」，避免模型为找回被折叠的载荷而重复执行有副作用的工具。
- **AutoCompressor**：达到上下文预算（`context_window × compression_ratio`）时，用 LLM 生成六段交接摘要替换早期轮次；失败降级为 Micro。被归档的消息完整保留在库中（`status=archived`），仅退出上下文。压缩发生时发 `chat:compressed` 事件，前端提示用户。

## 检查点与续跑

每轮结束把消息列表 + 状态 + 用量落检查点（JSONL，DB 索引）。进程崩溃或用户停止后，`Resume` 从最后一个检查点继续，用量跨段累计。中断的消息在前端显示「继续」入口。

## 子 Agent 委派

`delegate_task` 工具把子任务交给指定子 Agent：

- 子 run 的消息列表只有「子 Agent 人设（system）+ 任务（user）」，看不到父历史——上下文与预算隔离。
- 子 run 事件带 `ParentRunID`，前端独立成通道展示（`chat:subagent-*`），不与主 run 的终态混淆。
- 只把摘要回传父 run，过程详情在事件日志里。

## Steering 与 Follow-up

两条注入缝消费同一个队列：run 进行中用户插话（steer）在下一轮前注入；run 结束后队列里还有内容则自动续跑（follow-up）。实现「边跑边补充要求」的体验。

## 事件与重放

run 内所有事件带单调 `seq`，经 `event.RunEventLog` 环形缓冲 + JSONL 文件落盘。SSE 断线重连时按 `Last-Event-ID` 重放；窗口被覆盖则发 `chat:gap`，前端转全量快照。`/chat/runs/:id/events` 可导出完整事件流供自动化消费。

除内核自身事件外，service 层在 run 生命周期上注入两类业务事件，与内核事件共享同一套 seq 与重放机制：

| 事件 | 时机 | 用途 |
|---|---|---|
| `chat:skill` | 首帧正文之前（技能命中时） | 让执行过程时间线的首行展示命中的技能，叙事顺序为「技能命中 → 工具 → 回答」 |
| `chat:warn` | 文件写入落到 `.workbaby/` 之外时 | 越界告警，前端红色提示用户回滚 |

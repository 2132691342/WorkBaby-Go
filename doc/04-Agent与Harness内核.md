# Agent 与 Harness 内核

位置：`internal/harness/`（接入层 `internal/capability/`，编排在 `internal/service/chat.go`）。这是一个与业务无关的 ReAct 执行内核：给一个 Provider、一组工具、一份 Definition，跑到唯一终态。chat、工作流 LLM 节点、定时任务、后台任务共用它。

## 1. Agent 定义（Definition）

纯数据：Name / Description / Persona / Tools(ToolPolicy) / Memory(MemoryPolicy) / Budget。

- ToolPolicy：Allow / Deny（glob）、MaxTools。`FilterTools` 在装配方既有过滤（工具启停、技能白名单）之后应用：命中 Deny 剔除 → Allow 非空仅留命中 → MaxTools 截断。
- MemoryPolicy：Enabled / RecallLimit / Formation。
- Budget：MaxTurns / MaxTokens / ContextTokens / ToolCallTimeout / MaxWallTime，`Apply` 覆盖 Runner.Config 的非零项。

内置 Agent（按名引用，未知名回退 default）：

| name | 定位 | 工具 | 记忆 | 预算侧重 |
|---|---|---|---|---|
| default | 通用全能 | file_* / exec / delegate_task / run_workflow / websearch / webfetch / http / knowledge_search / memory_write / todo / request_input / plan 模式 | 开（召回 6）+ 形成 | ContextTokens 120k |
| coding | 工程 | exec / file_* / doc_reader / archive / websearch | 关 | MaxTurns 40 |
| research | 调研 | websearch / webfetch / http / knowledge_search | 开（召回 10）不形成 | 默认 |
| writer | 写作 | 未限制 | 开（召回 6）不形成 | 默认 |
| explore | 只读探索 | file_read / file_list / file_grep / file_search / doc_reader / knowledge_search / websearch / webfetch（白名单不含任何写工具） | 关 | MaxTurns 24 |

`explore` 的存在意义是「不可能改坏任何东西」：白名单里刻意没有 file_write / file_edit / exec / run_skill_script / delegate_task，宁可让它做不了，也不让它能做。

Persona = 各自定位 + 共享方法论段（先探查、先计划、最小改动、用工具验证结果、失败改道、不确定就问、成果必须可核验）。

自定义子智能体两个来源，`AgentProfileService.Sync` 一次物化成 Definition 整表替换进 harness 注册表（`harness.SetCustomAgents`）：

| 来源 | 载体 | 说明 |
|---|---|---|
| 设置页 CRUD | `agent_profiles` 表 | 界面上随手改；写操作（创建/启停/删除）后即时重同步 |
| 定义文件 | `{home}/agents/<名>.md` | frontmatter 元数据 + 正文人设；可随 dotfiles / 仓库分发 |

Agent 定义没有工作区维度（子智能体是个人能力，不随仓库走），因此同名冲突取「界面即最近一次显式编辑」：表 > 文件。命令正好相反——它有工作区级载体，按就近覆盖（见 doc/07）。

`Agent(name)` 查找顺序为内置 → 自定义 → 回退 default。delegate_task 委派、会话 `/agent` 切换、后台任务提交共用这一注册表。内置名（default / coding / research / writer / explore）为保留名；表与文件同名时以表为准（界面上的显式修改优先于磁盘上的历史文件）。

定义文件 frontmatter 与 Definition 的映射：

| 字段 | 落点 | 说明 |
|---|---|---|
| `name` | Definition.Name | 必填；缺省取文件名；须匹配 `^[a-z][a-z0-9-]{0,63}$` |
| `description` | Definition.Description | 必填；主 Agent 依据它决定何时委派 |
| `tools` | Tools.Allow | 逗号分隔 glob；`*` = 不设白名单（全部工具） |
| `disallowedTools` | Tools.Deny | 逗号分隔 glob |
| `maxTurns` | Budget.MaxTurns | 正整数 |
| `model` | Definition.Model | 专属模型；`inherit` 或不写 = 跟随会话模型 |
| `thoughtLevel` | Definition.Thinking | off / low / medium / high；须与 `model` 同时给出 |
| 正文 | Persona | 即该系统提示词 |

缺 name / description、名字非法、撞内置名的文件会被诊断并跳过（单个文件写坏不影响其余）。未接入执行链的字段（`color` / `mcpServers` / `injectAgentsMd`）仅登记告警——写错不生效必须可见，否则比不写更难排查。

### 模型与推理强度的覆盖规则

| 维度 | 优先级 | 说明 |
|---|---|---|
| 模型 | Agent 定义 > 会话模型 | 覆盖时发 `chat:warn`（kind=agent_model_override）——「界面显示 A、实际跑 B」必须可解释；`token_usages` / `run_records` / 费用一并记实际调用的模型，避免按模型归账时错账 |
| 推理强度 | 请求级（用户当次档位）> Agent 定义 > Provider/全局默认 | 仅当请求未显式指定时生效，用户当次的选择永远优先 |

两条边界：

- **模型名在会话的 Provider 上解析**：一个 Provider 一套凭据与协议方言，跨 Provider 换模型属于会话级选择，不由 Agent 定义承担。
- **`thoughtLevel` 必须与 `model` 同时给出**：不指定模型即「完全继承」，此时单方面改思考档位会让用户当次的选择失效。文件里违规只告警丢弃，设置页保存时直接拒绝（见 `AgentProfileService`）。委派子 run 同样遵守：子 Agent 定义了自己的模型就用它，否则继承父 run 的模型。

## 2. 上下文装配（ContextAssembler）

- ContextPiece：Key（语义标识）/ Title / Body / Priority。
- 优先级常量：Essential 10 / High 30 / Medium 50（默认）/ Low 70 / Lowest 80。
- `Build`：按加入顺序拼成一条 system 消息（标题行 + 正文，段间空行）。
- `BuildWithin(maxRunes)`：超预算按 Priority 从低到高丢弃，essential 永不丢，返回被丢段 Key。
- 装配入口 `service.buildSystem`：能力注册表 PreloadAll → 追加历史归档摘要(High) / 压缩保留指示(High) / 验证提醒(Low) / 工作区沙箱(Essential)；system 预算 = 上下文预算 × 30%（rune 口径）；被丢段经 `chat:context-trimmed` 事件回传前端。run 与 `/context` 占用透视同源。

## 3. 能力三通道（capability.Capability）

每个能力暴露三条通道：Preload（每轮自动注入上下文）/ Tools（模型主动调用）/ Capture（run 结束后自动沉淀）。

- Registry 按 order 串联 Preload：persona 10 → environment 15 → workspace 20 → todo 25 → session_var 28 → memory 30 → knowledge 40 → skill 50 → workflow 60 → inbox 70。
- Preload 单能力失败/panic 仅告警跳过，不阻断 run；Capture 脱离 run ctx、异步执行、每能力独立超时（默认 30s）。
- 能力间经 RunState 交换结果（技能命中写回工具白名单与命中详情，供时间线回放）。
- 常驻段：人格、环境（OS/时间/exec 调用形态）、工作区（绑定目录与落点纪律）；记忆注入长期记忆尾部（≤4000 rune）；知识库输入 ≥2 rune 自动召回 topK=3（≤3000 字符）并暴露 knowledge_search；技能命中注入正文（≤6000 rune，Low）并约束工具白名单，未命中注入轻量索引（≤800 rune，Lowest）；Todo 计划每轮回显（Low）；工作流注入可用清单（Lowest）；结构化状态按 user/session/temp 分节注入（Low）。
- **重型段门控（RelevanceGate）**：跨会话召回与知识库自动召回默认关闭，只有显式 `@引用` 或会话关键词命中才放行（判定顺序：opt-in > keyword > off，见 `capability/gate.go`）。目的不是省事，而是让「空闲会话上下文精简」成为结构性默认，而不是靠 Priority 裁剪在预算溢出时兜底——那时 token 代价已经付过了。
- **收件箱能力（inbox 70）**：无 Preload / 无工具，只在 Capture 通道工作——用本 run 的 provider/model 抽取记忆与技能候选，落待审收件箱（人审合入），细节见 doc/08。

## 4. 历史重建（toLLMMessages）

DB 历史 → `[]*llm.Message`：跳过 streaming 占位与 archived 消息（归档内容在 system 归档摘要段体现）；孤儿 tool（tool_call_id 无前置 assistant 匹配）剔除；空 tool 结果填 "(empty)"；空 assistant 占位（无 content 无 tool_calls）剔除；assistant 的 tool_calls 反序列化回填。

## 5. ReAct 主循环（Runner.runLoop）

| 阶段 | 行为 |
|---|---|
| 循环入口 | ctx 取消检查、MaxTokens 预算检查（熔断） |
| 压缩 | 折算预算后交压缩器（Auto 优先，Micro 兜底）；按前后差异发压缩事件（边界证据） |
| 建流 | 参数三层合并（请求级 > Provider 级 > 全局默认）；仅瞬时错误退避重试（Retry-After 优先，流中途不重试） |
| 转发 | 正文 → 增量事件、思维 → 思维事件；工具调用闭合即发调用事件；正文兜底解析文本形态工具调用（仅认本次暴露工具，单轮 ≤8） |
| 回填 | 组装 assistant（含 tool_calls）入上下文；发 turn 结束事件（per-turn 用量）；实测 input 回推 tokenScale（EMA 校准） |
| 截断分支 | 停止原因为 max_tokens/length 且含工具调用 → 不执行，回填「未执行 + 缩短重发」并推进停滞计数 |
| 无工具 | 先问 follow-up 缝（有则续接），无则 end_turn 收尾 |
| 有工具 | 执行并回填 → 问 steering 缝 → 存检查点 → 轮间调整（换模型/换工具集）→ 优雅停止点检查 |

终止原因：end_turn / max_tokens / budget_exceeded / max_turns / stagnation / cancelled / error。统一出口：先错误事件（分类 + 可操作提示）后 RunDone。取消优先归一为 cancelled；循环跑满且仍在发起工具调用归一为 max_turns。

## 6. 工具洋葱链（toolchain.go）

顺序即语义（外 → 内）：暴露校验 → 解析 → JSON Schema 校验 → 注入防护 → 目录信任 → 策略门 → 用户钩子 → 停滞 → 循环/预算 → 幂等恢复 → 执行。任一层返回消息即短路整链。

| 层 | 行为 |
|---|---|
| 暴露校验 | 未在本次定义中的工具直接拒绝（not_exposed） |
| 参数校验 | Schema 不合规按工具错误回填，推进失败熔断 |
| 注入防护 | 参数含伪工具调用标记 → 拒绝（prompt_injection） |
| 目录信任 | 信任三态（allow/ask/deny）+ 计划模式硬拦 → 拒绝（path_trust） |
| 策略门 | 显式规则 glob > 会话模式 × 风险默认；按 per-call 命令级风险裁决；deny → policy，ask 经审批 → approval |
| 用户钩子 | PreToolUse 事件触发用户命令（子进程协议）：allow 放行并把附加上下文带进回执、ask 升级为人工确认、deny 拦截 → user_hook；钩子故障不阻断（执行器已兜底放行） |
| 停滞 | 同名同参连续 ≥ StagnationLimit（默认 5）置熔断信号 |
| 循环/预算 | 全 run 同名同参计数 ≥ LoopLimit（默认 3）→ loop_guard；执行次数超上限 → tool_budget；签名键序归一化（JSON 规范化），同参异序仍判重复，计数在锁内并行安全 |
| 幂等恢复 | Resume 命中已完成成功调用 → 复用，不重放副作用 |
| 执行 | 施加超时（工具级覆盖全局默认 5min）、panic 隔离为错误结果、后处理钩子链（多槽，按注册序逐字段覆盖）、结果截断、成功且未拒绝时写幂等记忆 |

- 并发：整轮全只读且并发数 > 1 → 并行执行（结果按序回填）；否则严格串行（写工具无并发）。
- 拒绝回执为结构化 JSON（refused + reason_code + hint）。原因码：not_exposed / prompt_injection / path_trust / policy / approval / user_hook / loop_guard / tool_budget。

### 用户钩子子进程协议（user_hooks）

七类事件，触发即经 `cmd /c` 拉起子进程；`--` 表示该事件不参与 matcher 过滤：

| 事件 | matcher | 插入点 | 决策效果 |
|---|---|---|---|
| `SessionStart` | — | 首轮模型请求前（续跑不重放） | `additionalContext` 追加进 system |
| `UserPromptSubmit` | — | 用户提交后、模型调用前 | `additionalContext` 注入本轮 system；`continue:false` / exit 2 阻断本次请求 |
| `PreToolUse` | 工具名 | 策略门之后、执行之前 | `permissionDecision`：`allow` 放行（附加上下文带进回执）/ `ask` 升级人工确认 / `deny` 拦截 |
| `PermissionRequest` | 工具名 | 仅在需要询问时 | `decision.behavior`：`allow` / `deny`，直接代答，不打扰用户 |
| `PostToolUse` | 工具名 | 工具成功后 | `additionalContext` 追加进工具回执（`[hook]` 段） |
| `PostToolUseFailure` | 工具名 | 工具失败后 | 同上，用于恢复建议 / 诊断 |
| `Stop` | — | 本轮无工具调用、run 即将收尾 | `decision:"block"` + `reason` → 带着反馈再跑一轮（连续上限 3 次） |

- **载荷**：stdin 一行 JSON。公共字段 `hook_event_name` / `session_id` / `run_id` / `cwd` / `permission_mode`；事件专属 `source`（SessionStart）、`prompt`（UserPromptSubmit）、`tool_name` / `tool_input` / `tool_use_id`（工具类）、`tool_response` / `error`（Post 类）、`last_assistant_message` / `stop_hook_active`（Stop）。
- **决策**：stdout JSON，优先读 `hookSpecificOutput`，兼容顶层 `additionalContext` / `additional_context`。退出码 0 = 解析 stdout、2 = 阻断快捷方式、其他 = 当前钩子可恢复失败（记日志跳过，不影响后续钩子）。
- **matcher**：空 / `*` = 全部；只含字母数字下划线与 `|` 按精确名称名单（`Write|Edit`）；含其他字符按正则。非法正则在写入期即拒绝。
- **聚合**：多个匹配钩子按 `sort` 升序执行，拒绝优先于询问优先于放行（deny > ask > allow）；`PreToolUse` 命中 deny 立即短路。
- **故障**：超时（默认 10s，上限 60s）/ 子进程退出异常 / 非法 JSON 一律放行并记日志（失败原因带子进程输出前 200 字）。工具类钩子同步执行（结果要进回执）；Stop 仅在续接缝消费。

命令行按用户填写原样透传给 `cmd`（`SysProcAttr.CmdLine`，不做参数转义）——带引号的写法（`node "C:\Program Files\guard.js"`、`cmd /c "echo ok"`）不会被重排破坏。

## 7. 会话权限模式（tool.SessionMode）

| 模式 | 行为 |
|---|---|
| restricted | 只读直行，写/执行/网络/删除一律拒绝（不询问） |
| default | 只读直行，其余询问 |
| auto_edit | 只读与本地写直行，执行/删除/网络询问 |
| yolo | 全部放行 |

目录信任挂在策略门之前；未登记目录恒为 ask（fail-closed）。计划模式激活时非只读工具执行器级硬拦。

## 8. 上下文压缩

- 触发预算 = 上下文窗口 × 压缩比例；按 tokenScale 折算；上一轮实测已超预算则硬触发（兜底估算低估）。
- MicroCompressor（默认，确定性、不调 LLM）：把最旧「assistant(tool_calls) + 连续 tool 结果」整段折叠为占位（声明「已消费、勿重跑」，带工具名与 tool_call_id 锚点），仍超预算按轮（user 锚点）截断，最后一轮永不丢；两条路径都以段为单位，绝不制造孤儿 tool。
- AutoCompressor（优先）：尾部保留约 budget/2 且对齐「不拆 tool 对」边界、不越过最后 user 锚点；早期历史压成六段交接摘要（目标/进度/决策/文件/下一步/约束），前缀哈希命中则迭代合并；失败降级 Micro；摘要用量单独落库（turn=-1）。
- 压缩证据（filter_key、cutoff_at、removed_msgs、recovery_refs）→ `chat:compressed` 事件 + 会话元数据，用户可见。

## 9. 检查点与续跑

- 每轮工具回填后写检查点（messages / state / usage / content / thinking / 步骤记录 / assistant 消息 id）。
- 语义「可覆盖的最新一轮」：按 (run_id, turn) upsert 并删更早 turn；每会话保留最近 3 个 run。
- 步骤记录：已完成成功调用按 `tool:{name}|{args}` 记结果，Resume 命中复用不重放副作用。
- Resume：从最后一轮继续，用量跨段累计；不重发 run 开始事件（调用方补 resumed 标记）。
- 进程崩溃：启动排空把 streaming 消息标 failed + interrupted，前端给「继续」入口。

## 10. 插话与续跑缝隙（Steering / Follow-up）

同一会话级注入队列（按 sessionID 分桶），被两条缝消费：

- Steering：轮与轮之间、且本轮跑过工具后注入（长任务中途纠偏）；注入后重置停滞计数。
- Follow-up：本轮无工具调用、run 即将收尾时注入（自动续接下一波）。

注入消息立即落库（与工具结果共享 seq 分配器）；run 结束或取消时清空队列。

## 11. 子 Agent 委派（delegate_task）

- 上下文隔离：子 run 仅「人设 system + 任务 user」，看不到父历史。
- 预算隔离：独立轮次/墙钟/工具超时上限，不压缩；工具集由子 Agent 策略只能收缩不能升权。
- 事件隔离：子 run 仅工具层与生命周期事件转发父 sink，带父 run id 与 Agent 名；正文/思考不转发。service 侧映射为独立 `chat:subagent-*` 通道；子工具事件带 agent 标签实时推送，但不写父 run 的工具调用记录、消息块与 tool 历史。
- 去重：并发同参（agent + task）共享一次执行。
- 回传：只把摘要（≤4000 rune）回父 run；子 run 用量单独落库（source=delegate，turn=-1）。

## 12. 事件模型

- Event：Kind / RunID / ParentRunID / SessionID / Turn / Agent / Payload。
- EventKind：run.start、turn.start、turn.delta、turn.thinking、turn.end、tool.call、tool.start、tool.result、checkpoint、compressed、run.done、error、retry。
- Sink 接口（FuncSink / NopSink）；Runner 经 Sink 推事件，service 侧由 `runEventMapper`（chat_eventmap.go）统一映射为 `chat:*` 并收口落块 / 落 tool 消息——Runner 不感知持久化与前端协议，子 run 仅工具层与生命周期事件转发父 sink。
- 用量口径：input / output / cache_read / cache_write / total；CacheRead 与 CacheWrite 是 Input 的拆解维度，不叠加。
- service 桥接事件：`chat:stream(.start)` / `thinking` / `stats` / `tool(-start/-result)` / `skill` / `todo` / `compressed` / `warn` / `retry` / `error` / `done`；子 run：`chat:subagent-start` / `-error` / `-done`。
- seq：run 内单调；SSE 断线按 Last-Event-ID 重放，缓冲被覆盖则发 gap 让前端转全量快照。

## 13. 反幻觉核验

- 零工具调用却声称「已创建/已保存/已生成文件」→ 发 `chat:warn`（kind=unbacked_claim）强提示，不静默通过。
- 上一轮改了文件却未执行任何命令 → 下一轮注入「验证提醒」（Low）督促先跑最小验证再交付。

## 14. 不变量

- 单 run 唯一终态：所有退出路径经统一出口，先错误事件后 RunDone；取消优先归一为 cancelled。
- run 内事件 seq 单调。
- 检查点每 run 只保留最新一轮；Resume 只认最后一轮。
- assistant(tool_calls) 与 tool 结果配对完整：压缩与历史重建均不制造孤儿 tool 或空 assistant。
- 已完成成功工具调用经步骤记录幂等复用，不重放副作用。
- 工具调用必产生一条回填消息。

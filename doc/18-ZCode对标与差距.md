# ZCode 对标审计与差距（2026-09-13）

> 逐项对照 ZCode 官方功能清单，盘点 WorkBaby 现状（✅ 已具备 / 🟡 部分具备 / ❌ 缺失），
> 并给出补齐路线。审计基线：P0-P4 重构完成后的 main（df78206）+ 本轮 UI 改版。

## 1. 总体结论

- **agent / harness 内核**在 P0-P4 已按 pi / go-micro / nomifun / trpc-agent-go 对标补强
  （durable pause 审批、事件双通道、签名去重、检查点续跑、子代理委派、反幻觉证据、deferred tools），
  与 ZCode 的内核能力面**基本同构**。
- **交互层**此前对标不足：本轮（2026-09-13）补齐 目标模式 /goal、AGENTS.md 注入、
  划选引用追问、计划模式 UI 化 四项；剩余差距见 §3 路线。
- 明确不抄：闲时任务（依赖云端套餐配额）、Remote Control / 手机端、远程开发（SSH/WSL/Docker）
  —— 单机 Windows 个人助手的定位不需要。

## 2. 功能对照矩阵

### 2.1 聊天主链路

| ZCode 功能 | WorkBaby 现状 | 说明 |
|---|---|---|
| 流式回复 / 思考分离 | ✅ | think 块独立渲染，思考回看可折叠 |
| 工具轨迹（执行过程 · 读取N个文件 · … · 展开过程） | ✅ | 回合折叠叙事（D6-5）：receipt 聚合 + 展开看完整过程 |
| 文件变更摘要 + 撤销 | ✅ | chat:file-change + InlineDiffCard 就地 diff / 回滚（RollbackRun） |
| 编辑历史对话重发 | ✅ | resendFrom（截断 + 新内容重发） |
| 待发送队列 | ✅ | MidTurnQueue（流式中排队 / Ctrl+Enter 立即插话双通道） |
| 会话分叉 | ✅ | forkFrom（复制到该条为止的对话到新会话） |
| 划选对话内容追问 | ✅（本轮补齐） | 选区浮层「添加到当前任务」→ 引用块进输入框 |
| 辅助对话（/side 右栏并行小对话） | ❌ | P6 候选：需会话模型支持 side-channel 与主历史注入 |
| 压缩上下文 /compact | ✅ | 归档 + 逐轮摘要 + 保留指示钉住 + 压缩边界记录 |
| 上下文占用透视 /context | ✅ | system / 工具 / 历史三段 + 预算来源 |
| 崩溃/中断续跑 | ✅ | 检查点 Resume（已完成工具不重放） |

### 2.2 目标与执行控制

| ZCode 功能 | WorkBaby 现状 | 说明 |
|---|---|---|
| 目标模式 /goal（每轮校验 + 自动续跑） | ✅（本轮补齐） | 目标落会话元数据；run 收尾 LLM 实据校验（只看文件/命令/测试结果）；
  未达标携带下一步动作自动续跑，轮数上限 20；异常收尾自动暂停，可 resume |
| 执行模式四档（计划/需确认/自动编辑/完全访问，Shift+Tab 循环） | ✅（本轮补齐 UI 语义） | 后端四档权限本就齐（restricted/default/auto_edit/yolo）；
  「受限」更名为「计划」并对齐 ZCode 语义，/trust 循环切换含四档 |
| 推理强度（低/中/高） | ✅ | 思考强度三态 + Provider 级覆盖（EffectiveParams） |
| 反问超时自动继续 | ❌ | P6 候选：request_input 加倒计时默认项 |
| 用量上限熔断 | ✅ | 单次 run token 上限熔断收尾 |

### 2.3 上下文与知识

| ZCode 功能 | WorkBaby 现状 | 说明 |
|---|---|---|
| AGENTS.md 项目指令（全局 + workspace） | ✅（本轮补齐） | `%APPDATA%/WorkBaby/AGENTS.md` + `<workspace>/AGENTS.md`，
  每轮 run 注入 system（先全局后工作区），超 8k rune 截断 |
| @ 引用文件 / 文件夹 | ✅ | MentionPicker + 附件 + 工作区文件树拖拽引用 |
| $ 技能 | ✅ | SkillPicker + skill 元数据按预算注入 |
| # 关联历史会话 | 🟡 | /resume 跨会话恢复已有；输入框 # 会话引用 P6 候选 |
| Memory 项目记忆（自动提炼 + 自动带入） | ✅ | run 终局抽取（facts/procedures/skills 候选）→ 人审收件箱 → 合入记忆；
  新会话自动注入（capability Preload） |
| 知识库 / RAG | ✅ | kdocs 上传 / 切分 / 向量检索 + 检索代理注入 |
| Wiki 仓库架构导读 | ❌ | P6 候选：以工作流实现（LLM 按目录展开读码 → 生成导读 Markdown，带源码位置回链） |

### 2.4 扩展体系

| ZCode 功能 | WorkBaby 现状 | 说明 |
|---|---|---|
| MCP 服务器（stdio/http/sse + 启停） | ✅ | McpServersView + 内置运行时（node/python/pwsh 开箱即用） |
| 子智能体（内置 + 自定义定义文件） | ✅ | 内置 default/coding/research/writer + delegate 委派（同参去重、用量落库）；
  自定义子智能体已落地：设置页「子智能体」CRUD（agent_profiles 表）→ AgentProfileService 启动/写后同步进 harness 注册表 → delegate_task 按名委派、会话 /agent 与后台任务均可指定 |
| 斜杠命令 /（内置 + 保存提示词） | 🟡 | 内置 14 条（含本轮 /goal）；用户自定义命令保存 P6 候选 |
| Hooks（事件钩子） | 🟡 | harness 多槽钩子（进程内）已有；ZCode 式子进程协议 + 设置页管理 P6 候选 |
| 插件市场 | ❌ | 定位外：技能 + MCP + 命令已覆盖同能力面；市场分发不考虑 |
| 浏览器自动化 | ❌ | P6 大项：需内置 Chromium 驱动（CDP）+ 右侧浏览器面板；工作量≈2-3 周 |
| 定时任务 | ✅ | cron + 重试/错峰策略 + 运行日志 + keep-awake（D5-8/D6-7） |
| 闲时任务 | ❌（不抄） | 依赖云端套餐配额语义，单机无此概念 |
| 通知通道 | ✅ | 邮件 / Webhook 通道 + 熔断状态 |

### 2.5 任务与文件管理

| ZCode 功能 | WorkBaby 现状 | 说明 |
|---|---|---|
| 任务列表（分组/平铺/时间线 + 批量删除） | ✅ | 左栏分组（按时间）/平铺切换 + 内容搜索 + 批量删除 |
| 置顶 / 归档 / 自定义分组 / 拖拽 | ❌ | P6 候选（置顶 + 归档优先，分组其次） |
| 工作区文件树（变更过滤 + Git 状态） | 🟡 | 文件树 + 变更列表已有；Git 状态标识 / 分支选择器 P6 候选 |
| 命令中心（Ctrl+K 全局快搜） | ✅ | CommandPalette（会话 / 页面 / 快捷动作） |
| 使用统计 | ✅ | 仪表盘（趋势三线 / 模型排行 / 活跃热力） |

## 3. 补齐路线

### P5（本轮，2026-09-13 已落地）
- [x] UI 改版：ZCode 式外壳 / 主题 / 设置收敛
- [x] 目标模式 /goal（后端校验 + 自动续跑 + 前端目标卡）
- [x] AGENTS.md 注入（全局 + workspace）
- [x] 计划模式 UI 化（restricted 更名 + 语义对齐 + /trust 四档循环）
- [x] 划选引用追问

### P6（候选，按价值排序）
1. 浏览器自动化（内置 Chromium + CDP 驱动 + 右侧面板）——工作量最大，单独排期
2. ~~自定义子智能体~~ ✅ 已落地（设置页 CRUD → agent_profiles 表 → harness 注册表 → delegate_task//agent/后台任务）
3. Wiki 仓库导读（工作流实现：读码 → 目录 → 分页正文 + 源码回链）
4. 辅助对话（右栏并行小对话，继承主会话历史）
5. 会话置顶 / 归档；输入框 # 会话引用
6. 用户自定义斜杠命令（保存提示词）
7. Hooks 子进程协议 + 设置页管理
8. Git 面板（分支选择器 / 变更过滤）

### 明确不做
- 闲时任务（云端配额语义）、Remote Control / 手机端、远程开发（SSH/WSL/Docker）、插件市场

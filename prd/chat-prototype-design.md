# 聊天区原型与现有前端的差异总结

> 对照物：`prd/chat-prototype.html` ↔ `frontend/src/src/components/chat/*`、`App.vue`、`wb-ui.css` / `themes.css`。
> 范围：仅聊天输入框 + 消息列表（及其直接上下文：外壳、会话侧栏、聊天头）。
> 更新时间：2026-09-09。

## 一、设计系统与整体结构：一致 ✅

以下各点原型与现有前端**完全对齐**，列在这里是为了明确"未改动"边界：

- 颜色：`themes.css` 明暗两套 token 逐值一致（暖纸白 `#f7f7f5` / 深炭 `#101114`，单主色靛蓝，无渐变、无额外色值）；`data-theme` 挂 `<html>`，与 `useTheme.setTheme` 行为一致。
- 组件基元：`.msg-u / .bubble / .think / .tl / .tool / .approve / .composer-in / .mini / .send` 等照 `wb-ui.css` 还原。
- 应用外壳：标题栏（frameless + winctl）+ 侧栏四组导航 + 会话区（侧栏 40%）+ 聊天主区，结构同 `App.vue`。
- 聊天头：mascot + 标题 + 状态行（空闲绿点 / 流式主色脉动 + "执行中…"）+ 模型徽标 + 工作区 chip + 右侧面板入口 + 焦点模式，同 `ChatView.vue` header。
- 消息形态全集：历史压缩提示、用户气泡、思考块（134px 固定视口、正文开始自动折叠）、工具时间线（状态图标/耗时/args+result 折叠/展开全部）、文件变更摘要、用量徽标、审批卡、补充输入卡、停止 banner、流式光标。
- 输入框三层结构：条带（队列/技能提及/附件）→ textarea + 斜杠/@ 浮层 → 工具行（模型/权限/参数/技能/附件/入队/字数/上下文环/发送⇄停止）。
- 中途交互语义：流式中发送=入队、回合结束自动 flush、停止→stopped 终态 + 检查点续跑/重新生成、审批三态（批准/拒绝/跳过 + 本会话记住）。
- 用量徽标行内格式 `↑ 3.2k · ↓ 1.4k · 缓存命中 72% · 6.8s`，与 `UsageBadge.vue` 的 `fmtUsage` 一致。

## 二、原型新增 / 强化的设计（现前端没有，建议评估回移）

| # | 差异点 | 原型做法 | 现前端现状 | 建议 |
|---|---|---|---|---|
| 1 | **输入框 token 视觉化** | textarea 镜像高亮：`@技能`（淡紫）/`@文件·文件夹`（淡天蓝）//命令（淡靛蓝）实时渲染为淡色气泡，未知 `@xxx` 灰气泡提示未命中 | 仅 `@` 后的纯文本 token + 输入容器上方的 skill-mention chip 条带；文本本身无任何视觉区分 | **建议回移**。镜像高亮与原生 textarea 完全兼容（文字透明 + caret 保留），不影响 IME / 自增高 / 光标解析；能让"这条消息带了什么上下文"在输入时就可见 |
| 2 | **消息内 token 气泡** | 用户气泡内的 `@xxx` 渲染为白 20% 底小气泡（`.utk`） | `MessageItem` 用户气泡为纯文本 `whitespace-pre-wrap` | **建议回移**。与 1 配套，消除"看起来像手动输入"的问题；正则与后端 Skill 匹配的 token 规则保持同源即可 |
| 3 | **执行过程时间线的「技能命中行」** | 技能命中作为时间线首行（紫色闪电图标、`skill: code-review`、可展开查看注入的指令/放开工具/匹配来源）；发送内容 @ 了已知技能时自动插入 | `TaskTimeline` 只有工具调用；技能命中发生在 `buildSystem` 注入，前端完全不可见 | **建议回移**。叙事顺序变成：技能命中 → 工具 → 回答，因果清晰；需后端在 run 事件流中补一条 `chat:skill` 事件（或复用 tool 块落库） |
| 4 | **工具行「重试」元素类型** | `<span class="retry">`（+ `stopPropagation`） | `TaskTimeline.vue` 中 retry 是嵌在 `.tool-hd`（button）内的 `<button>` —— HTML 不允许按钮嵌套，浏览器会提前闭合外层按钮，可能破坏行结构与折叠交互 | **建议修复现前端**：改为 span 或将整行改为 div + role=button |
| 5 | **斜杠命令执行反馈规范化** | 每条命令执行后统一 `已执行 /命令 + 具体结果` 的 success toast；`/focus` `/help` `/clear` `/compact` `/model` `/trust` 均有真实可见效果 | `pickSlash` 的反馈散落（info toast / 静默跳页 / default 分支只提示命令名） | **建议回移**。低学习成本，逐条补齐即可 |
| 6 | **消息内错误工具行的展开/收起** | 错误行与成功行行为完全一致（手动开合） | `TaskTimeline` 有「异常自动展开」偏好（localStorage `wb.toolTimelineAutoOpen`），用户显式收起后仍可能被下一轮强展开 | 两者取向不同：现前端是"异常默认可见"，原型是"完全手动"。**保留现前端策略，但确认「收起全部」对 error 行生效**（现前端 manual map 已处理，无需改） |
| 7 | **会话侧栏搜索** | 侧栏会话区带实时过滤搜索框 | `SessionSidebar` 有树形血缘 + 时段分组 + 多选批量删除，但搜索框在组件内部与否以实现为准（原型有常驻搜索） | 视现前端实际情况补齐；原型交互值得保留 |

## 三、原型简化 / 省略的（现前端有，原型未实现）

按"对原型目标（评审交互形态）的影响"排序：

### 3.1 流式链路（原型为定时器演出，真实链路在 store）

- SSE 管线：`normalize → ChatStreamDecoder → StreamEventBatcher` 一帧一渲染；断线指数退避重连、30s 心跳 watchdog、`chat:retry` 建流重试提示条（原型有该条静态样式但未接逻辑）。
- `chat:done` 后拉权威快照替换流式气泡（消除"流式 + 权威"重影）；`chat:gap` 转全量回补。原型直接在计时器结束时收尾。
- 消息虚拟渲染窗口 + 滚动锚定（`MessageList` 的 pinned：上翻阅读历史不强制拉底）。
- 中途插话 steer（不打断 run、下一轮前注入）与排队 enqueue 是两条路径；原型只演了 enqueue。

### 3.2 数据与状态（原型为写死的静态数据）

- 模型选择器：真实模型列表（alias、provider）、熔断状态实时（`circuit-states`）、会话模型与所选不一致的提示（`sessionModelMismatch`）。
- 权限档位：会话级持久化（`permission_mode`），切换失败回滚 + 风险警告。
- 采样参数：后端三层合并的生效快照（`/chat/sessions/:id/params`），非本地滑杆。
- 上下文占用：`/usage/context` 权威分段 + 流式实测覆盖 + 本地估算兜底；**顶栏 `ContextRing` 与输入框双入口**（原型只有输入框一处环，聊天头缺环）。
- 输入上限：`kv/chat.maxInputChars` 动态拉取（默认 32000）；原型写死。
- 会话侧栏：树形血缘分支（parent_id 缩进 + 分支标签）、多选批量删除、加载态。

### 3.3 能力块（原型未渲染）

- `ArtifactCard` 交付成果卡片（present_files）；GenUI 内联渲染（`wb-genui` 调色板重映射）。
- `TodoProgressCard` 会话任务计划卡。
- 右侧工作区 / 文件变更 / 任务中心三面板（原型只有 header 入口 + toast 占位）。
- 桌宠陪伴体 `PetCompanion`、`ChatBackdrop` 自定义聊天背景。
- 附件真实链路：Wails `OpenFileDialog`/`UploadFile`、拖拽与剪贴板粘贴图片（原型为随机假附件）；`@文件` 提及自动转附件的真实行为已对齐。
- Markdown 渲染：`markdown-it` + DOMPurify + 终态异步增强（代码块装饰 / KaTeX / Mermaid）、超长代码限高滚动；原型为极简 `md()`（仅覆盖演示所需语法）。

### 3.4 全局体系

- i18n 中英双语（原型中文硬编码；侧栏底部语言切换为摆设）。
- 快捷键体系：Ctrl+N 新会话 / Ctrl+K 命令面板 / Ctrl+/ 聚焦输入 / Ctrl+B 折叠侧栏 / Ctrl+Shift+F 焦点模式（原型仅按钮，无键盘绑定）。
- 双主机 bootstrap、契约版本校验、启动错误态（静态页不适用）。
- `useToast` / `useDialog` 统一封装（原型自写 toast；删除等危险操作无二次确认弹窗，直接执行 + toast）。

## 四、纯视觉差异（无行为影响，落地时以现前端为准）

| 位置 | 原型 | 现前端 |
|---|---|---|
| 用量徽标容器 | 裸文本 span | 圆角胶囊描边（`rounded-full border bg-wb-surface`）+ 缓存命中率圆点（≥50% mint / 否则 warning）+ live 呼吸点 |
| 用量 tooltip | 单行 title | 多行明细（含「本轮」口径标注、缓存未命中提示 `usageCacheCold`） |
| 审批卡 / 停止 banner 挂载位置 | 内嵌在消息 bubble 内 | 独立组件在 MessageList 层级（`ApprovalInline` / `StopReasonBanner`），与消息并列 |
| @提及面板 | 增加了「技能 / 文件夹 / 文件」分组标题 | `MentionPicker` 平铺列表 + 类型图标 |
| 技能选择器 | 无搜索框 | `SkillPicker` 内置搜索 |
| 工具时间线分组 | 单层平铺 | 相同（均单层）；`delegate_task` 子 Agent 卡片原型仅在样式类中预留 |
| 字体 | 系统栈（未带 Inter 离线字体） | 离线 Inter + JetBrains Mono（`@font-face`） |

## 五、落地建议（优先级）

1. **修 bug**：`TaskTimeline.vue` 工具行「重试」按钮嵌套问题（二-4）。
2. **低成本高感知**：输入框 token 镜像高亮 + 用户消息 token 气泡（二-1/2）；斜杠命令反馈规范化（二-5）。
3. **需要后端配合**：技能命中事件进执行过程时间线（二-3，新增 `chat:skill` 事件或落 `message_blocks`）。
4. **视觉对齐**：用量徽标容器样式、@提及面板分组（四-1/4）。
5. 其余三-3.x 能力块与全局体系不在本次原型范围，维持现前端实现。

# API 契约总表

业务接口走 HTTP（gin），统一前缀 `/api/v1`，仅使用 GET / POST。系统能力（文件对话框、剪贴板、窗口、托盘）走 Wails 绑定，不在此表。

## 统一约定

- 响应包裹：成功 `{code:0, data:...}`；失败 `{code:<错误码>, message:<文案>, detail?}`。
- 请求体统一 JSON 绑定（类型不符即返回参数错误）；分页/条数参数由各接口的 `limit` / `k` 查询参数控制。
- 契约版本：`GET /api/v1/meta/contract`；前端启动比对，不一致显式报错。
- 错误码按域分段（示例）：2000 段持久化与路径、3000 段 LLM、4000 段工具与审批、5000 段会话与内核、6000 段记忆、8000 段技能、9000 段运行时与脚本。新增错误复用所属段位。

## 端点

### 元信息与运维

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /meta/version | 版本 |
| GET | /meta/health | 健康检查 |
| GET | /meta/contract | 契约版本 |
| GET | /meta/runtime | 内置运行时状态 |
| GET | /admin/overview | 后台概览（各域计数） |
| POST | /admin/cleanup-token-usages | 清理上游误报的缓存 token 明细 |
| GET | /docs | 文档列表 |
| GET | /docs/:name | 文档内容 |
| GET | /dashboard/stats | 仪表盘统计 |
| GET | /dashboard/trend | 会话/消息/token 趋势 |
| GET | /dashboard/token-trend | token 三线趋势（today/week/month/custom） |
| GET | /events | **SSE 事件流**（`scope`、`runId`、`last_event_id`） |
| GET | /files/*filepath | 受管文件静态服务 |

### 会话与消息

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /chat/sessions | 会话分页列表 |
| POST | /chat/sessions | 新建会话 |
| GET | /chat/sessions/:id | 会话详情 |
| POST | /chat/sessions/:id/rename | 重命名 |
| POST | /chat/sessions/:id/delete | 删除 |
| POST | /chat/sessions/delete-batch | 批量删除 |
| POST | /chat/sessions/:id/clear | 清空消息 |
| GET | /chat/sessions/search | 跨会话检索 |
| GET | /chat/sessions/:id/params | 生效参数与来源 |
| POST | /chat/sessions/:id/model | 切换模型 |
| POST | /chat/sessions/:id/permission | 切换权限模式 |
| POST | /chat/sessions/:id/workspace | 绑定工作区 |
| GET | /chat/sessions/:id/messages | 消息分页 |
| POST | /chat/messages/:sid/delete/:id | 删除单条 |
| POST | /chat/messages/:sid/truncate | 截断到指定消息 |
| POST | /chat/messages/:sid/fork | 从指定消息分叉 |
| GET | /chat/sessions/:id/todos | 待办状态 |
| POST | /chat/sessions/:id/todos/:itemID/toggle | 勾选待办 |
| POST | /chat/sessions/:id/compact | 压缩归档历史 |
| GET | /chat/sessions/:id/usage/context | 上下文占用分段 |
| GET | /chat/commands | 可用斜杠命令 |

### 流式运行

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | /chat/stream | 建 run 并开始流式生成 |
| POST | /chat/stream/:id/cancel | 中断当前 run |
| POST | /chat/stream/:id/resume | 从检查点续跑 |
| POST | /chat/sessions/:id/steer | run 中插入指令 |
| GET | /chat/runs | 运行历史列表 |
| GET | /chat/runs/:id/events | 某次运行的事件明细 |

### 审批与任务

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | /chat/approval/:id/decide | 审批决策（批准 / 本会话允许 / 拒绝） |
| POST | /chat/approval/:id/answer | 补充输入答复 |
| POST | /chat/approval/:id/skip | 跳过补充输入 |
| GET | /chat/approvals/pending | 未决审批（刷新恢复） |
| POST | /tasks | 提交后台任务 |
| GET | /tasks | 任务列表（可按 session_id） |
| GET | /tasks/:id | 任务详情 |
| POST | /tasks/:id/cancel | 取消任务 |

### 工作区、变更与工件

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /trust | 信任列表 |
| GET | /trust/resolve | 解析某路径信任态 |
| POST | /trust | 登记信任 |
| POST | /trust/revoke | 撤销信任 |
| GET | /trust/roots | 恒信任根列表 |
| GET | /chat/workspace/:id/files | 工作区文件树 |
| GET | /chat/workspace/:id/ls | 目录列举 |
| GET | /chat/workspace/:id/file | 读文件 |
| POST | /chat/workspace/:id/rename | 重命名 |
| POST | /chat/workspace/:id/copy | 复制 |
| POST | /chat/workspace/:id/remove | 删除 |
| GET | /chat/sessions/:id/changes | 文件变更列表（含 diff） |
| GET | /chat/changes/:cid | 单条变更详情 |
| POST | /chat/changes/:cid/rollback | 回滚变更 |
| GET | /chat/sessions/:id/artifacts | 产物列表 |
| POST | /chat/artifacts/:aid/delete | 删除产物 |

### 模型 Provider 与设置

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /ai-provider | Provider 列表 |
| POST | /ai-provider | 新建 |
| GET | /ai-provider/available | 可用模型（供会话选择） |
| GET | /ai-provider/kinds | 支持的协议类型 |
| GET | /ai-provider/:id | 详情 |
| POST | /ai-provider/:id/update | 更新 |
| POST | /ai-provider/:id/delete | 删除 |
| POST | /ai-provider/:id/test | 连通性测试 |
| POST | /ai-provider/reload | 从 model.json 热重载 |
| GET | /ai-provider/circuit-status | 就绪 / 熔断状态 |
| POST | /ai-provider/:id/reset-circuit | 重置并重建 |
| GET | /settings | 全量设置 |
| GET/POST | /settings/general | 通用设置 |
| GET/POST | /settings/smtp | 邮件配置 |
| GET/POST | /settings/websearch | 搜索配置 |
| GET/POST | /settings/exec/agent | exec 命令白名单 |
| GET/POST | /kv/:key | 任意 KV 读写 |

### 技能与 MCP

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /skills | 技能列表 |
| POST | /skills | 新建/导入（粘贴 SKILL.md） |
| POST | /skills/import-zip | zip 批量导入（回执含失败清单） |
| POST | /skills/:name/enabled | 启停 |
| POST | /skills/:name/update | 更新（内置只读） |
| POST | /skills/:name/delete | 删除（内置只读） |
| GET | /mcp/servers | MCP server 列表（env 掩码） |
| GET/POST | /mcp/servers/raw | 读取/写入 mcp.json |
| POST | /mcp/servers | 新增 |
| POST | /mcp/servers/:name/enabled | 启停 |
| POST | /mcp/servers/:name/delete | 删除 |
| POST | /mcp/servers/reload | 重载 |
| POST | /mcp/servers/reveal | 定位配置文件 |

### 工具

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /tools | 工具列表（含启用状态与 schema） |
| POST | /tools/:name/enabled | 启停工具 |

### 工作流

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /workflows/node-types | 节点类型清单 |
| GET | /workflows | 列表 |
| POST | /workflows | 新建 |
| GET | /workflows/:id | 详情 |
| GET | /workflows/:id/graph | DAG 结构 |
| POST | /workflows/:id/update | 更新元信息 |
| POST | /workflows/:id/update-graph | 更新 DAG |
| POST | /workflows/validate | 校验图 |
| POST | /workflows/:id/delete | 删除 |
| POST | /workflows/:id/run | 运行 |
| GET | /workflows/:id/executions | 执行历史 |
| GET | /executions/:id | 执行详情 |
| POST | /executions/:id/cancel | 取消 |
| POST | /executions/:id/pause | 暂停 |
| POST | /executions/:id/resume | 恢复 |
| POST | /executions/:id/input | 提交人工输入 |

### 知识库与记忆

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /kdocs | 文档列表 |
| POST | /kdocs | 新增文档 |
| POST | /kdocs/import-file | 导入本地文件 |
| GET | /kdocs/search | 检索 |
| GET | /kdocs/groups | 分组 |
| GET | /kdocs/group/:group | 按分组列文档 |
| GET | /kdocs/:id | 详情 |
| POST | /kdocs/:id/update | 更新 |
| POST | /kdocs/:id/delete | 删除 |
| POST | /kdocs/:id/reindex | 重建索引 |
| GET | /memory/search | 记忆检索 |
| GET | /memory/recall | 跨三类融合召回 |
| GET/POST | /memory/episodes | 情景列表 / 写入 |
| POST | /memory/episodes/:id/delete | 删除情景 |
| GET/POST | /memory/facts | 语义列表 / 写入 |
| POST | /memory/facts/:id/delete | 删除语义 |
| GET/POST | /memory/procedures | 程序列表 / 写入 |
| POST | /memory/procedures/:id/delete | 删除程序 |
| GET | /memory/stats | 三类计数 |
| GET | /memory/long-term/:id | 长期记忆全文 |

### 通道、定时任务、桌宠、文件夹与文件

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /channels | 通道列表 |
| POST | /channels | 新建 |
| POST | /channels/:id/update | 更新 |
| POST | /channels/:id/delete | 删除 |
| POST | /channels/:id/start | 启动 |
| POST | /channels/:id/stop | 停止 |
| POST | /channels/:id/test | 测试发送 |
| GET | /channels/:id/messages | 收发日志 |
| GET | /cron/jobs | 定时任务列表 |
| POST | /cron/jobs | 新建 |
| POST | /cron/jobs/:id/update | 更新 |
| POST | /cron/jobs/:id/delete | 删除 |
| POST | /cron/jobs/:id/trigger | 立即触发 |
| GET | /pet/state | 桌宠状态 |
| GET | /pet/config | 桌宠配置 |
| POST | /pet/config/update | 更新配置 |
| GET | /pet/sprites | 形象列表 |
| POST | /pet/sprites | 新增形象 |
| POST | /pet/sprites/:id/delete | 删除形象 |
| POST | /pet/window/:mode | 切换形态（pet / main） |
| GET | /folders/tree | 文件夹树 |
| GET | /folders | 按父级列文件夹 |
| POST | /folders | 新建文件夹 |
| POST | /folders/:id/update | 更新 |
| POST | /folders/:id/delete | 删除 |
| GET | /files/search | 搜索文件 |
| GET | /files | 文件列表 |
| POST | /files/upload | 上传（登记本地文件） |
| POST | /files/:id/delete | 删除 |
| GET | /files/:id/preview-url | 预览地址 |

## SSE 事件

订阅：`GET /api/v1/events?scope=<前缀>&runId=<运行 id>&last_event_id=<seq>`。帧格式：`id: <seq>`、`event: <名称>`、`data: <JSON>`；`seq` 由服务端按 run 单调分配，支持断线重放；重放窗口不足时下发 `chat:gap` 转全量快照。

| 事件 | 载荷要点 |
|---|---|
| `sse-ready` | 连接就绪（重放完成） |
| `ping` | 心跳（前端 watchdog 续期） |
| `chat:stream` | 正文增量 |
| `chat:thinking` | 思维链增量 |
| `chat:stats` | 本轮用量（input/output/cache/total/latency） |
| `chat:skill` | 命中技能（名称/来源/版本/工具白名单/注入字数） |
| `chat:tool` | 工具调用开始（id/name/arguments/agent/activity） |
| `chat:tool-result` | 工具结果（id/name/content/error/duration_ms/agent） |
| `chat:approval` | 审批请求（id/command/reason/risk/can_remember） |
| `chat:approval-decided` | 审批决策 |
| `chat:subagent-start` / `-done` / `-error` | 子 Agent 生命周期 |
| `chat:todo` | 待办状态变化 |
| `chat:file-change` | 文件变更（含 diff 信息） |
| `chat:artifact` | 产物登记 |
| `chat:warn` | 告警（越界写入；反幻觉核验 `kind=unbacked_claim`） |
| `chat:compressed` | 自动压缩发生（移除轮次、恢复引用） |
| `chat:context-trimmed` | system 段被预算裁剪（被丢段、预算） |
| `chat:retry` | 建流瞬时错误自动重试提示 |
| `chat:error` | 错误（code/message） |
| `chat:done` | 终态（status/reason/stop_reason/message_id/usage） |
| `chat:gap` | 重放窗口失效 → 前端拉全量快照 |
| `task:created` / `task:started` / `task:done` | 后台任务生命周期 |

## Wails 绑定（非 HTTP）

系统能力经 Wails 绑定暴露：文件/目录选择对话框、剪贴板、窗口形态切换与置顶、系统托盘、受管图片代读、`app:ready` 与 `app:*` 生命周期事件（含 `server_port` 与数据根路径）。

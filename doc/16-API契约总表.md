# API 契约总表

## 通用约定

| 项 | 说明 |
|---|---|
| 基础 URL | `http://127.0.0.1:{port}/api/v1`（端口由 `GetServerPort()` 绑定或 `app:ready` 事件注入，缓存于 `localStorage('wb.serverPort')`） |
| 响应信封 | `{code: 0, message: "ok", data: …}`；`code !== 0` 抛错，`message` 直接可展示 |
| 时间戳 | 毫秒整数（int64），字段名 `*_at` |
| 字段命名 | snake_case |
| 流式事件 | `GET /api/v1/events?scope=chat&runId=…`（SSE），断线带 `last_event_id` |
| 文件预览 | `GET /files/sprites/{id}`、`/files/files/{id}`、`/files/workspace/{sid}?path=…` |
| 错误码段位 | 1000–1999 通用 · 2000–2999 持久化 · 3000–3999 LLM · 4000–4999 工具 · 5000–5999 Harness · 6000–6999 记忆 · 7000–7999 RAG · 8000–8999 Skill/MCP · 9000–9999 Workflow/Pet/Channel/Cron |
| 契约版本 | `GET /meta/contract`，前端启动时与内置常量比对 |

## 系统与元信息

| 端点 | 说明 |
|---|---|
| `GET /meta/version` / `health` / `contract` / `runtime` | 版本、健康、契约版本、内置运行时状态 |
| `GET /admin/overview` | 管理总览 |
| `GET /docs` / `docs/:name` | 内置文档 |
| `GET /dashboard/stats` / `trend` / `token-trend` | 仪表盘统计与趋势 |
| `GET /events` | SSE 事件流（scope=chat/task/workflow/pet/app） |

## 会话与消息

| 端点 | 说明 |
|---|---|
| `GET/POST /chat/sessions` | 会话列表 / 创建（可携带 `workspace_path` 创建即绑定） |
| `GET /chat/sessions/:id` / `POST …/rename` / `…/delete` / `POST /chat/sessions/delete-batch` | 详情与维护 |
| `POST /chat/sessions/:id/model` / `permission` / `workspace` | 切模型 / 权限档 / 绑定工作区 |
| `GET /chat/sessions/:id/params` / `usage/context` | 生效参数快照 / 上下文分段占用 |
| `GET /chat/sessions/:id/messages` / `POST …/clear` | 消息列表 / 清空 |
| `POST /chat/stream` | 发起对话（返回 `run_id`），流式事件走 SSE |
| `POST /chat/stream/:id/cancel` / `…/resume` | 停止 / 断点续跑 |
| `POST /chat/sessions/:id/steer` | 中途插话 |
| `GET /chat/sessions/:id/todos` | 会话计划 |
| `POST /chat/messages/:sid/delete/:id` / `…/truncate` / `…/fork` | 删除 / 截断 / 分叉 |
| `POST /chat/sessions/:id/compact` | 手动压缩（保留指示 + 保留窗口） |
| `GET /chat/sessions/search` / `GET /chat/commands` | 搜索 / 斜杠命令元数据 |
| `GET /chat/runs` / `GET /chat/runs/:id/events` | 执行记录 / 事件导出 |

## 审批与信任

| 端点 | 说明 |
|---|---|
| `GET /chat/approvals/pending` | 待决审批 |
| `POST /chat/approval/:id/decide` / `…/answer` / `…/skip` | 批准或拒绝 / 补充输入回复 / 跳过 |
| `GET /trust` / `trust/resolve` / `trust/roots` | 信任列表 / 某路径解析态 / 恒信任根 |
| `POST /trust` / `trust/revoke` | 授权 / 撤销 |

## 工作区与文件

| 端点 | 说明 |
|---|---|
| `GET /chat/workspace/:id/files` / `ls` / `file` | 文件树 / 目录列表 / 读文件 |
| `POST /chat/workspace/:id/rename` / `copy` / `remove` | 重命名 / 复制 / 删除 |
| `GET /chat/sessions/:id/changes` / `GET /chat/changes/:cid` | 文件变更列表 / diff 详情 |
| `POST /chat/changes/:cid/rollback` | 回滚一次写入 |
| `GET /chat/sessions/:id/artifacts` / `POST /chat/artifacts/:aid/delete` | 产出物登记 |

## 任务与模型

| 端点 | 说明 |
|---|---|
| `GET/POST /tasks`、`GET /tasks/:id`、`POST /tasks/:id/cancel` | 后台任务队列 |
| `GET/POST /ai-provider`、`GET /ai-provider/available` / `kinds` | 供应商 CRUD / 可用模型 / 协议类型 |
| `POST /ai-provider/:id/update` / `delete` / `test` / `reload` | 维护与连通性测试 |
| `GET /ai-provider/circuit-status` / `POST /ai-provider/:id/reset-circuit` | 熔断状态与重置 |

## 设置

| 端点 | 说明 |
|---|---|
| `GET /settings`、`GET/POST /settings/general` | 系统设置键值 / 常规项 |
| `GET/POST /settings/smtp` / `settings/websearch` / `settings/exec/agent` | SMTP / 搜索 / exec 白名单 |
| `GET/POST /kv/:key` | 单键读写 |

## 能力域

| 端点 | 说明 |
|---|---|
| `GET/POST /skills`、`POST /skills/import-zip`、`POST /skills/:name/enabled` / `update` / `delete` | 技能管理 |
| `GET /mcp/servers`、`GET/POST /mcp/servers/raw`、`POST /mcp/servers`、`…/:name/enabled` / `delete`、`POST /mcp/servers/reload` / `reveal` | MCP 管理（raw 为 JSON 直编 + 校验回滚） |
| `GET /tools`、`POST /tools/:name/enabled` | 工具清单与启停 |
| `GET /workflows`（含 `node-types` / `:id/graph`）、`POST /workflows`、`…/:id/update` / `update-graph` / `delete` / `validate` / `:id/run` | 工作流定义与校验 |
| `GET /workflows/:id/executions`、`GET /executions/:id`、`POST /executions/:id/cancel` / `pause` / `resume` / `input` | 执行明细与人工干预 |
| `GET /kdocs`（含 `search` / `groups` / `group/:group`）、`POST /kdocs` / `import-file`、`POST /kdocs/:id/update` / `delete` / `reindex` | 知识库 |
| `GET /memory/episodes` / `facts` / `procedures` / `stats` / `recall` / `search` / `long-term/:id`、`POST …/delete` | 记忆中心 |
| `GET/POST /channels` 等 | 通知通道配置与发送日志 |
| `GET /cron-jobs` 等 | 定时任务 CRUD 与手动触发 |
| `GET /api/v1/pet/*`、`POST /pet/window/:mode` | 桌宠配置 / 形象 / 窗口形态 |

## SSE 事件清单

| 事件 | 载荷要点 |
|---|---|
| `chat:stream.start` | 模型、是否续跑 |
| `chat:stream` / `chat:thinking` | 正文 / 思维链增量 |
| `chat:tool` / `chat:tool-result` | 工具调用与结果（id / name / args / state / duration） |
| `chat:stats` | 本轮用量（input / output / cache_read / total / latency） |
| `chat:approval` / `chat:approval-decided` | 审批请求与裁决 |
| `chat:compressed` | 自动压缩（折叠条数） |
| `chat:file-change` / `chat:artifact` | 文件变更 / 产出物登记 |
| `chat:subagent-start` / `-done` / `-error` | 子 Agent 生命周期 |
| `chat:done` / `chat:error` / `chat:stopped` | 终态（含 stop_reason） |
| `chat:gap` | 重放窗口失效，前端转全量快照 |
| `chat:retry` | 建流瞬时错误自动重试 |
| `task:created` / `started` / `done` | 后台任务生命周期 |
| `workflow:*` / `pet:*` / `app:*` | 对应能力域事件 |
| `ping` | 30s 具名心跳（watchdog 依据） |

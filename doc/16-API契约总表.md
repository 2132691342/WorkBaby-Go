# 16 API 契约总表

> 业务 API 端点 + 前端类型 `Message` / `Session` / `Artifact` 等的字段表。所有字段 snake_case。

## 通用约定

| 项 | 说明 |
|---|---|
| 基础 URL | `http://127.0.0.1:{port}/api/v1`（port 由 `app:ready` 注入或 `wb.serverPort`） |
| 响应格式 | `{code: 0, message: "ok", data: {...}}` 或错误流（`pkg.AppError`） |
| 时间戳 | 毫秒整数（int64） |
| 流式事件 | `GET /api/v1/events?scope=chat&runId=...` SSE，详见 [04-Agent与Harness内核](./04-Agent与Harness内核.md) |
| 文件预览 | `GET /files/sprites/{id}` / `/files/files/{id}` / `/files/workspace/{sid}?path=...` |
| 错误码段位 | 1000–1999 通用 · 2000–2999 持久化 · 3000–3999 LLM · 4000–4999 工具 · 5000–5999 Harness · 6000–6999 记忆 · 7000–7999 RAG · 8000–8999 Skill/MCP · 9000–9999 Workflow/Pet/Channel/Cron |

## Chat（详见 [07-聊天与会话](./07-聊天与会话.md)）

| 端点 | 方法 | 请求 | 响应 |
|---|---|---|---|
| `/chat/sessions` | GET | `?page=&page_size=&agent=` | `{items: Session[], total: int}` |
| `/chat/sessions` | POST | `ChatSessionREQ` | `SessionRESP` |
| `/chat/sessions/:id` | GET | — | `SessionRESP` |
| `/chat/sessions/:id` | PATCH | `{name?, workspace_path?, permission_mode?}` | `SessionRESP` |
| `/chat/sessions/:id/delete` | POST | — | `{}` |
| `/chat/sessions/:id/fork` | POST | `{name, branch_point_seq}` | `SessionRESP` |
| `/chat/sessions/:id/clear` | POST | — | `{}` |
| `/chat/sessions/:id/messages` | GET | `?after_seq=&limit=` | `{items: MessageRESP[], total, next_seq}` |
| `/chat/sessions/:id/workspace` | POST | `{workspace_path}` | `SessionRESP` |
| `/chat/sessions/:id/permission` | POST | `{mode: restricted\|default\|auto_edit\|yolo}` | `SessionRESP` |
| `/chat/stream` | POST | `SendStreamREQ` | `SendStreamResult` |
| `/chat/stream/:runID/resume` | POST | — | `{run_id}` |
| `/chat/stream/:sid/cancel` | POST | — | `{}` |
| `/chat/sessions/:id/steer` | POST | `{content}` | `{queued, run_id}` |
| `/chat/messages/:sid/delete/:mid` | POST | — | `{}` |
| `/chat/compact` | POST | `{session_id, instructions?, keep_recent?}` | `CompactResultRESP` |

## Agent / Provider / Settings

| 端点 | 方法 |
|---|---|
| `/ai-provider` | GET / POST（创建）/ PATCH（更新） |
| `/ai-provider/:id` | GET / DELETE |
| `/ai-provider/:id/test` | POST（连通性测试） |
| `/ai-provider/available` | GET（已启用 provider + 模型聚合，用于模型选择器） |
| `/ai-provider/circuit-status` | GET |
| `/settings/general` / `/settings/:key` | GET / POST（合并保存） |
| `/auth/login` / `/auth/check` | 本机单用户，登录即绑定 |

## Tool / Skill / MCP

| 端点 | 方法 | 说明 |
|---|---|---|
| `/tools` | GET | 工具列表 + 元数据 |
| `/tools/:name/enabled` | POST | 启停 |
| `/skills` / `/skills/:name` | GET / POST / DELETE | Skill 列表 / 详情 / 上传 |
| `/mcp/servers` | GET / POST | MCP server 配置 + 启停 |
| `/mcp/servers/:id/test` | POST | 重启并握手测试 |

## Memory / Knowledge

| 端点 | 方法 |
|---|---|
| `/memory/episodes` | GET（按 session 列出）/ DELETE |
| `/memory/facts` | GET / POST / DELETE |
| `/memory/procedures` | GET / POST / DELETE |
| `/memory/search` | GET `?q=&top_k=` |
| `/knowledge/docs` | GET / POST（上传）/ DELETE |
| `/knowledge/search` | GET `?q=&top_k=&doc_ids=` |

## Workflow / Cron / Channel

| 端点 | 方法 |
|---|---|
| `/workflows` | GET / POST |
| `/workflows/:id` | GET / PATCH / DELETE |
| `/workflows/:id/run` | POST |
| `/workflows/:id/cancel` | POST |
| `/workflow/executions/:id` | GET |
| `/cron/jobs` | GET / POST / PATCH / DELETE |
| `/cron/jobs/:id/run` | POST（手动触发） |
| `/channels` | GET / POST / PATCH / DELETE |
| `/channels/:id/test` | POST（发送测试消息） |
| `/channels/executions` | GET |

## Pet

| 端点 | 方法 |
|---|---|
| `/pet/config` | GET / PUT |
| `/pet/sprites` | GET / POST（上传）/ DELETE |
| `/pet/state` | GET（前端 2.5s 轮询） |

## Dashboard

| 端点 | 方法 |
|---|---|
| `/dashboard/overview` | GET |
| `/dashboard/token-trend` | GET `?range=7d\|30d` |
| `/dashboard/runs` | GET `?page=&page_size=&status=` |

## 关键类型字段（节选）

### SessionRESP

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string | `SESSION_xxx` |
| `name` | string | 会话名 |
| `provider_id` | string | 当前 provider |
| `model` | string | 当前模型 |
| `workspace_path` | string | 绑定的工作区目录 |
| `permission_mode` | string | `restricted` / `default` / `auto_edit` / `yolo` |
| `parent_id` | string | 分叉来源会话（根为空） |
| `branch_point` | int64 | 分叉点 seq |
| `message_count` | int | — |
| `last_message_at` | int64 | 毫秒 |
| `created_at` / `updated_at` | int64 | 毫秒 |

### MessageRESP

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string | `MESSAGE_xxx` |
| `session_id` | string | — |
| `run_id` | string | 本消息所属 run |
| `seq` | int64 | 会话内单调 |
| `role` | string | `user` / `assistant` / `tool` |
| `content` / `thinking` | string | 正文 / 推理 |
| `tool_calls` | ToolCall[] | assistant 调用（JSON） |
| `tool_call_id` | string | tool 消息对应 id |
| `blocks` | MessageBlockRESP[] | 工具过程（前端 TaskTimeline 用） |
| `status` | string | `streaming` / `completed` / `failed` |
| `stop_reason` | string | `end_turn` / `length` / `cancelled` / ... |
| `model` | string | — |
| `input_tokens` / `output_tokens` / `cache_read_tokens` / `total_tokens` | int | 最终落库 |
| `latency_ms` | int64 | run 端到端耗时 |
| `created_at` / `updated_at` | int64 | 毫秒 |

### ArtifactPayload（chat:artifact 事件）

| 字段 | 说明 |
|---|---|
| `explanation` | 工具说明文本 |
| `files[]` | `{path, name, mime, size, url, kind}` |

### CompactResultRESP

| 字段 | 说明 |
|---|---|
| `compacted` | 本次压缩消息数 |
| `freed_chars` / `freed_tokens` | 释放的字符 / token |
| `pinned` | 是否触发了手动压缩（true）/ 自动（false） |

## 事件载荷字段表（`chat:*`）

| 事件 | 关键字段 |
|---|---|
| `chat:stream.start` | `{model, resumed?}` |
| `chat:stream` | `{delta}` |
| `chat:thinking` | `{delta}` |
| `chat:tool` | `{id, name, arguments, agent}` |
| `chat:tool-start` | `{id, name, agent}` |
| `chat:tool-result` | `{id, name, content, error?, duration_ms, agent?, refused?, data?}` |
| `chat:approval` | `{id, command, reason, risk}` |
| `chat:approval-decided` | `{id, decision}` |
| `chat:stats` | `{turn, input_tokens, output_tokens, cache_read_tokens, total_tokens, latency_ms}` |
| `chat:compressed` | `{removed_messages, summary_head}` |
| `chat:todo` | `{state: TodoStateRESP}` |
| `chat:file-change` | `{change: FileChange}` |
| `chat:artifact` | `{artifact: ArtifactPayload}` |
| `chat:subagent-start/done/error` | `{sub_run_id, agent, message?}` |
| `chat:error` | `{code, message, kind?}` |
| `chat:retry` | `{attempt, delay_ms, reason}` |
| `chat:gap` | `{run_id, last_seq}` |
| `chat:done` | `{status, reason, stop_reason, message_id, usage}` |

## 约束

- 所有 API 字段 snake_case，跨前后端对齐（CLAUDE.md §2.5）
- 所有时间戳 int64 毫秒
- 所有 ID ULID 大写带领域前缀（CLAUDE.md §2.7）
- SSE 事件带 seq，断线重连按 Last-Event-ID 重放

# 17 · API 契约总表

唯一真理源是 `internal/server/router.go`；本文与其一一对应，前端 `src/api/`、`types/api.ts` 必须与之一致。
规则：仅 GET/POST；路径风格 `/api/v1/{resource}/:id/{action}`；字段 snake_case；统一响应 `{code, message, data}`。

---

## 1. 统一约定

| 项 | 约定 |
|---|---|
| 基址 | `http://127.0.0.1:{serverPort}`（端口经 `app:ready` 注入） |
| 响应 | `{ code, message, data?, details? }`；`code=0` 成功；`1404` 未命中路由 |
| 方法 | 业务仅 `GET` / `POST`；删除一律 `POST .../delete` |
| 字段 | 下划线小驼峰（`message_count` / `user_message_id` / `api_key_masked`） |
| 静态资源 | `/files/**` 走 Wails AssetServer（sprite `/files/sprites/{id}`、托管文件 `/files/files/{id}`、工作区 `/files/workspace/{sessionId}?path=`），**不是** API |

---

## 2. 路由清单

### 2.1 meta / admin / dashboard

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/meta/version` | 版本 |
| GET | `/api/v1/meta/health` | 健康检查 |
| GET | `/api/v1/meta/runtime` | 运行时状态 |
| GET | `/api/v1/admin/overview` | 管理后台概览 |
| GET | `/api/v1/dashboard/stats` | 仪表盘统计快照 |
| GET | `/api/v1/dashboard/trend?range=7` | 最近 N 天会话/消息/token 趋势（N ≤ 90） |
| GET | `/api/v1/dashboard/token-trend?scope=today\|week\|month\|custom&start_at=&end_at=` | token 消耗三线图（见 §3） |
| GET | `/api/v1/docs` / `/api/v1/docs/:name` | 内置文档 |

### 2.2 events（SSE）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/events?scope=chat&runId={runId}` | SSE 长连接。事件：`chat:stream`(增量) / `chat:thinking` / `chat:stats`(每轮用量 input/output/cache_read/latency) / `chat:tool` / `chat:tool-result` / `chat:approval` / `chat:approval-decided` / `chat:done`(含领域 stop_reason) / `chat:error` / `chat:gap`(断线重放窗口覆盖) / `chat:todo` / `chat:file-change` / `chat:artifact` / `task:*`；心跳 `ping` |

### 2.3 chat / session

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/chat/sessions?page=1&page_size=20` | 会话分页（`{items, total}`） |
| POST | `/api/v1/chat/sessions` | 新建会话 `{name, model?, workspace_path?}`（workspace_path 非空时创建即绑定并自动信任） |
| GET | `/api/v1/chat/sessions/:id` | 会话详情 |
| POST | `/api/v1/chat/sessions/:id/rename` | 重命名 `{name}` |
| POST | `/api/v1/chat/sessions/:id/delete` | 删除会话 |
| POST | `/api/v1/chat/sessions/delete-batch` | 批量删除 `[]string` |
| POST | `/api/v1/chat/sessions/:id/workspace` | 设置会话工作区 `{workspace_id}` |
| GET | `/api/v1/chat/sessions/:id/messages?after_seq=0&limit=200` | 消息列表（增量游标；含持久化过程块） |
| POST | `/api/v1/chat/stream` | 发起流式 run `{session_id, content, temperature?, thinking_effort?}` → `{run_id, ...}` |
| POST | `/api/v1/chat/stream/:id/cancel` | 停止当前 run |
| POST | `/api/v1/chat/sessions/:id/steer` | run 进行中插入指令 `{content}` → `{run_id, message_id, queued}`；无活动 run 返回 5010 |
| POST | `/api/v1/chat/sessions/:id/clear` | 清空会话消息 |
| POST | `/api/v1/chat/messages/:sid/delete/:id` | 删除单条消息 |
| POST | `/api/v1/chat/messages/:sid/truncate` | 从某条起截断 `{message_id}` |
| POST | `/api/v1/chat/messages/:sid/fork` | 从某条 fork 出新会话（写入 `parent_id` / `branch_point` 会话树血缘） |
| POST | `/api/v1/chat/messages/:sid/truncate` | 从某条起截断（含该消息，即 rewind 语义）`{message_id}` |
| POST | `/api/v1/chat/approval/:id/decide` | 审批决策 `{approved}` |
| POST | `/api/v1/chat/sessions/:id/compact` | 手动压缩 `{instructions?, keep_recent?}` → `{compacted, freed_chars, freed_tokens, kept_recent, pinned, failed}` |
| GET | `/api/v1/chat/sessions/search?q=&scope=all\|title\|content&limit=` | 跨会话快速检索（/resume） |
| GET | `/api/v1/chat/sessions/:id/usage/context` | 上下文占用分段快照（/context） |

### 2.3.1 工作目录信任（decide / ask / deny 三态）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/trust` | 全部登记列表 |
| GET | `/api/v1/trust/roots` | 恒信任根（前端展示） |
| GET | `/api/v1/trust/resolve?path=` | 单目录解析（就近上溯命中登记） |
| POST | `/api/v1/trust` | 写入决策 `{path, state: allow\|ask\|deny}`（原子 upsert） |
| POST | `/api/v1/trust/revoke` | 撤销登记 `{path}` |

### 2.4 工作区文件

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/chat/workspace/:id/files` | 会话工作区文件列表 |
| GET | `/api/v1/chat/workspace/:id/file?path=` | 读工作区文件内容 |

### 2.5 ai-provider / settings / kv

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/api/v1/ai-provider` | 列表 / 创建 |
| GET | `/api/v1/ai-provider/available` | 可切换模型 |
| GET | `/api/v1/ai-provider/:id` | 详情 |
| POST | `/api/v1/ai-provider/:id/update` | 更新 |
| POST | `/api/v1/ai-provider/:id/delete` | 删除 |
| POST | `/api/v1/ai-provider/:id/test` | 连通性测试 |
| POST | `/api/v1/ai-provider/reload` | 从 model.json 热重载 |
| GET | `/api/v1/ai-provider/circuit-status` | 就绪状态（CLOSED=可用 / OPEN=构建失败含 reason / DISABLED=已禁用） |
| POST | `/api/v1/ai-provider/:id/reset-circuit` | 重置未就绪（重新构建实例） |
| GET/POST | `/api/v1/settings/general` | 通用设置 |
| GET/POST | `/api/v1/settings/smtp` | SMTP 配置（密码加密） |
| GET/POST | `/api/v1/settings/websearch` | 网络搜索配置 |
| GET/POST | `/api/v1/settings/exec/agent` | 命令执行白名单 |
| GET/POST | `/api/v1/kv/:key` | 运行时 KV（system_settings） |

### 2.6 skills / mcp / tools

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/api/v1/skills` | 列表 / 新建 |
| POST | `/api/v1/skills/import-zip` | 批量导入技能包 zip `{zip_path}` → `{imported, skipped, failed}` |
| POST | `/api/v1/skills/:name/enabled` | 启停 `{enabled}` |
| POST | `/api/v1/skills/:name/update` / `delete` | 更新 / 删除 |
| GET | `/api/v1/mcp/servers` | MCP server 列表 |
| GET/POST | `/api/v1/mcp/servers/raw` | mcp.json 原文读写 |
| POST | `/api/v1/mcp/servers` | 添加 server |
| POST | `/api/v1/mcp/servers/:name/enabled` / `delete` / `reload` / `reveal` | 启停 / 删除 / 重载 / 打开配置目录 |
| GET | `/api/v1/tools` | 工具列表 |
| POST | `/api/v1/tools/:name/enabled` | 工具启停 |

### 2.7 workflows / executions

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/api/v1/workflows` | 列表 / 创建 |
| GET | `/api/v1/workflows/node-types` | 节点目录：7 类节点 + 字段 schema（调色板/属性面板数据源） |
| GET | `/api/v1/workflows/:id` | 详情 |
| GET | `/api/v1/workflows/:id/graph` | 图（DAG wire：`nodes[{id,type,params,branch,pos}]` + `edges[{from,to,source_handle}]` + `outputs`） |
| POST | `/api/v1/workflows/:id/update-graph` | 保存可视化图（同上结构，双向无损） |
| POST | `/api/v1/workflows/:id/update` / `delete` / `run` | 元数据更新 / 删除 / 运行 → `{execution_id}` |
| POST | `/api/v1/workflows/validate` | 校验图 `{graph}` |
| GET | `/api/v1/workflows/:id/executions?limit=20` | 执行记录 |
| GET | `/api/v1/executions/:id` | 执行详情 `{execution, nodes[]}`（前端轮询 + 画布高亮） |
| POST | `/api/v1/executions/:id/cancel` / `pause` / `resume` | 取消 / 暂停 / 恢复（返回空，前端回拉详情刷新） |
| POST | `/api/v1/executions/:id/input` | 人工输入 `{value}` |

**分支语义**：condition 节点输出 `branch`（bool → `true`/`false`，字符串原样）；从 condition 源连出的边带 `source_handle`，保存时写入目标节点 `branch`，执行期据此决定是否激活；任一上游被跳过则整条支线整段跳过传播。

### 2.8 kdocs（知识库 / RAG）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/api/v1/kdocs` | 列表 / 新增 |
| POST | `/api/v1/kdocs/import-file` | 本地文件导入受管知识库 `{name, source_path, folder_id?}` → 复制到 `{home}/knowledge` 并后台索引 |
| GET | `/api/v1/kdocs/search?q=&limit=` | 检索 |
| GET | `/api/v1/kdocs/groups` / `/group/:group` | 分组 |
| GET | `/api/v1/kdocs/:id` | 详情 |
| POST | `/api/v1/kdocs/:id/update` / `delete` / `reindex` | 更新 / 删除 / 重索引 |

### 2.9 memory

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/memory/search?q=&k=` | 记忆检索 |
| GET/POST | `/api/v1/memory/episodes` | 情景记忆列表 / 写入 |
| POST | `/api/v1/memory/episodes/:id/delete` | 删除情景记忆 |
| GET | `/api/v1/memory/stats` | 记忆统计 |
| GET | `/api/v1/memory/recall?q=&k=` | 召回 |
| GET | `/api/v1/memory/long-term/:id` | 长期记忆 |
| GET/POST | `/api/v1/memory/facts` + `:id/delete` | 语义记忆 |
| GET/POST | `/api/v1/memory/procedures` + `:id/delete` | 程序记忆 |

### 2.10 channels / cron / pet / folders / files

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/api/v1/channels` + `:id/{update,delete,start,stop,test}` | 通知通道 |
| GET | `/api/v1/channels/:id/messages?limit=` | 通道消息日志 |
| GET/POST | `/api/v1/cron/jobs` + `:id/{update,delete,trigger}` | 定时任务 |
| GET | `/api/v1/pet/state` | 返回状态字符串 |
| GET | `/api/v1/pet/config` + `POST config/update` | 桌宠配置 |
| GET/POST | `/api/v1/pet/sprites` + `:id/delete` | 桌宠 sprite（预览用 `/files/sprites/{id}`） |
| POST | `/api/v1/pet/window/:mode` | 主窗 ⇄ 桌宠形态切换 |
| GET | `/api/v1/folders/tree?workspaceID=` / `/folders` + CRUD | 文件夹 |
| GET | `/api/v1/files/search?q=` / `/files` / `files/:id/delete` / `files/:id/preview-url` | 托管文件 |

---

## 3. Token 趋势（仪表盘三线图）

`GET /api/v1/dashboard/token-trend`

| 参数 | 取值 | 说明 |
|---|---|---|
| scope | `today`（默认）/ `week` / `month` / `custom` | 时间范围 |
| start_at / end_at | 毫秒时间戳 | `custom` 必填，精确到天 |

返回：

```json
{
  "scope": "today",
  "granularity": "hour",
  "start_at": 0, "end_at": 0,
  "labels": ["00","01","...","23"],
  "input": [..], "output": [..], "cache_read": [..],
  "total": 0
}
```

语义：默认「今日」固定 24 小时桶；`week` 为近 7 天、`month` 为本月 1 号起、`custom` 为自定义起止日，均按天递增。数据源为 `token_usages` 表（每次 LLM 调用一行）。

---

## 4. 前端路径纪律（防 404 清单）

- 前端 `http.ts` 的 origin 只含端口，**路径自带 `/api/v1`**，拼接后不能出现 `/api/v1/api/v1`。
- 静态图片/音频/视频用 `/files/**` 相对路径，**禁止**拼到业务 API 前缀。
- 删除/更新一律 `POST .../:id/delete|update`，前端不得出现 `/delete/:id` 风格。
- query 参数名与后端 `c.Query("...")` 完全一致（如 `page_size` 而非 `pageSize`）。
- 新增接口必须同时改：`router.go`、`api_*.go`、前端 store、`types/api.ts`、本文档。

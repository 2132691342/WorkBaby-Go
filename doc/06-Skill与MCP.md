# 06 · Skill 与 MCP

两个并列模块：`internal/skill/`（SKILL.md 解析 + 关键词路由 + 脚本挂工具）与 `internal/mcp/`（MCP stdio 客户端 + 工具桥接）。

---

## 1. Skill 系统

### 1.1 SKILL.md 格式

```
---
name: skill-name             # kebab-case，唯一
description: ...
when_to_use: [trigger, keywords]
allowed_tools: [tool1, tool2] # 工具白名单；空 = 不限制
version: 1.0
---

Markdown body  # 注入 system prompt
```

解析失败返回 8002。

### 1.2 三种来源

| 来源 | 生命周期 |
|---|---|
| builtin | `assets/skills/` 编译期 embed，启动 upsert 进表；只读 |
| download | 本地 zip 批量导入：每个含 `SKILL.md` 的目录为一个包（`scripts/` 子目录随包入库为可执行脚本） |
| custom | UI 编辑器创建 / 编辑（meta + SKILL.md 正文 + scripts） |

**唯一真相源是 `skills` 表**；SKILL.md 只是导入/导出格式。builtin 按 name upsert 且只读：zip 导入遇同名内置包直接跳过（避免覆盖后被启动期回写），custom/download 同名覆盖但保留用户启停状态。

**导入入口**：UI「从 zip 导入技能包」→ 原生对话框选 zip → `POST /api/v1/skills/import-zip`（服务端 `skill.ImportZip`），返回 `{imported, skipped, failed}` 逐包结果；单个包失败不阻断整体。

### 1.3 文件结构

```
internal/skill/
├── parser.go     # SKILL.md 解析（yaml frontmatter + body）
├── registry.go   # 运行时注册 + 关键词路由（Build/Reload/Get/Match）
└── builtin.go    # 从 embed FS 装载内置 Skill
```

- 单个 Skill 解析失败降级（记日志跳过）；
- `Match` 是确定性关键词匹配。

### 1.4 Skill 注入

chat service 发送时：关键词检测命中 → skill body 注入 system prompt + 工具白名单取交集。

### 1.5 Skill 执行引擎

**执行侧策略收紧**：harness `execOne` 校验调用工具必须在本次 run 暴露的 defs 中；模型幻觉出的未暴露工具名 → Refused 结构化回执（不计失败熔断，模型换路自愈）。Skill 白名单由此成为真实执行边界（而非仅 LLM 可见性过滤）。

**scripts 挂成工具**：`SkillREQ.scripts[]`（name/language/code）随 Skill 存 `skills.scripts_json`；注册通用工具 `run_skill_script`（`internal/tool/skillrun`）：

| 项 | 语义 |
|---|---|
| 参数 | `{skill, script, args?}`；resolver 从 skills 表解析脚本 |
| 解释器 | 固定映射 javascript→node / python→python / powershell→powershell，参数数组直传不经 shell |
| 隔离 | 脚本落临时文件用完即删；cwd = 会话工作区；内置运行时 bin 目录前置 PATH |
| 审批 | RiskExec，执行前过 ApprovalService（fail-closed：无审批门拒绝执行） |
| 限制 | 单脚本 ≤256KB；单次运行 2min 超时；错误码 9105 / 4003 / 8004 |

---

## 2. MCP（Model Context Protocol）

### 2.1 模块职责

- 桌面端 stdio 客户端（`internal/mcp/client.go`），JSON-RPC 2.0 行协议；
- 工具经 `MCPAdapter` 桥接进统一 `Tool` 接口，命名 `mcp__{server}__{tool}` 防冲突；
- 启动期 `mcp.Manager.Reload` 拨号 + 工具注册到 `tool.Registry`；
- 慢客户端断开 + Last-Event-ID 重放与 chat 的 SSE 同套机制（`server/sse.go`）。

### 2.2 接口子集（v1）

| 方法 | 说明 |
|---|---|
| `initialize` | 握手 + 协议版本协商 |
| `tools/list` | 列出 server 暴露的工具 |
| `tools/call` | 调用工具（JSON-RPC params） |
| `notifications/cancelled` | 客户端取消通知 |

### 2.3 文件结构

```
internal/mcp/
├── client.go   # stdio 客户端（exec.Command + Stdin/Stdout pipe + 行协议）
├── adapter.go  # MCP 工具 → 统一 Tool 接口（MCPAdapter）
└── manager.go  # 多 server 管理 + Reload + 工具注册/注销
```

### 2.4 配置（`mcp.json`）

配置源 `mcp.json`，支持 `mcpServers` 对象与 `servers` 数组两种格式（兼容历史配置）。同步规则见 `doc/02` §8.2。

### 2.5 错误处理

- 慢客户端：发送缓冲满即断开（触发重连重放），不丢帧；
- 子进程非 JSON 行忽略；
- 进程死亡 → 工具调用返回明确错误；
- 解析失败 → 工具不注册，调用时报 8001。

---

## 3. 安全

- Skill 脚本执行走 `run_skill_script` 工具，**不可绕过审批门**（fail-closed）；
- MCP 子进程仅启动白名单内命令（`mcp.json` 信任级别高于 exec 白名单，因配置源由用户编辑）；
- 所有工具的入参按 JSON Schema 校验后再执行。

---

## 4. 错误码

| Code | 含义 |
|---|---|
| 8000 | Skill/MCP 通用 |
| 8001 | skill / mcp server 不存在 |
| 8002 | SKILL.md 解析失败 |
| 8003 | builtin skill 只读 |
| 8004 | skill 禁用 / 脚本不存在 |

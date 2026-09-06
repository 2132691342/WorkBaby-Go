# 06 Skill 与 MCP

## 定位

- **Skill**：项目内置的"会做某件事的小机器人"，由声明式 `manifest.json` + 脚本（node/python/powershell）实现；模型按 Skill 名调用 `run_skill_script` 工具
- **MCP**（Model Context Protocol）：让 WorkBaby 作为 MCP client 连接外部 MCP server（stdio 传输），把它们暴露的工具当作本地工具用

## Skill

### 设计要点

- 内置 Skill 通过 `assets/skills/` embed 进二进制；运行时不依赖外网
- `SkillSource`（`internal/skill/`）：声明 Skill 名 → system 注入文本 + 工具白名单；命中规则由 `Matcher(input) → match(name)`
- `run_skill_script`（`internal/tool/skillrun/`）：把脚本落临时目录、用内置 runtime（node/python/pwsh）直跑，无 shell
- 审批：脚本执行一律 `RiskApprovalNeeds`（直跑解释器 = 等同本地命令）
- 沙箱：脚本 cwd 跟随会话工作区根（`WorkspaceRoot`）

### 核心契约

#### manifest.json

```json
{
  "name": "<skill-id>",
  "version": "1.0.0",
  "matcher": { "keywords": ["..."], "regex": "..." },
  "system": "你是一个...角色",
  "tools": ["file_read", "web_search"],
  "scripts": [
    { "name": "main", "language": "python", "file": "scripts/main.py", "args_schema": {} }
  ]
}
```

#### SkillSource.Preload

```
输入 user_input → Matcher 命中 → 返回 ContextPiece{Key:"skill", Body:system} + 工具白名单
              ↓ 注入到 SystemMessage
              ↓ SkillTools 写进 RunState（该 run 的工具集收窄）
```

#### `run_skill_script` 工具入参

| 字段 | 类型 | 必填 |
|---|---|---|
| `skill` | string | ✓ |
| `script` | string | ✓（manifest.scripts[].name） |
| `args` | []string | — |

返回 `CombinedOutput` + `exit code`，args 数组形式直传解释器，不拼 shell。

## MCP

### 设计要点

- **stdio 客户端**（`internal/mcp/`）遵循 [MCP 2025-06-18 spec](https://modelcontextprotocol.io/specification/2025-06-18)
- **Manager** 管理 server 进程生命周期：启动 / 重启 / 停用 / 失败降级
- MCP server 注册的工具经 `tool.Registry` 直接挂入 harness，无需适配层
- 工具名可能与内置冲突 → 注册时加 `mcp_<server>_<name>` 前缀
- 错误归一为 AppError 8003（连接失败 / 协议错）
- 内置运行时不依赖 npm / pip（用 17 节描述的 portable 打包）

### 核心契约

#### mcp.json 配置

```json
{
  "servers": [
    {
      "name": "<server-id>",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "."],
      "env": { "ROOT": "${workspace}" },
      "enabled": true
    }
  ]
}
```

DB 同步：`mcp_servers` 表持久化启停状态（mcp.json 是源，DB 是同步对象）。

#### Client.Connect / ListTools / CallTool

```go
client, _ := stdioClient.NewClient(transport)  // 启动子进程 + JSON-RPC 握手
tools,  _ := client.ListTools()                // → []tool.ToolDefinition
res,   _ := client.CallTool(ctx, name, args)   // → content 或 isError
```

启停控制：`Manager.ReloadServers()` 读 mcp.json + DB → 启/停 server → 增删 `tool.Registry` 条目。

### 关键流程

#### 启动失败优雅降级

```
Manager.Startup(...)
  for each server in enabled list:
    client, err := stdioClient.NewClient(...)
    if err != nil:
      log.Warn("MCP server {name} 启动失败，标记 degraded", err)
      continue  // 不中断整体启动
  return Manager with degraded set
```

UI 侧：Settings → MCP 面板显示 server 状态，failed 标红并给出 stderr 前 200 字符。

## Skill 与 MCP 的边界

| | Skill | MCP |
|---|---|---|
| 来源 | 项目内置（embed） | 外部服务（stdio 子进程） |
| 工具入口 | `run_skill_script` | MCP server 注册的每个工具独立 |
| System 注入 | SkillSource.Preload | 无（由 tool description 自然暴露） |
| 工具白名单 | manifest.tools | MCP server 实际暴露的工具 |
| 审批 | `RiskApprovalNeeds`（脚本 = 命令） | 按 MCP 工具的 RiskLevel + Gate 决策 |

## 约束

- Skill 脚本直跑解释器不拼 shell（避免命令注入）
- MCP 工具 schema 必须可被 JSON Schema 解析，schema 编译失败 = 注册失败（4004）
- MCP server 子进程崩溃 → Manager 状态置 degraded，重启由下次 ReloadServers 触发
- Skill system 注入与 MCP 工具白名单均为**请求级临时**，不落库到会话历史

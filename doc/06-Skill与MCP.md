# Skill 与 MCP

两个「给 Agent 扩能力」的通道：Skill 是本地技能包（知识 + 脚本），MCP 是外部工具进程。

## Skill

### 技能包协议

一个技能 = 一条 `skills` 表记录，来源可以是内置、表单创建或 zip 导入：

| 字段 | 用途 |
|---|---|
| `name` / `description` | 标识与描述 |
| `when_to_use` | 触发条件说明（自动匹配依据） |
| `body` | 技能正文（提示词级指令，命中后注入上下文） |
| `allowed_tools` | 该技能允许使用的工具白名单 |
| `scripts` | 可执行脚本（名称 + 语言 + 代码） |
| `enabled` / `source_kind` | 开关与来源（builtin / custom / zip） |

### 生效链路

1. **匹配**：每轮 run 前，`SkillService.Match(input)` 按 `when_to_use` / 关键词对用户输入打分，命中最高者成为激活技能。
2. **注入**：能力注册表把技能 `body` 注入 system 上下文，并把 `allowed_tools` 应用为该轮的工具白名单。
3. **执行**：`run_skill_script` 工具把脚本落盘到 `.workbaby/tmp` 后执行，解释器按语言固定映射（javascript / python / powershell），执行前过审批门。
4. 用户也可在输入框用 `@技能名` 显式引用。

zip 导入支持批量（含失败清单回执），导入后统一入 `skills` 表管理。

## MCP

### 定位

MCP server 是外部工具进程：WorkBaby 以 stdio 客户端身份拉起子进程，握手后枚举工具并转成本地工具注册表的一部分。

### 生命周期

```
配置(mcp_servers 表 / mcp.json) → Manager.Sync → 逐 server 拉起 stdio 子进程
  → Initialize 握手 → ListTools → 包装成 Tool 注册进 Registry
  → 调用即 JSON-RPC 转发；server 无响应/崩溃则该 server 标记 unready，不影响其他
  → 应用退出时统一回收子进程，避免孤儿进程
```

- 子进程 PATH 注入内置运行时目录（`runtimeMgr.BinDirs`），因此 `npx` / `uvx` 类 server 不依赖系统 Node / Python。
- 配置文件 `mcp.json` 是可选源：存在则同步进表；`raw` 接口支持直接编辑 JSON 并校验回滚。
- 环境变量中的凭据加密存储，列表接口返回脱敏值。
- `reload` 接口支持不停机热重载（注销旧注册、拉起新进程）。

### 失败语义

单个 MCP server 启动失败或调用失败只影响它自己（unready + 原因），不阻断应用启动与其他工具。管理页展示每个 server 的就绪态与工具清单。

## 两者与工具系统的关系

Skill 与 MCP 都在启动 / 变更时向 `tool.Registry` 注册工具，模型看到的工具列表是「内置工具 ∪ MCP 工具」的并集，再由激活技能的 `allowed_tools` 做一轮过滤。新增一个能力来源只需要实现 `tool.Tool` 并注册。

# 06 Skill 与 MCP

## 定位

- **Skill**：SKILL.md 声明的「会做某类事的指导 + 可选脚本」；按用户输入触发词路由，命中后把指导正文注入 system 并收窄本轮工具白名单；脚本经 `run_skill_script` 工具执行
- **MCP**（Model Context Protocol）：WorkBaby 作为 MCP client 连接外部 server（stdio 传输），把远端工具桥接成本地工具

## Skill

### 设计要点

- `skills` 表是唯一真相源，SKILL.md 只是导入/导出格式；内置 Skill 位于 `assets/skills/*/SKILL.md` 经 embed 打包，启动期 `SyncBuiltin` upsert 进表（保留用户 Enabled 状态），再由 Registry 全量装载
- 触发路由：`Registry.Match(input)` 对每个启用 Skill 的 `when_to_use` 触发词做大小写不敏感子串匹配，**命中触发词最长者优先**（更具体），等长按 skill 名字典序稳定排序；`when_to_use` 缺省时以 skill 名兜底（触发词为空会让 Skill 永远匹配不上）
- 命中后：指导正文注入 system（`capability/skill.go`，order=50）；`when_to_use` 之外的 `allowed_tools` 白名单写进 `RunState.SkillTools`，本轮工具集收窄到白名单（空 = 不限制）
- `run_skill_script`（`internal/tool/skillrun/`）：脚本内容来自 skills 表的 ScriptsJSON，落临时目录、用内置 runtime（node/python/pwsh）直跑，args 数组形式直传解释器，无 shell；cwd 跟随会话工作区根
- 审批：脚本执行由 runner 统一 per-call 裁决（`RiskApprovalNeeds`，每次确认）；脱链直调保留 Approver 兜底（fail-closed）

### 核心契约

#### SKILL.md

```markdown
---
name: pdf
version: 1.0.0
description: 处理 PDF 文档
when_to_use:
  - 合并 pdf
  - pdf 转文本
allowed_tools: [file_read, run_skill_script]
---
（Markdown 正文 = 命中后注入 system 的指导）
```

可选 `scripts/` 目录随 zip 包导入，落 `skills.ScriptsJSON`（`{name, language, content}`），挂成 `run_skill_script` 可调用的脚本。

## MCP

### 设计要点

- **stdio 客户端**（`internal/mcp/`）遵循 MCP 2025-06-18 spec：initialize 握手 → notifications/initialized → tools/list（跟随 nextCursor 翻页拉全量）→ tools/call
- **Manager** 管理 server 进程生命周期：启动 / 停用 / 失败降级 / **崩溃自动注销**（子进程退出触发 Done 通道 → Manager.stop 注销其已注册工具，避免模型反复调用失效工具）
- 工具名 `mcp_{server}_{tool}`；远端工具名中 `[^A-Za-z0-9_-]` 替换为 `_`（防非法字符让整次 LLM 请求 400）；与内置工具或其他 server 重名时跳过不覆盖
- 结果处理：文本块拼接回填；image / resource 块降级为占位文本（保留「这里有一张图 / 一个资源」的信号）；整体截断 50k 字符防撑爆上下文
- 子进程启动：Windows 上 `.cmd` / `.bat` 外壳与 cmd 内建命令自动包 `cmd /c`；内置运行时 bin 目录前置到子进程 PATH（`Manager.WithPathDirs`）；运行时 `rt.Ensure()` 同步完成后才 Sync MCP
- 错误归一为 AppError（8003 连接失败 / 8004 握手失败 / 8005 调用失败 / 8006 拉取失败）

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

`mcp.json` 是配置源，同步进 `mcp_servers` 表；env 值 AES-GCM 加密落库。

#### 启动失败优雅降级

单个 server 启动 / 握手 / 拉工具失败只标记 unready（错误写进 status），不阻断整体启动；设置页 MCP 面板展示状态与 stderr 尾部内容。

## Skill 与 MCP 的边界

| | Skill | MCP |
|---|---|---|
| 来源 | 内置 embed / zip 导入（skills 表为真相源） | 外部服务（stdio 子进程） |
| 工具入口 | `run_skill_script`（脚本统一入口） | 每个远端工具独立注册 |
| System 注入 | 触发词命中后注入指导正文 | 无（由 tool description 自然暴露） |
| 工具白名单 | SKILL.md 的 allowed_tools | MCP server 实际暴露的工具 |
| 审批 | 脚本执行 per-call 裁决 | 按工具 RiskLevel + Gate 决策 |

## 约束

- Skill 脚本直跑解释器不拼 shell（避免命令注入）
- MCP 工具 schema 必须可被 JSON Schema 解析，schema 编译失败 = 注册失败（4004）
- MCP server 子进程崩溃 → Done 通道触发自动 stop 并注销其工具；恢复由下一次 Reload 或手动启停
- Skill system 注入与 MCP 工具白名单均为请求级临时，不落库到会话历史

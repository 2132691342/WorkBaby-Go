# Skill 与 MCP

两个「给 Agent 扩展能力」的机制：Skill 是打包好的作业指导（提示词 + 工具白名单 + 可执行脚本），MCP 是外部工具进程。

## Skill

### 模型与来源

`skills` 表是唯一真相源；`SKILL.md` 只是导入/导出格式。字段：name（唯一）、description、when_to_use（触发词，每行一个）、body（Markdown 正文）、allowed_tools（JSON）、scripts_json（脚本数组）、frontmatter、source_kind、source_ref、version、enabled。

| 来源 | 说明 |
|---|---|
| builtin | 编译期内置（嵌入资源），每次启动重新解析入库，**只读**（不可改删），用户启停状态保留 |
| custom | 用户粘贴创建 / 目录自动发现 / **自进化草稿合入**（见下） |
| download | zip 技能包导入（支持批量，回执含成功与失败清单；内置同名保护跳过） |

**自进化**：run 终局由能力层用本轮 provider/model 抽取可复用流程，产出技能草稿入回写收件箱（`inbox_items`，kind=skill，状态 pending）；用户在记忆中心批准后落为 `custom` 技能并重建注册表。同名内置技能拒绝覆盖，避免草稿顶掉内置能力。

### 目录自动发现（两级叠加）

| 层级 | 路径 | 生效范围 |
|---|---|---|
| 全局 | `{home}/skills` | 启动同步一次，全部会话可用 |
| 工作区 | `<workspace>/.workbaby/skills` | run 前按会话叠加，**同名覆盖全局** |

目录约定：`<root>/<技能名>/SKILL.md`，可选 `<技能名>/scripts/*`（脚本 name = 去扩展名文件名，与 `run_skill_script` 的 script 参数一致）。

行为：切换工作区时整根摘除上一工作区技能并重扫（不跨工作区串味，被覆盖的全局版本恢复）；磁盘上已消失的条目自动摘除，但**内置技能与 UI 自建技能不受影响**；目录重扫保留用户启停状态；工作区未变化时零文件系统开销。触发词兼容 YAML 标量与序列两种写法。

### 命中与生效

1. **匹配**：每轮 run 前按用户输入对触发词做子串匹配，最长触发词优先（等长按技能名字典序），保证结果确定。
2. **注入**：正文注入 system（上限 6000 rune，低优先级），同时把 `allowed_tools` 作为本轮工具白名单约束；命中详情经事件回传前端时间线。
3. **未命中**：注入轻量技能索引（名 + 一句话用途，上限 800 rune，最低优先级），让模型知道有哪些能力可引导用户调用。
4. **显式引用**：输入框 `@技能名` 可直接命中。

### 脚本执行

`run_skill_script(skill, script, args)` 把脚本落临时文件后按语言固定映射执行（javascript → node、python → python、powershell → pwsh、bash → bash），**参数数组直传不经 shell**，执行前过审批门（执行代码一律需确认）。单脚本上限 256KB，执行超时 2 分钟，临时文件用完即删。

## MCP

### 定位与生命周期

MCP server 是外部工具进程：以 stdio 客户端身份拉起子进程，握手后枚举工具并注册进本地工具注册表。

- 握手：`initialize` → `notifications/initialized` → `tools/list`；调用走 `tools/call`，结果回填文本内容并区分 isError。
- 注册：工具名加来源前缀避免冲突，重名时跳过不覆盖；停用/删除 server 时注销对应工具。
- 热重载：`mcp.json` 为源，启动与重载时同步进 `mcp_servers` 表再对齐子进程；单条启动失败只标记 unready 并降级（不阻断其他 server 与应用启动）。
- 子进程生命周期跟随服务关闭；调用失败带明确错误码。

### 端点

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /mcp/servers | 列表（含运行状态，env 值掩码） |
| GET/POST | /mcp/servers/raw | 读取/写入 mcp.json 原文（写入失败回退备份） |
| POST | /mcp/servers | 新增 |
| POST | /mcp/servers/:name/enabled | 启停 |
| POST | /mcp/servers/:name/delete | 删除 |
| POST | /mcp/servers/reload | 重载并对齐子进程 |
| POST | /mcp/servers/reveal | 在文件管理器中定位配置文件 |

## 不变量

- 内置技能只读；技能名唯一，覆盖导入以 name 为准。
- 工作区技能仅在对应工作区生效，切换即失效；目录删除后不残留幽灵技能。
- 技能正文与索引都有注入上限，不挤占上下文预算。
- MCP 单条失败不影响其他 server 与本机工具；工具调用不经 shell 拼接。

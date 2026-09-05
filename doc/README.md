# WorkBaby · 设计文档

WorkBaby 是 Windows 本地优先的个人 AI 助手桌面客户端：单进程承载桌面壳与业务服务，
Agent 可调用工具干活（文件、命令、知识库、工作流、定时任务），状态全部落本地 SQLite。

平台：Windows 10/11 x64（WebView2）。技术栈与强制条款见 `CLAUDE.md` 与 `01-总体架构.md`。

## 文档索引

| 文档 | 内容 |
|---|---|
| [01-总体架构.md](01-总体架构.md) | 双主机进程模型、分层与依赖方向、启动序列、存储语义、设计优势 |
| [02-数据模型.md](02-数据模型.md) | 全部表与实体、ID/时间戳/字段命名约定、检查点与审批记录 |
| [03-LLM适配层.md](03-LLM适配层.md) | Provider 接口、协议归一、错误分类与有界重试 |
| [04-Agent与Harness内核.md](04-Agent与Harness内核.md) | ReAct 循环、事件体系、检查点幂等续跑、摘要压缩、子 Agent 委派、六钩子 LoopHooks、失败数据化唯一出口 |
| [05-工具系统.md](05-工具系统.md) | Tool 接口与风险分级、策略门与审批、内置工具清单、memory_write / run_workflow 等新增工具 |
| [06-Skill与MCP.md](06-Skill与MCP.md) | Skill 约定与注入、MCP stdio 客户端与工具桥接 |
| 能力接入契约（[01-总体架构 §3](../doc/01-总体架构.md)） | `internal/capability/`：Preload / Tools / Capture 三通道，Registry 串联装配 |
| [07-聊天与会话.md](07-聊天与会话.md) | 流式主链路、SSE 事件契约与可靠性、审批/补充输入、续跑 |
| [08-记忆系统.md](08-记忆系统.md) | 三层记忆、形成与召回、降级策略 |
| [09-知识库与RAG.md](09-知识库与RAG.md) | Loader / Chunker / 索引 / 检索 |
| [10-工作流引擎.md](10-工作流引擎.md) | 图模型、节点类型、执行器、暂停与恢复 |
| [11-媒体与生成式UI.md](11-媒体与生成式UI.md) | 媒体产物、工件登记、GenUI 内联渲染 |
| [12-定时任务与通知通道.md](12-定时任务与通知通道.md) | cron 调度、邮件 / Webhook / 控制台通道 |
| [13-桌宠.md](13-桌宠.md) | 形态切换、状态机、sprite 播种、迷你聊天、托盘 |
| [14-前端架构.md](14-前端架构.md) | 双主机前端、状态管理、流式渲染、设计 token、聊天交互 |
| [15-构建与分发.md](15-构建与分发.md) | 开发/验收命令、wails 构建、NSIS、架构门禁 |
| [17-API契约总表.md](17-API契约总表.md) | 全部路由与字段规范 |
| [18-内置运行时.md](18-内置运行时.md) | Node / Python / PowerShell 解压与 PATH 注入 |

## 能力矩阵

| 能力 | 说明 |
|---|---|
| 聊天 | 多轮 ReAct、流式输出、思考与正文分离、摘要压缩、限流自动重试、断点幂等续跑 |
| 子 Agent | 委派 coding/research/writer：上下文/预算/工具/输出四重隔离 + 同参去重，过程可视化 |
| 工具 | 命令执行、文件读写列、HTTP、联网搜索、文档解析、计划(todo)、补充输入(request_input) |
| 审批 | 策略门（allow/ask/deny）+ 命令白名单与危险模式；暂停态持久化；拒绝走 Refused 语义让模型自愈 |
| 记忆 | 短期窗口 / 长期 / 情景 / 语义 / 程序 多层；形成异步、召回不阻塞首 token |
| 知识库 | 多格式加载、分块索引、检索（FTS5 + 向量降级） |
| 工作流 | 可视化 DAG 编辑、条件分支、暂停 / 恢复 / 取消 |
| 定时与通知 | cron 触发工作流；邮件 / Webhook / 控制台投递与日志 |
| 文件与工作区 | 文件夹树、托管预览、写前快照与 diff 回滚 |
| 桌宠 | 与聊天同形象同状态；迷你聊天面板；托盘唤出；内置吉祥物首启播种 |
| 计量 | 每次 LLM 调用落明细；消息级用量 + 仪表盘分线趋势；上下文占用单一数据源 |
| 可观测 | run 事件 JSONL 导出（回放/测试/无头消费）；三通道日志 |

## 数据目录

默认用户目录（开发可用 `WORKBABY_HOME` 覆盖）：

```text
config.yaml            # Viper 配置
model.json / mcp.json  # 模型与 MCP 配置（文件为源，同步进库）
db/workbaby.db         # SQLite（WAL），含业务表与 FTS5 虚拟表
logs/                  # 三通道日志（info / warn / error）
workspaces/            # 会话工作区
runs/                  # run 事件 JSONL（无头导出）
runtimes/{id}/{ver}/   # 内置运行时解压目录
files/ media/ sprites/ # 托管文件、媒体产物、桌宠素材
knowledge/             # 知识库受管导入目录
```

检查点存 `agent_checkpoints` 表（SQL，跨重启可恢复）。

## 开发命令

```bash
go build ./...
go test ./...
powershell -ExecutionPolicy Bypass -File scripts/check-boundaries.ps1
wails dev                                         # 开发模式
wails build -nsis -ldflags "-s -w" -trimpath      # 发布
```

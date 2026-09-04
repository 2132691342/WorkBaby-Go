# WorkBaby

> **能干活的个人 AI 助手** —— 本地优先的桌面 Agent 客户端。
> Go 1.25 + Wails v2 + gin + SQLite + Vue 3 + TypeScript，单进程双主机。

## 架构：双主机（gin HTTP + SSE + Wails 壳）

```text
┌───────────────────────────────────────────────────────────────┐
│ Vue 3 前端（WebView2）                                          │
│  ├─ fetch       ──► http://127.0.0.1:{serverPort}/api/v1/**    │
│  ├─ EventSource ──► /api/v1/events   (SSE 流式/推送)            │
│  └─ Wails runtime ─► 系统能力：文件对话框 / 剪贴板 / 托盘 / 窗口   │
└───────────────────────────────────────────────────────────────┘
```

- 业务 API 全部走 gin HTTP（`POST/GET` + `{code,message,data}` 统一响应），随机回环端口启动后经 `app:ready` 注入；
- Agent 执行事件经 `harness → event.Bus → server/sse → EventSource` 推送（SSE 断线重放 + 心跳）；
- Wails 绑定仅保留系统能力；托盘菜单常驻，可唤出主窗口 / 桌宠、隐藏到托盘或退出；
- 配置以文件为源：`model.json`（LLM Provider）、`mcp.json`（MCP Server），启动同步进 DB。

## 快速开始

```bash
cd frontend && npm install && cd ..
go mod tidy
wails doctor
wails dev        # 开发模式（gin 随机端口 + Wails 壳）
```

构建与发布、日常命令见 [`doc/16-开发流程与命令.md`](doc/16-开发流程与命令.md) 与 [`doc/15-构建与分发.md`](doc/15-构建与分发.md)。

## 文档

- 设计与实现文档在 [`doc/`](doc/)（中文，共 18 篇，按模块从架构到运行时）；
- 工程规范基座为根目录 [`CLAUDE.md`](CLAUDE.md)（编码前必读，冲突以它为准）；
- 用户可见的内置文档在 [`assets/docs/`](assets/docs/)，运行时经 `GET /api/v1/docs` 暴露。
# WorkBaby-Go

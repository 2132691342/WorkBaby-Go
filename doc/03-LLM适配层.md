# 03 · LLM 适配层

`internal/llm/`：统一 Provider 抽象 + 三种协议适配（openai / anthropic / ollama），把流式协议、工具调用、推理文本、用量统计归一化后交给 `internal/harness/` 消费，并以错误分类驱动有界重试。

## 设计

- **一个接口，三种协议**：所有上游经 `Provider` 接口接入；`registry` 子包按 `AiProviderDO.kind` 构建实例（未知 kind 报 3020），构建失败记入 unready 表并在就绪接口可见。
- **协议归一化**：正文/推理分离的 `Message`、`TokenUsage`、`NormalizedToolCall` 是跨协议通货；各家的帧格式与字段差异全部在适配层内部吸收。
- **参数三级合并**：`ResolveParams` 按 请求 > Provider > 全局默认 取最高优先级非 nil 值；`ExtraBody` 浅合并；显式 `disabled` 的 ThinkingConfig 保留（显式意图不参与零值判断）。
- **错误即分类**：HTTP/transport 错误统一映射为 3000 段 `AppError`，重试器只看分类桶，不解析上游报文。
- **边界**：`llm/` 不 import harness / agent / api / service / wails；不感知会话与持久化。

## 核心契约

### Provider 接口（`provider.go`）

| 方法 | 语义 |
|---|---|
| `Name() string` / `Kind() ProviderKind` | 展示名；协议族 `openai` / `anthropic` / `ollama` |
| `Chat(ctx, *ChatRequest) (*ChatResponse, error)` | 非流式单轮 |
| `Stream(ctx, *ChatRequest) (<-chan StreamChunk, error)` | 流式；流结束/取消后由实现 close；流中错误经 `StreamChunk{Err}` 发送 |
| `Models(ctx) ([]ModelInfo, error)` | 模型清单 |
| `Ping(ctx) error` | 连通性 |

`ChatRequest`：Model / Messages（system 在首）/ Tools / Temperature / TopP / MaxTokens / Stop / Thinking / ExtraBody / User。
`StreamChunk`：Delta(Message 片段) / ToolCall（闭合时填一次）/ FinalUsage（末片）/ FinishReason / Err。

### 归一化类型

| 类型 | 要点 |
|---|---|
| `Message` | Role / Content / Thinking（推理文本独立，不混入 Content）/ ToolCalls / ToolCallID / ToolName |
| `TokenUsage` | InputTokens / OutputTokens / CacheReadTokens / CacheWriteTokens / TotalTokens |
| `NormalizedToolCall` | ID / Name / Arguments(`json.RawMessage`) |
| `toolcall.Accumulator` | OpenAI 流式 tool_calls 累积器：按 index 拼参数字符串，括号配对判完整后吐出；流尾 `Dump()` 兜底未闭合调用 |

### 三适配（`openai/` `anthropic/` `ollama/`）

| 适配 | 端点 | 流式帧 | 工具调用 | 推理文本 |
|---|---|---|---|---|
| openai | `{base}/chat/completions`（base 由用户完整给出，含版本段） | SSE `data:` 行，`[DONE]` 终止 | `tool_calls` 字符串片段 → Accumulator | reasoning_content/thinking 归入 Thinking |
| anthropic | `{base}/v1/messages`（anthropic-version: 2023-06-01） | SSE 事件序列 message_start → content_block_* → message_delta → message_stop | `content_block.type==tool_use` 完整块 | `content_block.type==thinking` 块 |
| ollama | `{base}/api/chat`（默认 `http://127.0.0.1:11434`） | ndjson 每行一对象，`done:true` 终止 | `message.tool_calls` 完整数组 | — |

三家 429 响应均把 `Retry-After` 头经 `RetryAfterHint` 以 `retry_after=Ns` 后缀嵌入错误文本。

### 错误码（3000 段，`errors.go`）与 MapHTTPStatus

| 码 | 语义 | HTTP 映射 |
|---|---|---|
| 3000 | 通用 | — |
| 3001 | provider 未就绪 | — |
| 3002 | API key 无效 | 401 / 403 |
| 3003 | 限流 | 429 |
| 3004 | 上游服务错误 | 5xx |
| 3005 | 响应超时 / transport 错误 | 408；连接类错误 |
| 3006 | 其余 4xx 请求错误 | ≥400 兜底 |
| 3007 | 模型不存在/无权限 | 404 |
| 3008 | 上下文过长 | 413 |
| 3009 | 能力未支持 | — |

### 错误分类与重试（`retry.go`）

| ErrorClass | 归入条件 | 策略 |
|---|---|---|
| transient | 3003/3004/3005、`[3003][3004][3005]` 内层标记、超时、EOF/连接重置/拒绝 | 可重试 |
| auth | 3001/3002（配置错误） | 不重试 |
| context | 3008（需压缩） | 不重试 |
| request | 3006/3007/3009（请求本身错） | 不重试 |
| cancelled | `context.Canceled` | 永不重试，且优先于一切判定 |
| unknown | 其余 | 保守不重试 |

- `ClassifyError`：先看外层 `AppError` 码；`pkg.Wrap` 会把内层段位码字符串化进 Details，故再扫描错误文本中的 `[code]` 标记兜底。
- `RetryPolicy`：`DefaultRetryPolicy()` = 3 次尝试、800ms 基准、8s 封顶；`Backoff` = Retry-After 优先，否则 `基准<<attempt` + 0~25% 抖动，封顶；`Wait` 期间尊重 ctx 取消。
- `RetryAfter`：从错误文本正则提取 `retry_after=(\d+)s`（1~120s 有效，越界按 0）。

## 关键流程

1. **建流重试**：harness `Runner.streamWithRetry` 调 `provider.Stream`；建流失败且 `IsTransient` 时，发 `EventRetry`（service 侧转为 `chat:retry` 事件，载荷 attempt / delay_ms / reason），`Wait` 退避后重建流，最多 `MaxAttempts` 次。
2. **流中途错误不重试**：避免重复输出；错误经 `StreamChunk{Err}` 交给 harness 处置。
3. **归一化**：适配层逐帧解析 → Delta/Thinking 片段、Accumulator 闭合的 `NormalizedToolCall`、末片 `FinalUsage` → harness 持续 Emit 并累计用量。
4. **就绪管理**：registry 构建/单实例 Reload；`PingAll` 顺序探活；未就绪原因在 `Status` 中可见，熔断状态由 service 侧对外暴露。

## 约束

- 依赖方向单向：registry → openai/anthropic/ollama → llm 基础类型；`llm/` 不反向依赖任何实现。
- HTTP 客户端不设 `Client.Timeout`（会把长流式在 30s 硬截断）：建连 10s / 响应头 30s，生命周期由调用方 ctx 控制。
- BaseURL 不做补全或嗅探：路径不合规得到 provider 明确错误（404/400），便于排查。
- 重试只发生在建流阶段且仅限瞬时类；取消信号永远优先、不吞不重。

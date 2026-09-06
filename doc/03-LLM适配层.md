# 03 LLM 适配层

## 定位

`internal/llm/` 把上游差异（OpenAI / Anthropic / Ollama / 国产兼容端）归一为统一的 `ChatRequest` / `ChatResponse` / `StreamChunk` / `Message` / `TokenUsage` 协议供 harness 使用。

## 设计要点

- **Provider 接口**（`llm.Provider`）：`Name` / `Kind` / `Chat` / `Stream` / `Models` / `Ping`，每个上游一个实现（`openai/`、`anthropic/`、`ollama/`）
- **流式 `StreamChunk`** 统一形态：`{Delta Message, ToolCall *NormalizedToolCall, FinalUsage *TokenUsage, FinishReason *string, Err error}`；harness 循环消费
- **工具调用归一**：`NormalizedToolCall{ID, Name, Arguments json.RawMessage}`，所有上游（结构化 + 文本兜底）汇流到同一形态
- **thinking 方言探测**：7 种方言（auto / none / adaptive / enabled / reasoning_effort / enable_bool），未知上游回落 `none`（不注入任何字段，宁可不开也不发错）
- **重试**：`DefaultRetryPolicy()` 仅重试瞬时错误（429 / 5xx / 超时），指数退避 + 抖动，`Retry-After` 优先
- **错误归一化**：`UpstreamError` 抽人话 + 原始 JSON 不得进 UI 文案

## 核心契约

### ChatRequest

| 字段 | 含义 |
|---|---|
| `Model` | 当前 run 的模型名 |
| `Messages` | 已归一化的消息序列（harness 装配） |
| `Tools` | `[]ToolDefinition{Name, Description, Parameters}` |
| `Temperature` / `TopP` / `MaxTokens` / `Thinking` / `ExtraBody` | 三层合并：请求级 > Provider 级 > 全局默认 |
| `User` | 固定 `"local"`（本机单用户） |

### TokenUsage

| 字段 | 含义 |
|---|---|
| `InputTokens` / `OutputTokens` | 本轮实际 |
| `CacheReadTokens` / `CacheWriteTokens` | 缓存拆分维度（与 Input 拆解，不叠加） |
| `TotalTokens` | 上游给出（部分上游不回 → 用三项之和兜底） |

### Thinking 方言探测表

| 探测 | 命中 host | 命中 model |
|---|---|---|
| `adaptive` | `minimax / minimax.chat / api.minimax` | — |
| `enabled` | `bigmodel.cn / open.bigmodel / volces.com / volcengineapi.com / ark.cn-beijing / moonshot.cn / moonshot.ai` | — |
| `enable_bool` | `dashscope / bailian / aliyuncs.com` | qwen3 / `-thinking` |
| `reasoning_effort` | `api.openai.com` | `o1` / `o3` / `o4` / `gpt-5` |
| `none` | `deepseek.com / api.deepseek` | — |
| `none`（默认） | — | 其它 |

显式指定（Provider 设置）优先于探测。

### `ResolveParams(req, providerParams, defaults)`

合并顺序：请求级 `reqParams` → `providerParams` → `defaults`。三处都缺时回落 LLM API 默认。

## 关键流程

### 流式响应汇聚

```
provider.Stream() → chan StreamChunk
  → harness 循环：
    Delta.Content → EventTurnDelta("content", ...)
    Delta.Thinking → EventTurnThinking("thinking", ...)
    ToolCall → EventToolCall(...)
    FinalUsage → 累计 r.usage
    FinishReason → 终止判定
    Err → 跳出本轮收尾
```

### BuildAssistantMessage

构造发给下一轮的 assistant 消息：合并 `content` + `tool_calls`（`role=assistant`）。

### 上游错误归一化

`ExtractUpstreamError(body)` → `UpstreamError{StatusCode, Message, Kind, Raw}`，前端拿 `Message`（人话），不直接拼 `Raw`。

## 约束

- 任何上游差异（字段名 / 枚举值 / 默认开闭）都在 `internal/llm/<provider>/` 内吸收，不泄漏到 harness
- thinking 字段默认**不下发**（避免未知上游 400）；用户显式选择才发
- `chatcmpl-tool-*` 风格的 tool call id 由上游给，harness 不造，但保证幂等键 `tool:<name>|<args>` 在 resume 时命中

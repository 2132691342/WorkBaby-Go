# LLM 适配层

位置：`internal/llm/`。职责：把「流式对话 + 工具调用 + 思维链」这一套协议统一成单一抽象，上层（harness）不感知具体供应商。

## Provider 接口

```go
type Provider interface {
    Name() string
    Kind() ProviderKind                       // anthropic | openai | ollama
    Chat(ctx, *ChatRequest) (*ChatResponse, error)
    Stream(ctx, *ChatRequest) (<-chan StreamChunk, error)
    Models(ctx) ([]ModelInfo, error)
    Ping(ctx) error
}
```

`ChatRequest` 携带 messages / tools / temperature / thinking_effort；`StreamChunk` 是统一流式单元，四种载荷：

| 载荷 | 含义 |
|---|---|
| `Delta.Message.Content` | 正文增量 |
| `Delta.Message.Thinking` | 思维链增量（独立通道，永不混入正文） |
| `ToolCall` | 完整工具调用（归一化后） |
| `FinishReason` | 本轮流终止原因（`stop` / `tool_calls`） |
| `FinalUsage` | 本轮 token 用量 |

## 适配实现

| 文件 | 适配对象 | 关键差异处理 |
|---|---|---|
| `anthropic/client.go` | Claude 系 | thinking 走独立 content block；usage 的 `cache_read_input_tokens` / `cache_creation_input_tokens` 归一化，`InputTokens` 统一为「含缓存」口径 |
| `openai/client.go` | OpenAI 及兼容端点（DeepSeek / GLM / Qwen 等） | `prompt_tokens_details.cached_tokens` → `CacheReadTokens`；思维链兼容 `<think>` 标签的端点在渲染层剥离 |
| `ollama/client.go` | 本地模型 | 无 key，`cache_read_count` 归一化 |

缓存 token 的统一口径：**缓存命中是输入的子维度，不与 Input 叠加**。因此「实际计费输入 = InputTokens − CacheReadTokens」在所有协议下同义，仪表盘据此画三线图。

### 上游缓存误报校正

部分 OpenAI 兼容端点（如 GLM）会把 `cached_tokens` 报成等于 `prompt_tokens`，导致命中率恒为 100%、token 总数虚高。三个 adapter 在解析 usage 后统一做防御性 clamp：

| 适配 | 规则 |
|---|---|
| openai | `CacheReadTokens > InputTokens` → 清零 + warn |
| anthropic | `CacheRead + CacheWrite > InputTokens` → 清零 + warn |
| ollama | `CacheReadCount > PromptEvalCount` → 清零 |

存量脏数据由 `POST /admin/cleanup-token-usages` 一次性清理（`cache_read_tokens > input_tokens` 的行清零，幂等）。

## Thinking 渲染策略

`thinking.go` 按端点能力选择思维链呈现方式：

- 原生支持（Claude / DeepSeek-R1 等）：thinking 走独立字段，前端独立面板展示。
- 不支持但模型会把 `<think>` 写进正文：适配层在渲染前剥离已闭合块与未闭合尾部，避免推理文本污染正文与记忆。

## Registry 与熔断

`registry.Registry` 持有全部 enabled provider 的就绪态：

- 构建时逐条解密 apiKey，失败的单条标记 unready，不影响其他 provider。
- 每次调用记录成败；连续失败进入熔断（拒绝请求并带原因），冷却后半开试探。
- 前端可查熔断状态并手动重置（`/ai-provider/circuit-status`、`/ai-provider/:id/reset-circuit`）。

## 自动降级

harness 支持挂载 `TurnAdjuster`：建流失败（限流 / 5xx / 熔断）时按 `chat.fallback_model` 降级重试本轮。前端在流式气泡顶部显示「第 N 次重试」提示，正文恢复后自动消失。

## 配置源

`ai_providers` 表是真源；若 `{home}/model.json` 存在，启动时同步进表（文件缺失 / 解析失败仅告警，不覆盖 DB 存量）。apiKey 在 DB 中恒为密文，读取时按需解密。

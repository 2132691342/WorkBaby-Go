# LLM 适配层

位置：`internal/llm/`（子包 `registry/`、`openai/`、`anthropic/`、`ollama/`、`toolcall/`）。职责：把「流式对话 + 工具调用 + 思维链」统一为单一抽象，上层 harness 不感知具体供应商。

## Provider 抽象

```go
type Provider interface {
    Name() string
    Kind() ProviderKind // openai | anthropic | ollama
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    Stream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error)
    Models(ctx context.Context) ([]ModelInfo, error)
    Ping(ctx context.Context) error
}
```

- `ChatRequest`：Model / Messages（system 在首）/ Tools / Temperature / TopP / MaxTokens / Stop / Thinking / ExtraBody / User / SessionID。
- `StreamChunk`：正文增量、思维增量（独立通道，永不混入正文）、工具调用（某调用闭合时发一次）、结束原因、末帧用量、错误。
- 流式原语：三类协议统一经 `llm.NewChunkStream(ctx, buf)` 建通道，发送在 ctx 取消时返回 false 由生产者退出；生产者负责 close，**消费者必须读到 channel 关闭**（提前放弃会把生产者挂死在满缓冲的 send 上，泄漏 goroutine 与上游连接）。
- `ChatResponse`：Message / ToolCalls / Usage / StopReason（end_turn / tool_use / max_tokens / length / stop）。
- `Message`：Content（模型可见正文）、Thinking（推理文本，独立字段）、ToolCalls、ToolCallID/ToolName（role=tool 回填）、Name。
- `NormalizedToolCall{ID, Name, Arguments}`：各协议经 `toolcall` 子包归一到此。

## 注册中心

`registry.Registry` 持有 enabled provider 实例与未就绪原因：

- `Build`：逐条解密 apiKey；解密或装配失败的单条记入 unready（带错误原因），不影响其他条。按 kind 装配，openai 先解析思维方言。
- `Get(id)` 未就绪返回错误及原因；`IsReady` / `Reason` / `Status` 供 api 层判定；`PingAll` 顺序探测；`Reload` 单实例重建。
- 前端状态（circuit-status）：CLOSED（已构建）/ OPEN（构建失败，带 reason）/ DISABLED（设置关闭）；reset 接口重建。

## 三类协议适配

| 维度 | openai | anthropic | ollama |
|---|---|---|---|
| 端点 | `{base}/chat/completions` | `{base}/v1/messages` | `{base}/api/chat` |
| 鉴权 | `Authorization: Bearer` | `x-api-key` + `anthropic-version` | 无 |
| 采样字段 | 顶层 `temperature`/`top_p`/`max_tokens`/`stop` | 顶层，`stop`→`stop_sequences`，`max_tokens` 缺省 1024 | 收进 `options`：`temperature`/`top_p`/`num_predict` |
| system | messages 首条 | 顶层 `system` 多段 | messages 内 |
| 工具定义 | `tools[].function.{name,description,parameters}` | `tools[]{name,description,input_schema}` | `tools[]{type,function{...}}` |
| 工具结果 | role=tool + tool_call_id | role=user + content[].tool_result | role=tool + tool_call_id |
| 流帧 | SSE `data:`，`[DONE]` 终止 | 事件流：message_start → content_block_* → message_delta → message_stop | ndjson，`done:true` 终止 |
| 工具调用 | `delta.tool_calls[].function.arguments` 片段累积 | `tool_use` 块，`input_json_delta` 累积、block stop 闭合 | 一次性完整 |
| 结束原因 | `finish_reason` | `stop_reason` | `done_reason` |
| 思维 | `delta.reasoning_content` | `thinking` 块 + `thinking_delta` | `message.thinking` |
| 缓存 | `prompt_tokens_details.cached_tokens` | `cache_read_input_tokens` / `cache_creation_input_tokens` | `cache_read_count` |

`/models`：openai 走 `{base}/models`；ollama 走 `/api/tags`；anthropic 返回静态清单。

## 思维方言

`ThinkingStyle`：`none`（不发）/ `adaptive` / `enabled` / `reasoning_effort` / `enable_thinking`；空串 = auto。

- 探测：按 host / 模型关键词有序匹配，无命中回落 `none`（安全默认，避免向上游发未知字段 400）。
- 解析：显式指定优先，否则自动探测；出参曝光实际生效方言。
- 渲染：把归一化的 `ThinkingConfig{Type,BudgetTokens}` 译成上游顶层字段；`disabled` 在部分方言下仍需显式下发（部分模型默认开思考），无法关闭时改不下发。
- effort ↔ 预算映射：off → disabled；low/medium/high → enabled + 2048/8192/16384。
- 分离：思考增量走独立通道；兼容端点把 `<think>` 写进正文时，沉淀侧剥离已闭合块与未闭合尾部。

## 参数三层合并

字段级取最高优先级的非 nil 值：`请求级 > Provider 级 > 全局默认`。

- 覆盖字段：temperature / top_p / max_tokens / thinking；ExtraBody 浅合并（请求级覆盖同名键）。
- Provider 级来源：`Temperature==0`、`ThinkingEffort==""` 视作未设置（跳到下层）。
- 全局默认：`chat.defaultTemperature`（缺省 0.2）、`chat.defaultThinking`（缺省 medium）；MaxTokens/TopP 为 0 视为不限。
- ThinkingConfig 不参与零值判断——显式 `disabled` 也保留（关闭是显式意图）。
- 来源标注：生效值与来源（provider / default / builtin）一并返回，前端输入框与设置页共用。

## 错误分类与重试

- 错误码 3000-3999：未就绪 / key 无效 / 限流 / 5xx / 超时 / 请求错 / 模型不存在 / 上下文过长 / 不支持；`MapHTTPStatus` 映射 HTTP 码；上游报文拆为单行文案 + 修复建议。
- 分类归桶：transient（429/5xx/超时/连接重置，可重试）/ auth / context / request / cancelled / unknown。
- 重试策略：多次尝试、指数退避 + 抖动封顶；优先 Retry-After（有上限），等待尊重 ctx 取消。
- 仅重试「建流」失败（瞬时类）；流中途错误不重试（避免重复输出）。
- 自动降级：建流失败耗尽重试后换备用模型重试本轮；备用模型须与主模型同 Provider，同 run 换模型次数受停滞上限约束。

## 流式解析

- openai：逐行读 SSE，`[DONE]` 时冲刷未闭合工具调用；非法 JSON 帧上报后继续。
- anthropic：按事件类型处理，text/thinking 累积，tool_use 参数在 `input_json_delta` 累积、block stop 闭合；`message_delta` 取 stop_reason 与用量。
- ollama：ndjson 每行独立解析，`done:true` 时给结束原因与用量。
- 工具调用累积：按 index 拼 arguments，括号/字符串配平判定闭合，结束前冲刷未闭合项。
- 用量：多在末帧才带，故为指针、仅末片填充。

## 用量与计费

- 口径：缓存命中是输入的子维度，**不与 Input 叠加**；`InputTokens` 统一为完整 prompt（含缓存读写）。故「实际计费输入 = Input − CacheRead」跨协议同义。
- 防御性校正：cache 读/写大于 Input（上游误报）时清零并告警，避免命中率恒 100%。
- `TotalTokens = Input + Output`（不加缓存）。
- 单价：`pricing.<model>` 存 `input_per_m` / `output_per_m` / `cache_read_per_m`；非缓存输入按 input 单价、缓存读按 cache_read（未配则按 input 全价）、输出按 output 估算；未配单价不计费。
- 落库：每次上游调用落一行 `token_usages`（run / message / turn / provider / model / latency / cost），`source` 区分 chat / workflow / cron / memory / delegate（委派单独记账）。

## 能力声明与模型元数据

- `context_window`（0 = 全局默认，驱动压缩预算）、`max_output_tokens`（0 = 不限）。
- 三态能力：`supports_tool_call` / `supports_vision` / `supports_reasoning` 为三态值——未声明按 kind 或模型名推断，显式值优先。
  - tool_call：openai / anthropic 默认支持，ollama 默认不支持。
  - vision / reasoning：按模型名关键词推断。
- 出参同时给三态原值与解算后布尔值，前端不必自行推断。
- 内置模型目录（`internal/llm/modelmeta/catalog.json`，嵌入）：provider 行未声明 `context_window` 或未配单价时按模型名兜底——子串匹配、最长 match 优先；目录未收录回退全局缺省/不计费。上下文窗口三级回退：provider 声明 → 模型目录 → 全局缺省。

## 两层模型与预设

- 协议实现 ≠ provider 配置：三家 client 是唯一的协议实现层，`ai_providers` 表行只是配置数据（kind 选协议 + baseURL/模型/参数）。新增一个 OpenAI 兼容 provider 零代码。
- 内置预设（`GET /ai-provider/presets`）：常见模型服务（OpenAI / Anthropic / DeepSeek / Kimi / GLM / Qwen / OpenRouter / SiliconFlow / 本机 Ollama / LM Studio）的 kind + base_url + 首选模型清单，前端「新增模型」一键预填。
- 三家协议 client 的线协议测试（`internal/llm/<kind>/client_test.go`）经 `internal/llmtest` 假上游回放 SSE/ndjson 帧，锁定解析契约：字段漂移在测试层暴露，不进用户对话。

## 配置源与不变量

- `ai_providers` 表是真源；`{home}/model.json` 存在时启动与热重载同步进表（文件缺失条目置 disabled 保留历史）。apiKey 落库恒为密文，构建时按需解密。
- 思维链永不混入正文；只进独立通道与独立字段。
- Provider 不感知会话，历史组装由上层完成；channel 由实现 close，流内错误经流对象上报。
- 缓存 token 始终是输入的子维度。
- 未知上游不注入思维字段（方言回落 none），可显式指定方言纠偏。
- 单条 provider 解密/装配失败只影响自身就绪，不阻断全局。

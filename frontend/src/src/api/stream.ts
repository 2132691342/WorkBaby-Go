import type { ChatStreamReq, ChatStreamEvent } from '@/types/api'
import { apiPost, apiGet, getApiBase } from './http'
import { onUnmounted } from 'vue'

/**
 * 流式聊天订阅 + 发送（SSE 版，doc/15 §3）。
 *
 * <p>双主机架构：先 POST /api/v1/chat/stream 发起 run 拿 runID，
 * 再建立 GET /api/v1/events?scope=chat&runId={runId} 的 SSE 长连接消费事件。
 *
 * <p><b>M2 可靠性套件</b>（与后端 sse.go 的 seq/Last-Event-ID 重放/具名 ping 心跳配套）：
 * <ul>
 *   <li>lastEventId 追踪：断线重连带 last_event_id（Header 优先，手动重连走 query），
 *       服务端从 RunEventLog 重放缺口</li>
 *   <li>watchdog：75s（> 2×30s 心跳）无任何事件判定假死，主动断开重连</li>
 *   <li>指数退避重连（500ms 起步，上限 8s），重连成功（sse-ready）后回调
 *       {@link StreamChatOpts.onReconnect} 由 store 重拉权威快照</li>
 * </ul>
 */

export interface StreamHandle {
  promise: Promise<void>
  cancel: () => void
}

/** streamChat 可选项。 */
export interface StreamChatOpts {
  /** 手动重连成功后调用（store 据此重拉会话快照，弥补重放覆盖不到的窗口）。 */
  onReconnect?: () => void
}

/**
 * 归一化后端停止原因 → 前端领域原因（blocks.ts 的 stopReasonText 只认这组值）。
 *
 * <p>为什么必须归一化：历史版本后端 chat:done 同时下发领域 reason（completed/cancelled…）
 * 与 LLM 原始 finish_reason（stop/length/tool_calls…）。原始值一旦直通前端，
 * stopReasonText 不命中 → 兜底文案「已停止生成（部分内容已保留）」在正常结束也误弹。
 * 归一化后未知/平台自定义终因一律按 completed 处理（不制造惊扰横幅）。
 */
function normalizeStopReason(raw: string | null | undefined): string {
  const v = (raw ?? '').trim().toLowerCase()
  switch (v) {
    case 'completed':
    case 'end_turn':
    case 'stop':
    case 'tool_calls':
    case 'function_call':
    case '':
      return 'completed'
    case 'cancelled':
      return 'cancelled'
    case 'max_turns':
      return 'max_turns'
    case 'stagnation':
      return 'stagnation'
    case 'length':
    case 'max_tokens':
    case 'budget_exceeded':
    case 'token_budget':
      return 'token_budget'
    case 'error':
    case 'content_filter':
      return 'error'
    default:
      return 'completed'
  }
}

/** SSE event 载荷 → decoder 事件；返回 null 表示该事件无需下传。 */
export function mapSSEEvent(name: string, data: unknown): ChatStreamEvent | null {
  const p = (data ?? {}) as Record<string, unknown>
  switch (name) {
    case 'chat:stream':
      return { type: 'content', data: p.delta ?? '' }
    case 'chat:thinking':
      return { type: 'thinking', data: p.delta ?? '' }
    case 'chat:stats':
      // 每轮结束累计用量 → decoder 的 stats 分支（setStats）
      return {
        type: 'stats',
        data: {
          input_tokens: p.input_tokens,
          output_tokens: p.output_tokens,
          cache_read_tokens: p.cache_read_tokens,
          cache_creation_tokens: p.cache_creation_tokens,
          total_tokens: p.total_tokens,
          latency_ms: p.latency_ms
        }
      }
    case 'chat:tool':
      return { type: 'tool_call', data: { id: p.id, name: p.name, args: p.arguments, agent: p.agent ?? '' } }
    case 'chat:tool-result':
      return {
        type: 'tool_result',
        data: { id: p.id, name: p.name, output: p.content ?? '', state: p.error ? 'error' : 'success', agent: p.agent ?? '' }
      }
    case 'chat:subagent-start':
      return { type: 'subagent_start', data: { sub_run_id: p.sub_run_id, agent: p.agent ?? '' } }
    case 'chat:subagent-done':
      return { type: 'subagent_done', data: { sub_run_id: p.sub_run_id, agent: p.agent ?? '', reason: p.reason ?? '' } }
    case 'chat:subagent-error':
      return { type: 'subagent_error', data: { sub_run_id: p.sub_run_id, agent: p.agent ?? '', message: p.message ?? '' } }
    case 'chat:approval':
      return {
        type: 'tool_approval_request',
        data: {
          id: p.id,
          command: p.command ?? '',
          reason: p.reason ?? '',
          risk: p.risk === 'irreversible' ? 'irreversible' : p.risk === 'input_required' ? 'input_required' : 'needs_approval'
        }
      }
    case 'chat:approval-decided':
      return {
        type: 'approval_decided',
        data: { id: p.id, decision: p.decision ?? 'unknown' }
      }
    case 'chat:done':
      // 领域 reason 优先；原始 stop_reason 归一化兜底，避免误弹「已停止生成」
      return {
        type: 'stopped',
        data: { reason: normalizeStopReason(String(p.reason ?? p.stop_reason ?? p.status ?? 'completed')) }
      }
    case 'chat:error':
      return { type: 'error', data: { code: p.code, message: p.message ?? 'unknown error' } }
    case 'chat:retry':
      return { type: 'retry', data: { attempt: p.attempt, delay_ms: p.delay_ms } }
    case 'chat:gap':
      // 断线重放窗口已被环形缓冲覆盖：decoder 转 requestSnapshot，
      // 由 store 立即拉权威快照兜底。
      return { type: 'gap', data: { run_id: p.run_id, last_seq: p.last_seq } }
    case 'ping':
      // 心跳：仅用于前端 watchdog 续期（resetWatchdog 在 handle 内），不下传 decoder
      return null
    // P2：模型产出/任务生命周期事件，旁路消费
    case 'chat:todo':
      return { type: 'todo', data: { state: p.state } }
    case 'chat:file-change':
      return { type: 'file_change', data: { change: p.change } }
    case 'chat:artifact':
      return { type: 'session_artifact', data: { artifact: p.artifact } }
    case 'task:created':
    case 'task:started':
    case 'task:done':
      return { type: 'task', data: { task: p.task } }
    default:
      return null
  }
}

/** SSE 事件名白名单（订阅这些类型，其余忽略）。 */
const EVENT_NAMES = [
  'chat:stream',
  'chat:thinking',
  'chat:stats',
  'chat:tool',
  'chat:tool-result',
  'chat:approval',
  'chat:approval-decided',
  'chat:done',
  'chat:error',
  'chat:gap',
  // M2 可靠性：具名心跳（服务端 sse.go 30s 一次）
  'ping',
  // P2 扩展事件
  'chat:todo',
  'chat:file-change',
  'chat:artifact',
  'task:created',
  'task:started',
  'task:done',
  // 子 Agent 委派生命周期（后端 chat.go 对子 run 事件独立成通道，避免假 chat:done）
  'chat:subagent-start',
  'chat:subagent-done',
  'chat:subagent-error',
  // 建流瞬时错误自动重试提示
  'chat:retry'
] as const

/** 重连退避：500ms 起步指数递增，8s 封顶。 */
const RECONNECT_BASE_MS = 500
const RECONNECT_MAX_MS = 8000
/**
 * watchdog 阈值：心跳 30s 一次，容忍连续两跳丢失 + 调度抖动。
 * 触发即判定连接假死（TCP 未断但数据不通），主动断开走退避重连。
 */
const WATCHDOG_MS = 75_000

export function streamChat(
  req: ChatStreamReq,
  onEvent: (event: ChatStreamEvent) => void,
  opts: StreamChatOpts = {}
): StreamHandle {
  let es: EventSource | null = null
  let finished = false
  let cancelled = false
  let lastEventId = ''
  let reconnectAttempt = 0
  let watchdog: ReturnType<typeof setTimeout> | null = null
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null

  const promise = (async (): Promise<void> => {
    // 1) 发起 run：POST /api/v1/chat/stream → {run_id, session_id, ...}；续跑走 /chat/stream/{id}/resume
    let runID: string
    try {
      if (req.resume_run_id) {
        const res = await apiPost<{ run_id: string }>(`/api/v1/chat/stream/${req.resume_run_id}/resume`)
        runID = res.run_id
      } else {
        const res = await apiPost<{ run_id: string }>('/api/v1/chat/stream', {
          session_id: req.session_id,
          content: req.message,
          // model_id 是前端解析过的 model 名；后端 SendStream / CreateSession 用作兜底回查 provider
          model: req.model_id ?? undefined,
          temperature: req.temperature ?? undefined,
          thinking_effort: req.thinking_effort ?? undefined
        })
        runID = res.run_id
      }
    } catch (e) {
      finished = true
      onEvent({ type: 'error', data: { message: e instanceof Error ? e.message : String(e) } })
      return
    }
    if (cancelled) return

    // 2) SSE 连接 + 手动重连
    const base = getApiBase()
    if (!base) {
      finished = true
      onEvent({ type: 'error', data: { message: '[4000] server not initialized' } })
      return
    }

    const clearWatchdog = (): void => {
      if (watchdog !== null) {
        clearTimeout(watchdog)
        watchdog = null
      }
    }

    const resetWatchdog = (): void => {
      clearWatchdog()
      watchdog = setTimeout(() => scheduleReconnect(), WATCHDOG_MS)
    }

    const scheduleReconnect = (): void => {
      if (finished || cancelled) return
      es?.close()
      es = null
      clearWatchdog()
      if (reconnectTimer !== null) return
      const delay = Math.min(RECONNECT_BASE_MS * 2 ** reconnectAttempt, RECONNECT_MAX_MS)
      reconnectAttempt++
      reconnectTimer = setTimeout(() => {
        reconnectTimer = null
        if (!finished && !cancelled) openStream()
      }, delay)
    }

    const openStream = (): void => {
      const url =
        `${base}/events?scope=chat&runId=${encodeURIComponent(runID)}` +
        (lastEventId ? `&last_event_id=${encodeURIComponent(lastEventId)}` : '')
      es = new EventSource(url)

      es.addEventListener('sse-ready', () => {
        // 重连成功：先补快照（重放覆盖不到的窗口由 store 权威拉取兜底），再续命 watchdog
        if (reconnectAttempt > 0) {
          opts.onReconnect?.()
          reconnectAttempt = 0
        }
        resetWatchdog()
      })

      const handle = (name: string) => (e: MessageEvent<string>) => {
        if (cancelled) return
        resetWatchdog()
        if (e.lastEventId) lastEventId = e.lastEventId
        let data: unknown
        try {
          data = e.data ? JSON.parse(e.data) : {}
        } catch {
          data = {}
        }
        const ev = mapSSEEvent(name, data)
        if (ev) onEvent(ev)
        if (name === 'chat:done' || name === 'chat:error') {
          finished = true
          clearWatchdog()
          es?.close()
        }
      }

      for (const name of EVENT_NAMES) {
        es.addEventListener(name, handle(name) as EventListener)
      }
      es.onerror = () => {
        if (finished || cancelled) {
          clearWatchdog()
          es?.close()
          return
        }
        scheduleReconnect()
      }
      resetWatchdog()
    }

    openStream()

    // 3) 等待终态（chat:done / chat:error 置 finished）
    while (!finished && !cancelled) {
      await new Promise((r) => setTimeout(r, 100))
    }
    clearWatchdog()
    if (reconnectTimer !== null) clearTimeout(reconnectTimer)
    // es 经 openStream() 闭包赋值；TS 跨函数不追踪外部 let，静态推断仍为 null → 显式断言恢复类型
    if (es !== null) (es as EventSource).close()
  })()

  const cancel = (): void => {
    cancelled = true
    finished = true
    if (watchdog !== null) clearTimeout(watchdog)
    if (reconnectTimer !== null) clearTimeout(reconnectTimer)
    es?.close()
  }

  // 让调用方在 onUnmounted 时机自动调取消。
  try { onUnmounted(cancel) } catch { /* 非组件调用上下文忽略 */ }

  return { promise, cancel }
}

// 保留：让调用方在挂载后主动拉取权威消息（store 已用 loadMessages）。
export function fetchMessages(sessionID: string): Promise<unknown> {
  return apiGet(`/api/v1/chat/sessions/${sessionID}/messages?limit=200`)
}

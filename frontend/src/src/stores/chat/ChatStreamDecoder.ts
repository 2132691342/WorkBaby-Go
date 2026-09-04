import type {
  Artifact,
  ArtifactPayload,
  BackgroundTask,
  ChatStats,
  ChatStreamEvent,
  FileChange,
  TodoStateRESP
} from '@/types/api'
import type { UiNode } from '@/components/genui/GenUiRenderer.vue'

/**
 * 流式聊天事件解码器（纯函数）：输入 WS 帧事件 → 输出 {@link StreamEventUpdate} 描述对象，store 负责 apply。
 * 事件映射：{@code content/thinking → appendContent/appendThinking}、{@code tool_call → addTool}、{@code tool_result → updateTool}、{@code artifact → setArtifacts}、
 * {@code tool_approval_request → setApproval}、{@code error → setError}、{@code stopped → setStopReason}（中性终态）。
 */
export interface ToolCallInfo {
  id: string
  name: string
  args?: string
  result?: string
  state?: 'running' | 'success' | 'error'
  started_at?: number
  duration_ms?: number
  /** 子 Agent 来源标签（delegate 转发的子 run 工具事件）；空 = 主 Agent。 */
  agent?: string
}

export interface ApprovalRequest {
  id: string
  command: string
  reason: string
  /**
   * 风险等级：
   * - `needs_approval`：危险但可恢复，本会话批准过同一命令后免审
   * - `irreversible`：执行后无法撤销（rm -rf / format / dd if= …），每次都要确认
   * - `input_required`：模型补充输入请求（request_input 工具），卡片切问答形态
   */
  risk?: 'needs_approval' | 'irreversible' | 'input_required'
}

/** decoder 输出：store 拿到后按需 apply 到 ref state。 */
export interface StreamEventUpdate {
  appendContent?: string
  appendThinking?: string
  addTool?: ToolCallInfo
  updateTool?: { id: string; name?: string; argsDelta?: string; result?: string; success: boolean; agent?: string }
  setStats?: ChatStats
  setArtifacts?: ArtifactPayload
  setGenUi?: UiNode
  setApproval?: ApprovalRequest
  /**
   * 清 pendingApproval（chat:approval-decided → approval_decided）。
   * 仅 id 匹配的才清，避免误清并发新审批请求。
   */
  clearApproval?: { id: string; decision: string }
  setError?: string
  /** 建流瞬时错误自动重试提示（限流/5xx）；正文恢复后自动清除。 */
  setRetry?: { attempt: number; delay_ms: number }
  /** 流终止原因（cancelled 等）。中性终态，区别于 error。 */
  setStopReason?: string
  // ===== P2 扩展事件 =====
  /** todo 工具每次动作后推送的快照（覆盖式）。 */
  setTodo?: TodoStateRESP
  /** file_write 写入后推送的单条变更（最新在前；按 run 范围前端可选择性过滤）。 */
  pushFileChange?: FileChange
  /** 工件登记事件（file_write 旁路自动 upsert）。 */
  pushArtifact?: Artifact
  /** 后台任务生命周期事件（task:created/started/done）。 */
  upsertTask?: BackgroundTask
  /** 子 Agent 生命周期（subagent_start/done/error）：按 id 合并状态，不整体替换。 */
  updateTask?: { id: string; agent?: string; state?: BackgroundTask['state']; error?: string }
  /**
   * 断线重放窗口已失效（chat:gap）→ 请求 store 立即拉取权威快照。
   * 由 store 的 batcher 消费（异步动作），applyStreamUpdate 忽略。
   */
  requestSnapshot?: boolean
}

/** 解码单个事件为 store update；type 不识别 / data 字段缺失返回 null。 */
export function decodeStreamEvent(event: ChatStreamEvent, now: number = Date.now()): StreamEventUpdate | null {
  const data = event.data
  switch (event.type) {
    case 'content':
      return typeof data === 'string' ? { appendContent: data } : null
    case 'thinking':
      return typeof data === 'string' ? { appendThinking: data } : null
    case 'stats':
      return data && typeof data === 'object' ? { setStats: data as ChatStats } : null
    case 'tool_call': {
      if (!data || typeof data !== 'object') return null
      const d = data as { id: string; name: string; agent?: string }
      return { addTool: { id: d.id, name: d.name, state: 'running', started_at: now, agent: d.agent || undefined } }
    }
    case 'tool_call_delta': {
      if (!data || typeof data !== 'object') return null
      const d = data as { id: string; arguments_delta?: string }
      return d.arguments_delta ? { updateTool: { id: d.id, argsDelta: d.arguments_delta, success: false } } : null
    }
    case 'tool_result': {
      if (!data || typeof data !== 'object') return null
      const d = data as { id: string; name: string; output: string; state: string; agent?: string }
      const update: StreamEventUpdate = {
        updateTool: { id: d.id, name: d.name, result: d.output, success: d.state === 'success', agent: d.agent || undefined }
      }
      // 修复：gen_ui 工具结果同步解析 UiTree（避免 store 二次解析）
      if (d.name === 'gen_ui' && d.output) {
        try {
          const tree = JSON.parse(d.output) as { root: UiNode }
          update.setGenUi = tree.root
        } catch {
          // 解析失败时忽略 setGenUi，保留 updateTool 让用户在 tool_result 中查看原文
        }
      }
      return update
    }
    case 'artifact':
            return data && typeof data === 'object' ? { setArtifacts: data as ArtifactPayload } : null
        case 'todo': {
          if (!data || typeof data !== 'object') return null
          const d = data as { state?: TodoStateRESP }
          return d.state ? { setTodo: d.state } : null
        }
        case 'file_change': {
          if (!data || typeof data !== 'object') return null
          const d = data as { change?: FileChange }
          return d.change ? { pushFileChange: d.change } : null
        }
        case 'session_artifact': {
          if (!data || typeof data !== 'object') return null
          const d = data as { artifact?: Artifact }
          return d.artifact ? { pushArtifact: d.artifact } : null
        }
        case 'task': {
          if (!data || typeof data !== 'object') return null
          const d = data as { task?: BackgroundTask }
          return d.task ? { upsertTask: d.task } : null
        }
        case 'subagent_start': {
          if (!data || typeof data !== 'object') return null
          const d = data as { sub_run_id?: string; agent?: string }
          if (!d.sub_run_id) return null
          return {
            upsertTask: {
              id: d.sub_run_id, session_id: '', agent: d.agent ?? 'subagent', prompt: '',
              state: 'running', run_id: d.sub_run_id, result: '', error: '',
              created_at: now, started_at: now, finished_at: 0
            }
          }
        }
        case 'subagent_done': {
          if (!data || typeof data !== 'object') return null
          const d = data as { sub_run_id?: string; agent?: string }
          if (!d.sub_run_id) return null
          return { updateTask: { id: d.sub_run_id, agent: d.agent, state: 'completed' } }
        }
        case 'subagent_error': {
          if (!data || typeof data !== 'object') return null
          const d = data as { sub_run_id?: string; agent?: string; message?: string }
          if (!d.sub_run_id) return null
          return { updateTask: { id: d.sub_run_id, agent: d.agent, state: 'failed', error: d.message ?? '' } }
        }
        case 'tool_approval_request': {
      if (!data || typeof data !== 'object') return null
      const d = data as { id: string; command: string; reason: string; risk?: string }
      return {
        setApproval: {
          id: d.id,
          command: d.command,
          reason: d.reason,
          risk: d.risk === 'irreversible' ? 'irreversible' : d.risk === 'input_required' ? 'input_required' : 'needs_approval'
        }
      }
    }
    case 'approval_decided': {
      // 反向事件：清 pendingApproval。id 不匹配则忽略（避免误清并发新请求）
      if (!data || typeof data !== 'object') return null
      const d = data as { id: string; decision: string }
      return { clearApproval: { id: d.id, decision: d.decision } }
    }
    case 'error':
      return { setError: extractError(data) }
    case 'retry': {
      if (!data || typeof data !== 'object') return null
      const d = data as { attempt?: number; delay_ms?: number }
      return { setRetry: { attempt: d.attempt ?? 1, delay_ms: d.delay_ms ?? 0 } }
    }
    case 'gap':
      // 断线重连后重放窗口已被覆盖（RunEventLog 环形缓冲滚掉）：
      // 增量补不齐 → 立即拉权威快照做最终一致
      return { requestSnapshot: true }
    case 'stopped': {
      // 用户主动停止 → 中性终态。后端 ChatStreamService.onCancelled 发出。
      const reason = data && typeof data === 'object' && 'reason' in data
        ? String((data as { reason: unknown }).reason)
        : 'cancelled'
      return { setStopReason: reason }
    }
    default:
      return null
  }
}

/** 从事件 data 安全取错误消息字符串（与原 store 实现一致）。 */
function extractError(data: unknown): string {
  if (data && typeof data === 'object' && 'message' in data) {
    return String((data as { message: unknown }).message)
  }
  return String(data)
}

/**
 * 应用单个 update 到 store 状态（store 内部 use）。
 * 这里做成 pure 让 chat.ts 简化；副作用（修改 ref）封装在此函数里。
 */
export function applyStreamUpdate(
  update: StreamEventUpdate,
  state: {
    streamingContent: { value: string }
    streamingThinking: { value: string }
    streamingTools: { value: ToolCallInfo[] }
    streamingStats: { value: ChatStats | null }
    streamingArtifacts: { value: ArtifactPayload | null }
    streamingGenUi: { value: UiNode | null }
    pendingApproval: { value: ApprovalRequest | null }
    error: { value: string | null }
    /** 建流自动重试提示（可选）。 */
    streamingRetry?: { value: { attempt: number; delay_ms: number } | null }
    /** 流终止原因（用户主动停止时由 stopped 事件写入）。 */
    stopReason: { value: string | null }
    // ===== P2 扩展字段（可选，调用方按需传入） =====
    todoState?: { value: TodoStateRESP | null }
    fileChanges?: { value: FileChange[] }
    artifacts?: { value: Artifact[] }
    tasks?: { value: BackgroundTask[] }
  },
  now: number = Date.now()
): void {
  if (update.appendContent !== undefined) {
    state.streamingContent.value += update.appendContent
    // 正文恢复 → 重试提示完成使命
    if (state.streamingRetry && state.streamingRetry.value) state.streamingRetry.value = null
  }
  if (update.appendThinking !== undefined) {
    state.streamingThinking.value += update.appendThinking
  }
  if (update.setStats) {
    state.streamingStats.value = update.setStats
  }
  if (update.addTool) {
    state.streamingTools.value.push(update.addTool)
  }
  if (update.updateTool) {
    const t = state.streamingTools.value.find((x) => x.id === update.updateTool!.id)
    if (t) {
      if (update.updateTool.argsDelta) {
        t.args = (t.args ?? '') + update.updateTool.argsDelta
      }
      if (update.updateTool.result !== undefined) {
        t.result = update.updateTool.result
      }
      if (update.updateTool.agent) {
        t.agent = update.updateTool.agent
      }
      t.state = update.updateTool.success ? 'success' : 'error'
      if (t.started_at) t.duration_ms = now - t.started_at
    } else {
      // tool_result 在 tool_call 之前到达（理论可能）— 仍插入
      state.streamingTools.value.push({
        id: update.updateTool.id,
        name: update.updateTool.name ?? '?',
        result: update.updateTool.result,
        state: update.updateTool.success ? 'success' : 'error',
        agent: update.updateTool.agent
      })
    }
  }
  if (update.setArtifacts) {
    state.streamingArtifacts.value = update.setArtifacts
  }
  if (update.setGenUi !== undefined) {
    state.streamingGenUi.value = update.setGenUi
  }
  if (update.setApproval) {
    state.pendingApproval.value = update.setApproval
  }
  if (update.clearApproval) {
    // 仅 id 匹配的才清：避免在「A 决策事件」到达时误清「B 新审批请求」
    if (state.pendingApproval.value && state.pendingApproval.value.id === update.clearApproval.id) {
      state.pendingApproval.value = null
    }
  }
  if (update.setError !== undefined) {
    state.error.value = update.setError
  }
  if (update.setRetry !== undefined && state.streamingRetry) {
    state.streamingRetry.value = update.setRetry
  }
  if (update.setStopReason !== undefined) {
    state.stopReason.value = update.setStopReason
  }
  if (update.setTodo && state.todoState) {
    state.todoState.value = update.setTodo
  }
  if (update.pushFileChange && state.fileChanges) {
    const list = state.fileChanges.value
    const i = list.findIndex((c) => c.id === update.pushFileChange!.id)
    if (i >= 0) list[i] = update.pushFileChange!
    else list.unshift(update.pushFileChange!)
  }
  if (update.pushArtifact && state.artifacts) {
    const list = state.artifacts.value
    const i = list.findIndex((a) => a.id === update.pushArtifact!.id)
    if (i >= 0) list[i] = update.pushArtifact!
    else list.unshift(update.pushArtifact!)
  }
  if (update.upsertTask && state.tasks) {
    const list = state.tasks.value
    const i = list.findIndex((t) => t.id === update.upsertTask!.id)
    if (i >= 0) list[i] = update.upsertTask!
    else list.unshift(update.upsertTask!)
  }
  if (update.updateTask && state.tasks) {
    const t = state.tasks.value.find((x) => x.id === update.updateTask!.id)
    if (t) {
      if (update.updateTask.agent) t.agent = update.updateTask.agent
      if (update.updateTask.state) t.state = update.updateTask.state
      if (update.updateTask.error !== undefined) t.error = update.updateTask.error
      if (update.updateTask.state === 'completed' || update.updateTask.state === 'failed') {
        t.finished_at = now
      }
    }
  }
}